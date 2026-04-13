package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/engine"
	"github.com/jinto/gopaw/llm"
	mcpreg "github.com/jinto/gopaw/mcp"
	"github.com/jinto/gopaw/sandbox"
	"github.com/jinto/gopaw/store"
)

// Server is the HTTP/WebSocket gateway that bridges REST clients and browsers
// to the agent engine. It owns the chi router, the engine session, the
// scheduler, and all handler state.
type Server struct {
	config    *core.Config
	configMu  sync.RWMutex // protects config during hot-reload
	store     *store.Store
	session   *engine.Session
	scheduler *engine.Scheduler
	router    chi.Router
}

// New wires together all dependencies and returns a ready-to-serve Server.
// mcpReg may be nil when no MCP servers are configured.
func New(cfg *core.Config, st *store.Store, provider llm.Provider, fallback llm.Provider, sb *sandbox.Sandbox, mcpReg *mcpreg.Registry) *Server {
	session := &engine.Session{
		Provider:         provider,
		FallbackProvider: fallback,
		Sandbox:          sb,
		Store:            st,
		Config:           cfg,
		McpRegistry:      mcpReg,
	}

	s := &Server{
		config:    cfg,
		store:     st,
		session:   session,
		scheduler: engine.NewScheduler(session),
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

		// Memory
		r.Get("/memory/search", s.handleMemorySearch)

		// Users / identity
		r.Post("/users/link", s.handleUsersLink)
		r.Get("/users/{id}/identities", s.handleUsersIdentities)
		r.Delete("/users/{id}/identities/{channel}", s.handleUsersUnlink)
	})

	// WebSocket sits outside /api/v1 — auth is done via query param or header.
	r.HandleFunc("/ws", s.handleWebSocket)

	// Static web assets with SPA fallback — must be last (catch-all).
	r.Handle("/*", staticHandler())

	return r
}

// ProcessEvent runs a single event through the engine session and returns
// the agent response. This is used by the channel dispatch loop to bridge
// inbound channel messages to the agent engine.
func (s *Server) ProcessEvent(ctx context.Context, event core.Event) (string, error) {
	return s.session.Run(ctx, event, nil)
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
