package server

import (
	"context"
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
	"github.com/jinto/kittypaw/llm"
	mcpreg "github.com/jinto/kittypaw/mcp"
	"github.com/jinto/kittypaw/sandbox"
	"github.com/jinto/kittypaw/store"
)

// Server is the HTTP/WebSocket gateway that bridges REST clients and browsers
// to the agent engine. It owns the chi router, the engine session, the
// scheduler, channel spawner, tenant router, and all handler state.
type Server struct {
	config     *core.Config
	configMu   sync.RWMutex // protects config during hot-reload
	store      *store.Store
	session    *engine.Session // default-tenant session; HTTP handlers use this
	scheduler  *engine.Scheduler
	router     chi.Router
	spawner    *ChannelSpawner // manages channel lifecycle for hot-reload
	tenants    *TenantRouter   // routes channel events to tenant-scoped sessions
	eventCh    chan core.Event // shared event channel between channels and dispatch loop
	version    string
	pkgManager *core.PackageManager // shared package manager for API handlers
	secrets    *core.SecretsStore   // shared secrets store for API handlers
}

// DefaultTenantID is the tenant ID used when the daemon runs without an
// explicit tenants/ directory (legacy single-tenant deployments).
const DefaultTenantID = "default"

// New wires together all dependencies and returns a ready-to-serve Server.
// mcpReg may be nil when no MCP servers are configured.
// Call StartChannels before ListenAndServe to activate messaging channels.
func New(cfg *core.Config, st *store.Store, provider llm.Provider, fallback llm.Provider, sb *sandbox.Sandbox, mcpReg *mcpreg.Registry, version string) *Server {
	// Seed TOML-configured workspace paths into DB (idempotent).
	if len(cfg.Sandbox.AllowedPaths) > 0 {
		if err := st.SeedWorkspacesFromConfig(cfg.Sandbox.AllowedPaths); err != nil {
			slog.Error("seed workspaces from config", "error", err)
		}
	}

	cfgDir, err := core.ConfigDir()
	if err != nil {
		// ConfigDir only fails if $HOME is unset — catastrophic for the server.
		panic(fmt.Sprintf("fatal: config dir unavailable: %v", err))
	}

	// Initialize shared secrets + package manager (before session so PM is available).
	secrets, secretsErr := core.LoadSecretsFrom(filepath.Join(cfgDir, "secrets.json"))
	if secretsErr != nil {
		slog.Warn("failed to load secrets store, package config will be limited", "error", secretsErr)
	}
	pkgMgr := core.NewPackageManagerFrom(cfgDir, secrets)

	apiTokenMgr := core.NewAPITokenManager(cfgDir, secrets)

	session := &engine.Session{
		Provider:         provider,
		FallbackProvider: fallback,
		Sandbox:          sb,
		Store:            st,
		Config:           cfg,
		McpRegistry:      mcpReg,
		BaseDir:          cfgDir,
		PackageManager:   pkgMgr,
		APITokenMgr:      apiTokenMgr,
	}
	if err := session.RefreshAllowedPaths(); err != nil {
		slog.Warn("startup: failed to load workspace paths, file access denied by default", "error", err)
	}

	// Create workspace indexer and trigger initial background indexing.
	indexer := engine.NewFTS5Indexer(st)
	session.Indexer = indexer
	go func() {
		wss, err := st.ListWorkspaces()
		if err != nil {
			slog.Warn("startup: failed to list workspaces for indexing", "error", err)
			return
		}
		for _, ws := range wss {
			if _, err := indexer.Index(context.Background(), ws.ID, ws.RootPath); err != nil {
				slog.Warn("startup: workspace indexing failed",
					"workspace_id", ws.ID, "root_path", ws.RootPath, "error", err)
			}
		}
	}()

	tenants := NewTenantRouter()
	tenants.Register(DefaultTenantID, session)

	s := &Server{
		config:     cfg,
		store:      st,
		session:    session,
		scheduler:  engine.NewScheduler(session, engine.NewSharedBudget(cfg.Features.DailyTokenLimit), pkgMgr),
		tenants:    tenants,
		eventCh:    make(chan core.Event, 64),
		version:    version,
		pkgManager: pkgMgr,
		secrets:    secrets,
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

// StartChannels creates the ChannelSpawner, reconciles the initial channel
// configs, and starts the dispatch and retry goroutines.
// Must be called before ListenAndServe.
//
// Before spawning any channel, StartChannels runs ValidateTenantChannels
// across every tenant's config so duplicate bot tokens / Kakao relay URLs
// surface as fatal log warnings at boot rather than silent update races
// at runtime (family-multi-tenant spec C3).
func (s *Server) StartChannels(ctx context.Context, configs []core.ChannelConfig) error {
	tenantChannels := map[string][]core.ChannelConfig{
		DefaultTenantID: configs,
	}
	if err := core.ValidateTenantChannels(tenantChannels); err != nil {
		return fmt.Errorf("channel config validation: %w", err)
	}

	s.spawner = NewChannelSpawner(ctx, s.eventCh)
	if err := s.spawner.Reconcile(DefaultTenantID, configs); err != nil {
		slog.Warn("initial channel reconcile: some channels failed", "error", err)
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

			slog.Info("processing channel event",
				"type", event.Type,
				"tenant", event.TenantID,
				"chat_id", payload.ChatID,
				"from", payload.FromName,
			)

			// Build RunOptions with Confirmer-based permission callback if available.
			var runOpts *engine.RunOptions
			ch, chOK := s.spawner.GetChannel(event.TenantID, event.Type)
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

			response, err := session.Run(ctx, event, runOpts)
			if err != nil {
				slog.Error("channel event: engine error",
					"type", event.Type,
					"tenant", event.TenantID,
					"chat_id", payload.ChatID,
					"error", err,
				)
				continue
			}

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
				ch, ok := s.spawner.GetChannel(tenantID, core.EventType(p.EventType))
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
