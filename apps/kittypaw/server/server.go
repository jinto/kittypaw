package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jinto/kittypaw/channel"
	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/engine"
	"github.com/jinto/kittypaw/store"
)

// Server is the HTTP/WebSocket gateway that bridges REST clients and browsers
// to the agent engine. It owns the chi router, the engine session, the
// scheduler, channel spawner, tenant router, and all handler state.
type Server struct {
	config         *core.Config
	configMu       sync.RWMutex // protects config during hot-reload
	store          *store.Store
	session        *engine.Session // default-tenant session; HTTP handlers use this
	scheduler      *engine.Scheduler
	router         chi.Router
	spawner        *ChannelSpawner        // manages channel lifecycle for hot-reload
	tenants        *TenantRouter          // routes channel events to tenant-scoped sessions
	tenantList     []*core.Tenant         // ordered tenant metadata for startup validation
	tenantRegistry *core.TenantRegistry   // shared cross-tenant registry (Share.read / Fanout)
	eventCh        chan core.Event        // shared event channel between channels and dispatch loop
	tenantMu       sync.Mutex             // serializes AddTenant/RemoveTenant — validation→register→reconcile must not interleave
	tenantDeps     map[string]*TenantDeps // retained close-targets (Store+MCP) for RemoveTenant; populated on successful AddTenant
	version        string
	pkgManager     *core.PackageManager // default-tenant package manager for API handlers
	secrets        *core.SecretsStore   // default-tenant secrets store for API handlers

	// reloadReconcile, if non-nil, replaces s.spawner.Reconcile inside
	// handleReload. Test-only hook that lets AC-RELOAD-SYNC inject a barrier
	// to observe the synchronous contract; production always leaves this nil
	// and falls through to the live spawner.
	reloadReconcile func(tenantID string, cfgs []core.ChannelConfig) error
}

// DefaultTenantID is the tenant ID used when the daemon runs without an
// explicit tenants/ directory (legacy single-tenant deployments).
const DefaultTenantID = "default"

// New wires together all dependencies and returns a ready-to-serve Server.
// Callers must pass at least one TenantDeps; New panics on an empty slice
// because a daemon with no tenants has nothing to route to.
//
// One engine.Session is built per tenant. The family tenant (IsFamily=true)
// receives a ChannelFanout wired to the shared eventCh so its skills can
// push to personal tenants via Fanout.send; personal tenants keep Fanout
// nil so the JS global stays hidden (I5 — personal cannot reach personal).
// Every session shares the same *core.TenantRegistry pointer so Share.read
// can resolve peer tenants by ID.
//
// The HTTP handler surface (scheduler, /api/v1, secrets) remains bound to
// the default tenant in PR-1; multi-tenant HTTP routing is scoped to a
// follow-up. Call StartChannels before ListenAndServe to activate messaging.
func New(tenants []*TenantDeps, version string) *Server {
	if len(tenants) == 0 {
		panic("server.New: tenants slice must be non-empty")
	}

	// Identify the default tenant: explicit DefaultTenantID match wins; the
	// first entry is the fallback so a single-tenant install with a non-
	// "default" ID still boots.
	defaultDeps := tenants[0]
	for _, td := range tenants {
		if td.Tenant.ID == DefaultTenantID {
			defaultDeps = td
			break
		}
	}
	cfg := defaultDeps.Tenant.Config

	// eventCh MUST exist before Fanout construction — ChannelFanout retains
	// a reference for every future send.
	eventCh := make(chan core.Event, 64)

	// Shared cross-tenant registry. BaseDir points at the tenants/ root
	// (the parent of each tenant's BaseDir) so Share.read and future
	// listing operations have a consistent anchor.
	tenantsRoot := filepath.Dir(defaultDeps.Tenant.BaseDir)
	registry := core.NewTenantRegistry(tenantsRoot, DefaultTenantID)
	tenantList := make([]*core.Tenant, 0, len(tenants))
	for _, td := range tenants {
		registry.Register(td.Tenant)
		tenantList = append(tenantList, td.Tenant)
	}

	router := NewTenantRouter()
	var defaultSession *engine.Session
	depsByID := make(map[string]*TenantDeps, len(tenants))
	for _, td := range tenants {
		sess := buildTenantSession(td, registry, eventCh)
		router.Register(td.Tenant.ID, sess)
		depsByID[td.Tenant.ID] = td
		if td == defaultDeps {
			defaultSession = sess
		}
	}

	s := &Server{
		config:         cfg,
		store:          defaultDeps.Store,
		session:        defaultSession,
		scheduler:      engine.NewScheduler(defaultSession, engine.NewSharedBudget(cfg.Features.DailyTokenLimit), defaultDeps.PkgMgr),
		tenants:        router,
		tenantList:     tenantList,
		tenantRegistry: registry,
		eventCh:        eventCh,
		tenantDeps:     depsByID,
		version:        version,
		pkgManager:     defaultDeps.PkgMgr,
		secrets:        defaultDeps.Secrets,
	}
	s.router = s.setupRoutes()
	return s
}

// setupRoutes builds the full route tree. API routes live under /api/v1 and
// are optionally gated by an API key. The WebSocket endpoint sits at /ws.
// Setup and bootstrap endpoints are unauthenticated so the onboarding
// wizard can run before an API key exists.
func (s *Server) setupRoutes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(s.corsMiddleware)

	// Health check (unauthenticated — daemon liveness probe).
	r.Get("/health", s.handleHealth)

	// Bootstrap (unauthenticated — returns api_key + ws_url to the GUI).
	r.Get("/api/bootstrap", s.handleBootstrap)

	// Setup / onboarding routes (unauthenticated).
	r.Route("/api/setup", func(r chi.Router) {
		// Always accessible.
		r.Get("/status", s.handleSetupStatus)
		r.Get("/kakao/pair-status", s.handleSetupKakaoPairStatus)

		// Localhost only.
		r.Post("/reset", s.handleSetupReset)

		// Guarded — localhost only, blocked after onboarding is complete.
		r.Group(func(r chi.Router) {
			r.Use(s.requireLocalhost)
			r.Use(s.requireOnboardingIncomplete)
			r.Post("/llm", s.handleSetupLlm)
			r.Post("/telegram", s.handleSetupTelegram)
			r.Post("/telegram/chat-id", s.handleSetupTelegramChatID)
			r.Post("/kakao/register", s.handleSetupKakaoRegister)
			r.Post("/api-server", s.handleSetupAPIServer)
			r.Post("/workspace", s.handleSetupWorkspace)
			r.Post("/http-access", s.handleSetupHttpAccess)
			r.Post("/complete", s.handleSetupComplete)
		})
	})

	r.Route("/api/v1", func(r chi.Router) {
		if s.config.Server.APIKey != "" {
			r.Use(s.requireAPIKey)
		}

		// Status / history
		r.Get("/status", s.handleStatus)
		r.Get("/executions", s.handleExecutions)

		// Agents
		r.Get("/agents", s.handleAgents)
		r.Get("/agents/{id}/checkpoints", s.handleCheckpointsList)
		r.Post("/agents/{id}/checkpoints", s.handleCheckpointsCreate)

		// Skills
		r.Get("/skills", s.handleSkills)
		r.Post("/skills/run", s.handleSkillsRun)
		r.Post("/skills/teach", s.handleSkillsTeach)
		r.Post("/skills/teach/approve", s.handleTeachApprove)
		r.Delete("/skills/{name}", s.handleSkillsDelete)
		r.Post("/skills/{name}/enable", s.handleSkillEnable)
		r.Post("/skills/{name}/disable", s.handleSkillDisable)
		r.Post("/skills/{name}/explain", s.handleSkillExplain)
		r.Get("/skills/{id}/fixes", s.handleSkillFixes)

		// Fixes
		r.Post("/fixes/{id}/approve", s.handleFixApprove)

		// Suggestions
		r.Get("/suggestions", s.handleSuggestionsList)
		r.Post("/suggestions/{skill_id}/accept", s.handleSuggestionsAccept)
		r.Post("/suggestions/{skill_id}/dismiss", s.handleSuggestionsDismiss)

		// Checkpoints
		r.Post("/checkpoints/{id}/rollback", s.handleCheckpointRollback)

		// Chat
		r.Post("/chat", s.handleChat)

		// Config
		r.Get("/config/check", s.handleConfigCheck)
		r.Post("/reload", s.handleReload)

		// Admin — runtime tenant lifecycle. Localhost-only on top of the
		// /api/v1 requireAPIKey gate: the daemon binds to 127.0.0.1 by
		// default, but if a future deployment exposes it, admin mutations
		// still require local access.
		r.Route("/admin", func(r chi.Router) {
			r.Use(s.requireLocalhost)
			r.Post("/tenants", s.handleAdminTenantAdd)
			r.Post("/tenants/{id}/delete", s.handleAdminTenantRemove)
		})

		// Install
		r.Post("/install", s.handleInstall)

		// Search
		r.Get("/search", s.handleSearch)

		// Packages (gallery)
		r.Get("/packages", s.handlePackagesList)
		r.Post("/packages/install-from-registry", s.handlePackageInstallFromRegistry)
		r.Get("/packages/{id}", s.handlePackageDetail)
		r.Delete("/packages/{id}", s.handlePackageUninstall)
		r.Post("/packages/{id}/config", s.handlePackageConfigSet)

		// Channels
		r.Get("/channels", s.handleChannels)

		// Memory
		r.Get("/memory/search", s.handleMemorySearch)

		// Users / identity
		r.Post("/users/link", s.handleUsersLink)
		r.Get("/users/{id}/identities", s.handleUsersIdentities)
		r.Delete("/users/{id}/identities/{channel}", s.handleUsersUnlink)

		// Reflection
		r.Get("/reflection", s.handleReflectionList)
		r.Post("/reflection/{key}/approve", s.handleReflectionApprove)
		r.Post("/reflection/{key}/reject", s.handleReflectionReject)
		r.Post("/reflection/clear", s.handleReflectionClear)
		r.Post("/reflection/run", s.handleReflectionRun)
		r.Get("/reflection/weekly-report", s.handleWeeklyReport)

		// Persona evolution
		r.Get("/persona/evolution", s.handleEvolutionList)
		r.Post("/persona/evolution/{id}/approve", s.handleEvolutionApprove)
		r.Post("/persona/evolution/{id}/reject", s.handleEvolutionReject)

		// Profiles
		r.Get("/profiles", s.handleProfileList)
		r.Post("/profiles", s.handleProfileCreate)
		r.Post("/profiles/{id}/activate", s.handleProfileActivate)

		// Workspaces
		r.Get("/workspaces", s.handleWorkspacesList)
		r.Post("/workspaces", s.handleWorkspacesCreate)
		r.Delete("/workspaces/{id}", s.handleWorkspacesDelete)
	})

	// WebSocket sits outside /api/v1 — auth is done via query param or header.
	r.HandleFunc("/ws", s.handleWebSocket)

	// Static web assets with SPA fallback — must be last (catch-all).
	r.Handle("/*", staticHandler())

	return r
}

// getConfig returns the current server config under RWMutex for hot-reload safety.
func (s *Server) getConfig() *core.Config {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.config
}

// ProcessEvent runs a single event through the engine session and returns
// the agent response. This is used by the channel dispatch loop to bridge
// inbound channel messages to the agent engine.
func (s *Server) ProcessEvent(ctx context.Context, event core.Event) (string, error) {
	return s.session.Run(ctx, event, nil)
}

// StartChannels creates the ChannelSpawner, reconciles each tenant's
// channel configs, and starts the dispatch and retry goroutines. Must be
// called before ListenAndServe.
//
// Two startup validations run BEFORE any channel spawns:
//   - ValidateTenantChannels: a single Telegram bot token / Kakao relay
//     URL cannot be claimed by two tenants (C3 — prevents silent update
//     races where one tenant's bot steals another's messages).
//   - ValidateFamilyTenants: the family tenant must not declare channels
//     (C10 — family is a coordinator, not a channel owner; a misconfigured
//     [telegram] on family would race the real personal bot for updates).
func (s *Server) StartChannels(ctx context.Context) error {
	tenantChannels := make(map[string][]core.ChannelConfig, len(s.tenantList))
	for _, t := range s.tenantList {
		if t.Config == nil {
			continue
		}
		tenantChannels[t.ID] = t.Config.Channels
	}

	if err := core.ValidateTenantChannels(tenantChannels); err != nil {
		return fmt.Errorf("channel config validation: %w", err)
	}
	if err := core.ValidateFamilyTenants(s.tenantList); err != nil {
		return fmt.Errorf("family tenant validation: %w", err)
	}

	s.spawner = NewChannelSpawner(ctx, s.eventCh)
	for tenantID, configs := range tenantChannels {
		if len(configs) == 0 {
			continue
		}
		if err := s.spawner.Reconcile(tenantID, configs); err != nil {
			slog.Warn("initial channel reconcile: some channels failed",
				"tenant", tenantID, "error", err)
		}
	}
	go s.dispatchLoop(ctx)
	go s.retryPendingResponses(ctx)
	return nil
}

// dispatchLoop reads events from the shared eventCh, routes them to the
// tenant-scoped engine session, and returns responses via the spawner.
//
// Events with an empty or unknown TenantID are dropped by the TenantRouter
// (no default fallback) to avoid cross-tenant privacy leaks — see C1 in
// the family-multi-tenant spec.
func (s *Server) dispatchLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.eventCh:
			if !ok {
				return
			}
			// EventFamilyPush carries a FanoutPayload (not a ChatPayload) and
			// delivers an already-composed message to the target tenant —
			// skip the agent loop entirely. Without this branch the generic
			// ParsePayload below silently produces a zero-valued ChatPayload,
			// the event routes through session.Run, and the push text never
			// reaches the target channel.
			if event.Type == core.EventFamilyPush {
				s.deliverFamilyPush(ctx, event)
				continue
			}
			payload, err := event.ParsePayload()
			if err != nil {
				slog.Warn("channel event: bad payload", "type", event.Type, "error", err)
				continue
			}

			session := s.tenants.Route(event)
			if session == nil {
				// Drop was already logged + counted by TenantRouter.
				continue
			}

			// AC-T7: chat_id ownership check. Route() matched TenantID to a
			// Session, but a compromised/leaked bot token could still inject
			// an event whose chat_id belongs to a different tenant. Without
			// this gate alice's Session.Run would persist bob's conversation
			// under alice's store — a privacy breach the TenantID check
			// alone cannot catch. Permissive when AdminChatIDs is empty
			// (legacy single-tenant installs, web_chat-only tenants).
			if !core.ChatBelongsToTenant(session.Config, payload.ChatID) {
				s.tenants.RecordMismatch(event.TenantID)
				slog.Warn("tenant_routing_mismatch",
					"tenant", event.TenantID,
					"chat_id", payload.ChatID,
					"type", event.Type,
				)
				continue
			}

			slog.Info("processing channel event",
				"type", event.Type,
				"tenant", event.TenantID,
				"chat_id", payload.ChatID,
				"from", payload.FromName,
			)

			// Build RunOptions with Confirmer-based permission callback if available.
			var runOpts *engine.RunOptions
			ch, chOK := s.spawner.GetChannel(event.TenantID, event.Type, "")
			if chOK {
				if confirmer, ok := ch.(channel.Confirmer); ok {
					evType := string(event.Type)
					chatID := payload.ChatID
					runOpts = &engine.RunOptions{
						OnPermission: func(pCtx context.Context, desc, res string) (bool, error) {
							s.logPermissionEvent("requested", evType, chatID, desc, res)

							timeout := s.permissionTimeout()
							permCtx, cancel := context.WithTimeout(pCtx, timeout)
							defer cancel()

							ok, err := confirmer.AskConfirmation(permCtx, chatID, desc, res)
							var decision string
							switch {
							case err != nil:
								decision = "timeout"
							case ok:
								decision = "approved"
							default:
								decision = "denied"
							}
							s.logPermissionEvent(decision, evType, chatID, desc, res)
							return ok, err
						},
					}
				}
			}

			var response string
			var runErr error
			panicked := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked = true
						engine.RecoverTenantPanic(session, "server.dispatchLoop", r)
					}
				}()
				response, runErr = session.Run(ctx, event, runOpts)
			}()
			if panicked {
				// Health marked Degraded inside the recover helper.
				// Drop this event — re-invoking the same panicking run
				// on the same input would loop; AC-T8 only requires that
				// the daemon survive and other tenants keep ticking.
				continue
			}
			if runErr != nil {
				slog.Error("channel event: engine error",
					"type", event.Type,
					"tenant", event.TenantID,
					"chat_id", payload.ChatID,
					"error", runErr,
				)
				continue
			}
			// Clean completion re-promotes the tenant to Ready so a
			// transient panic self-heals without operator action.
			engine.MarkTenantReady(session)

			if !chOK {
				slog.Warn("channel event: no channel for response routing, enqueuing for retry",
					"type", event.Type, "tenant", event.TenantID)
				if event.Type != core.EventKakaoTalk {
					_ = s.store.EnqueueResponse(event.TenantID, string(event.Type), payload.ChatID, response)
				}
				continue
			}

			if err := ch.SendResponse(ctx, payload.ChatID, response); err != nil {
				slog.Error("channel event: send response failed",
					"type", event.Type,
					"tenant", event.TenantID,
					"chat_id", payload.ChatID,
					"error", err,
				)
				// Kakao uses ephemeral action IDs — retry is futile.
				if event.Type != core.EventKakaoTalk {
					if qErr := s.store.EnqueueResponse(event.TenantID, string(event.Type), payload.ChatID, response); qErr != nil {
						slog.Error("channel event: enqueue response failed", "error", qErr)
					}
				}
			}
		}
	}
}

// deliverFamilyPush routes an EventFamilyPush to the target tenant's channel
// and bypasses the agent loop. The payload is a finished outbound message
// (Fanout.send already gave a skill author's hand-authored text), so we do
// not re-invoke the LLM — doing so would paraphrase, translate, or drop the
// message entirely depending on prompt context.
//
// Routing order:
//  1. Target tenant must exist + have at least one declared channel.
//  2. ChannelHint picks a specific channel type; fall back to Channels[0].
//  3. AdminChatIDs[0] is the destination chat; empty = log + drop (nowhere
//     to send).
//  4. If the channel is not currently running (hot-reload, post-restart),
//     enqueue to pending_responses so the retry loop can pick it up.
func (s *Server) deliverFamilyPush(ctx context.Context, event core.Event) {
	var p core.FanoutPayload
	if err := json.Unmarshal(event.Payload, &p); err != nil {
		slog.Warn("family_push: bad payload", "tenant", event.TenantID, "error", err)
		return
	}

	target := s.tenantRegistry.Get(event.TenantID)
	if target == nil || target.Config == nil {
		slog.Warn("family_push: unknown target tenant", "tenant", event.TenantID)
		return
	}
	if len(target.Config.Channels) == 0 {
		slog.Warn("family_push: target has no channels configured; dropping",
			"tenant", event.TenantID)
		return
	}

	channelType := resolveFamilyPushChannel(target.Config.Channels, p.ChannelHint)

	chatID := ""
	if len(target.Config.AdminChatIDs) > 0 {
		chatID = target.Config.AdminChatIDs[0]
	}
	if chatID == "" {
		slog.Warn("family_push: target has no admin chat; dropping",
			"tenant", event.TenantID, "channel", channelType)
		return
	}

	ch, chOK := s.spawner.GetChannel(event.TenantID, channelType, "")
	if !chOK {
		s.enqueueFamilyPushForRetry(event.TenantID, channelType, chatID, p.Text, "channel not running")
		return
	}

	if err := ch.SendResponse(ctx, chatID, p.Text); err != nil {
		s.enqueueFamilyPushForRetry(event.TenantID, channelType, chatID, p.Text,
			fmt.Sprintf("send failed: %v", err))
		return
	}

	slog.Info("family_push_delivered",
		"from", "family", "to", event.TenantID, "channel", channelType, "chat_id", chatID)
}

// enqueueFamilyPushForRetry parks an undelivered family push in pending_responses
// so the retry loop can pick it up after the channel comes back. Kakao is
// excluded because its action IDs are ephemeral — by the time the retry fires,
// the originating action no longer exists, so re-sending would 4xx-loop forever.
func (s *Server) enqueueFamilyPushForRetry(tenantID string, channelType core.EventType, chatID, text, reason string) {
	slog.Warn("family_push: deferred to retry queue",
		"tenant", tenantID, "channel", channelType, "reason", reason)
	if channelType == core.EventKakaoTalk {
		return
	}
	if qErr := s.store.EnqueueResponse(tenantID, string(channelType), chatID, text); qErr != nil {
		slog.Error("family_push: enqueue failed", "tenant", tenantID, "channel", channelType, "error", qErr)
	}
}

// resolveFamilyPushChannel picks which target channel a FamilyPush lands on.
// Hint matching is exact on the ChannelType string ("telegram", "slack",
// "kakao_talk"); a miss falls back to the first persistent push channel so
// delivery degrades instead of dropping.
//
// web_chat is excluded from the fallback (but honored if explicitly hinted
// — caller's explicit ask wins). web_chat is per-WebSocket-session: there
// is no durable destination to push to in the background, so silently
// landing every "no hint" family push on it would simply discard the
// message. Persistent channels (telegram/slack/discord/kakao_talk) own
// their own queueing semantics and are safe defaults.
func resolveFamilyPushChannel(channels []core.ChannelConfig, hint string) core.EventType {
	if hint != "" {
		for _, c := range channels {
			if string(c.ChannelType) == hint {
				return c.ChannelType.ToEventType()
			}
		}
	}
	for _, c := range channels {
		if c.ChannelType == core.ChannelWeb {
			continue
		}
		return c.ChannelType.ToEventType()
	}
	// Only web_chat configured — return it so the caller's "no channel
	// running" branch can enqueue to pending_responses rather than crashing.
	return channels[0].ChannelType.ToEventType()
}

// permissionTimeout returns the configured permission timeout duration.
func (s *Server) permissionTimeout() time.Duration {
	s.configMu.RLock()
	secs := s.config.Permissions.TimeoutSeconds
	s.configMu.RUnlock()
	if secs <= 0 {
		secs = 120
	}
	return time.Duration(secs) * time.Second
}

// logPermissionEvent records a permission decision to the audit log.
func (s *Server) logPermissionEvent(decision, channelType, chatID, desc, resource string) {
	if err := s.store.LogPermissionEvent(decision, channelType, chatID, desc, resource); err != nil {
		slog.Warn("permission audit log failed", "error", err)
	}
}

// retryPendingResponses periodically retries failed response deliveries.
// Uses no-drop semantics: if a channel is absent (e.g., mid-ReplaceSpawn),
// the response stays in the queue for the next tick.
func (s *Server) retryPendingResponses(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(1 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pending, err := s.store.DequeuePendingResponses(10)
			if err != nil {
				slog.Warn("retry: dequeue failed", "error", err)
				continue
			}
			for _, p := range pending {
				tenantID := p.TenantID
				if tenantID == "" {
					// Pre-migration rows: safe to route to default ONLY while
					// the daemon is single-tenant. Once a second tenant is
					// registered, an empty tenant_id is ambiguous and could
					// leak across the privacy boundary (spec C1) — drop it
					// instead of guessing. Uses MarkResponseDelivered for
					// cleanup until a dedicated dropped-audit table is
					// introduced (Plan B).
					if len(s.tenants.Sessions()) > 1 {
						slog.Warn("retry: PERMANENTLY dropping pending row with empty tenant_id (C1 privacy guard)",
							"id", p.ID, "chat_id", p.ChatID, "tenants", len(s.tenants.Sessions()))
						_ = s.store.MarkResponseDelivered(p.ID)
						continue
					}
					tenantID = DefaultTenantID
				}
				ch, ok := s.spawner.GetChannel(tenantID, core.EventType(p.EventType), "")
				if !ok {
					// Channel absent — do NOT drop. Leave in queue for next tick.
					continue
				}
				if err := ch.SendResponse(ctx, p.ChatID, p.Response); err != nil {
					slog.Warn("retry: send failed",
						"id", p.ID, "retry", p.RetryCount, "error", err)
					if kept, rErr := s.store.IncrementResponseRetry(p.ID); rErr != nil {
						slog.Error("retry: increment failed", "id", p.ID, "error", rErr)
					} else if !kept {
						slog.Warn("retry: max retries exceeded, dropping", "id", p.ID)
					}
				} else {
					slog.Info("retry: delivered pending response",
						"id", p.ID, "chat_id", p.ChatID)
					_ = s.store.MarkResponseDelivered(p.ID)
				}
			}
		case <-cleanupTicker.C:
			if n, err := s.store.CleanupExpiredResponses(24); err != nil {
				slog.Warn("retry: cleanup failed", "error", err)
			} else if n > 0 {
				slog.Info("retry: cleaned up expired responses", "count", n)
			}
		}
	}
}

// ListenAndServe starts the HTTP server and scheduler, blocking until a
// SIGINT or SIGTERM triggers graceful shutdown of both.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Cancelable context for the scheduler goroutine.
	schedCtx, schedCancel := context.WithCancel(context.Background())
	go s.scheduler.Start(schedCtx)

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-done
		slog.Info("shutting down server")

		// Stop all channels first (parallel cancel + wait).
		if s.spawner != nil {
			s.spawner.StopAll()
		}

		// Stop scheduler tick loop and cancel context, then wait for
		// in-flight skill goroutines to drain before shutting down HTTP.
		s.scheduler.Stop()
		schedCancel()
		s.scheduler.Wait()

		// Close MCP server connections (CommandTransport handles 5s → SIGTERM).
		if s.session.McpRegistry != nil {
			s.session.McpRegistry.Shutdown()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	slog.Info("server listening", "addr", addr)
	return srv.ListenAndServe()
}
