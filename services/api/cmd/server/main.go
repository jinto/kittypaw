package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/cache"
	"github.com/kittypaw-app/kittyapi/internal/config"
	"github.com/kittypaw-app/kittyapi/internal/janitor"
	"github.com/kittypaw-app/kittyapi/internal/model"
	"github.com/kittypaw-app/kittyapi/internal/proxy"
	"github.com/kittypaw-app/kittyapi/internal/ratelimit"
)

// shutdownGrace is the time given to in-flight requests after SIGTERM/SIGINT
// before the server is force-closed. Long enough for slow upstream calls
// (e.g. KMA village-fcst with 15s client timeout) to finish, short enough
// that systemd does not escalate to SIGKILL (default TimeoutStopSec=90s).
const shutdownGrace = 30 * time.Second

func main() {
	initLogging()

	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

// initLogging wires slog as the default logger. JSON handler to stderr so
// systemd journal captures structured fields. Level from LOG_LEVEL env
// (debug|info|warn|error), defaults to info. Unknown values fall back
// to info AFTER an explicit warn — silent fallback masks operator typos
// like LOG_LEVEL=verbose or LOG_LEVEL=WARN (case-sensitive lookup).
func initLogging() {
	raw := strings.ToLower(os.Getenv("LOG_LEVEL"))
	level := slog.LevelInfo
	known := true
	switch raw {
	case "", "info":
		// default
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		known = false
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if !known {
		slog.Warn("unknown LOG_LEVEL — falling back to info", "value", raw)
	}
}

// run wires the config, DB, router, and HTTP server, blocking until either
// the listener errors or a SIGINT/SIGTERM arrives. On signal, it drains
// in-flight requests for shutdownGrace before returning.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// signal.NotifyContext cancels ctx on the first SIGINT or SIGTERM.
	// A second signal restores default behavior (process termination) —
	// this gives operators an escape hatch when graceful drain hangs.
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	// Defers below run in reverse order on return: cleanup() first
	// (closes sweep goroutines) → pool.Close() (DB pool) → stopSignals()
	// (release signal handler). Independent resources, but the order
	// keeps signal listening live until the last possible moment so
	// a second SIGTERM during drain still triggers immediate exit.
	defer stopSignals()

	pool, err := model.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer pool.Close()

	userStore := model.NewUserStore(pool)
	refreshStore := model.NewRefreshTokenStore(pool)
	placeStore := model.NewPlaceStore(pool)
	deviceStore := model.NewDeviceStore(pool)

	router, cleanup := NewRouter(cfg, userStore, refreshStore, deviceStore, placeStore)
	defer cleanup()

	// Credential lifecycle janitor — daily KST 04:00 sweep over devices
	// (idle reap + revoked retention) and refresh_tokens (expired
	// retention). Plan 24. Goroutine returns on ctx cancel; the in-flight
	// sweep (~ms-scale unless N rows hit batch caps) is allowed to drop
	// rather than block shutdown — next process restart picks up.
	go janitor.New(deviceStore, refreshStore, janitor.DefaultPolicy, nil).Run(ctx)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
		// ReadHeaderTimeout caps the slowloris attack window — clients
		// must finish sending headers within 10s. Body read timeouts
		// are per-handler (MaxBytesReader). WriteTimeout caps slow
		// readers stalling response goroutines (15s is the longest
		// per-handler upstream timeout, 30s gives one full retry).
		// IdleTimeout reaps keep-alive zombies before nginx upstream
		// connection cap is hit.
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining", "grace", shutdownGrace)
	}

	// Detach from the canceled root ctx so Shutdown gets its own deadline;
	// the server otherwise refuses to wait once the parent is done.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// NewRouter builds the HTTP router and returns it alongside a cleanup
// function that releases the four internal sweep goroutines (cache,
// state store, CLI code store, rate limiter). Callers MUST invoke
// cleanup when the server stops — main() defers it; tests use
// t.Cleanup. Without this hook the goroutines leak past process
// shutdown intent.
func NewRouter(cfg *config.Config, userStore model.UserStore, refreshStore model.RefreshTokenStore, deviceStore model.DeviceStore, placeStore model.PlaceStore) (*chi.Mux, func()) {
	r := chi.NewRouter()

	// chi.middleware.RealIP intentionally omitted — it trusts attacker-
	// rotatable headers (True-Client-IP / X-Forwarded-For). Trust model
	// lives in ratelimit.realIP() — see that function's doc comment.
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "Retry-After", "Warning"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// JWKS provider — the same single-key store backs /.well-known/
	// jwks.json publication AND user JWT verification (Plan 21 PR-B).
	// Built before middleware wiring so authMW can consume it.
	jwksProvider := auth.NewSingleKeyProvider(&cfg.JWTPrivateKey.PublicKey, cfg.JWTKID)

	// Auth middleware — sets *User in context (nil for anonymous).
	// audience pinned to AudienceAPI — user JWTs only. cross-audience
	// leak guard (device JWT with aud=chat) lives in auth.Verify.
	authMW := auth.Middleware(jwksProvider, auth.AudienceAPI, userStore)

	// Rate limiter (used after authMW).
	limiter := ratelimit.New()

	// OAuth handler — wired here (before route registration) so the
	// device refresh route can be registered Authorization-free below.
	states := auth.NewStateStore()
	oauthHandler := &auth.OAuthHandler{
		UserStore:         userStore,
		RefreshTokenStore: refreshStore,
		DeviceStore:       deviceStore,
		StateStore:        states,
		JWTPrivateKey:     cfg.JWTPrivateKey,
		JWTKID:            cfg.JWTKID,
		HTTPClient:        &http.Client{Timeout: 10 * time.Second},
	}

	// CLI OAuth for kittypaw login (HTTP callback + code-paste modes).
	cliCodes := auth.NewCLICodeStore()

	googleCfg := auth.GoogleConfig{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.BaseURL + "/auth/google/callback",
	}
	githubCfg := auth.GitHubConfig{
		ClientID:     cfg.GitHubClientID,
		ClientSecret: cfg.GitHubClientSecret,
		RedirectURL:  cfg.BaseURL + "/auth/github/callback",
	}

	// Data proxy.
	dataCache := cache.New()
	airKorea := &proxy.AirKoreaHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     cfg.AirKoreaAPIKey,
	}
	holiday := &proxy.HolidayHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     cfg.HolidayAPIKey,
	}
	weather := &proxy.WeatherHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     cfg.WeatherAPIKey,
	}
	almanac := &proxy.AlmanacHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     cfg.HolidayAPIKey,
	}
	places := &proxy.PlacesHandler{
		Store: placeStore,
	}

	// Service discovery — SDK reads this once on startup.
	// auth_base_url is derived from BaseURL because /auth/* is currently
	// hosted under the api host; future host split (auth.kittypaw.app)
	// only requires changing this single line.
	discovery := map[string]string{
		"api_base_url":        cfg.APIBaseURL,
		"auth_base_url":       strings.TrimRight(cfg.BaseURL, "/") + "/auth",
		"skills_registry_url": cfg.SkillsRegistryURL,
	}
	if cfg.KakaoRelayURL != "" {
		discovery["kakao_relay_url"] = cfg.KakaoRelayURL
	}
	if cfg.ChatRelayURL != "" {
		discovery["chat_relay_url"] = cfg.ChatRelayURL
	}

	// Routes split into two chi.Groups (Plan 23 PR-D 결정 3 + PR-D
	// follow-up review fix):
	//
	//   Group 1: ratelimit-only — device refresh. Sits outside authMW
	//     so a daemon's stale Authorization header (e.g. revoked device
	//     JWT with aud=chat) can't trip the user-aud middleware and 401
	//     before the handler ever runs. The opaque refresh token in
	//     the body is the credential. STILL gets ratelimit (anonymous
	//     IP-based key) — without this, the route is a credential-
	//     stuffing oracle + DB-pool DoS surface (review HIGH 0.95).
	//
	//   Group 2: authMW + rate-limit — every other route. Anonymous
	//     callers fall through (User=nil in context); auth'd callers
	//     get *User populated.
	//
	// chi requires Use() before any route registration on a mux, so
	// the split MUST be expressed via Groups (not via positional Use()).
	r.Group(func(r chi.Router) {
		// Per-route bucket "refresh:ip:<peer>" — isolated from the
		// shared anonymous bucket so noisy data-fetch IPs cannot
		// starve daemon refresh from the same source (Round 3
		// review MED 0.80 follow-up).
		r.Use(ratelimit.Middleware(limiter, "refresh"))
		r.Post("/auth/devices/refresh", oauthHandler.HandleDeviceRefresh())
	})

	r.Group(func(r chi.Router) {
		r.Use(authMW)
		r.Use(ratelimit.Middleware(limiter))

		r.Get("/health", handleHealth)
		r.Get("/.well-known/jwks.json", auth.HandleJWKS(jwksProvider))
		r.Get("/discovery", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(discovery); err != nil {
				slog.Error("encode discovery", "err", err)
			}
		})

		cliCfg := auth.CLILoginConfig{
			GoogleCfg: googleCfg,
			CodeStore: cliCodes,
			BaseURL:   cfg.BaseURL,
		}
		r.Route("/auth", func(r chi.Router) {
			r.Get("/google", oauthHandler.HandleGoogleLogin(googleCfg))
			r.Get("/google/callback", oauthHandler.HandleGoogleCallback(googleCfg))
			r.Get("/github", oauthHandler.HandleGitHubLogin(githubCfg))
			r.Get("/github/callback", oauthHandler.HandleGitHubCallback(githubCfg))
			r.Post("/token/refresh", oauthHandler.HandleTokenRefresh())
			r.Get("/me", auth.HandleMe)

			// CLI OAuth routes.
			r.Get("/cli/{provider}", oauthHandler.HandleCLILogin(cliCfg))
			r.Get("/cli/callback", oauthHandler.HandleCLICallback(cliCfg))
			r.Post("/cli/exchange", oauthHandler.HandleCLIExchange(cliCfg))

			// Device endpoints (Plan 23 PR-D). Refresh is registered
			// outside authMW above; pair/list/delete require user JWT.
			r.Post("/devices/pair", oauthHandler.HandlePair())
			r.Get("/devices", oauthHandler.HandleDevicesList())
			r.Delete("/devices/{id}", oauthHandler.HandleDeviceDelete())
		})

		r.Route("/v1/air/airkorea", func(r chi.Router) {
			r.Get("/realtime/station", airKorea.RealtimeByStation())
			r.Get("/realtime/city", airKorea.RealtimeByCity())
			r.Get("/forecast", airKorea.Forecast())
			r.Get("/forecast/weekly", airKorea.WeeklyForecast())
			r.Get("/unhealthy", airKorea.UnhealthyStations())
		})

		r.Route("/v1/calendar", func(r chi.Router) {
			r.Get("/holidays", holiday.Holidays())
			r.Get("/anniversaries", holiday.Anniversaries())
			r.Get("/solar-terms", holiday.SolarTerms())
		})

		r.Route("/v1/weather/kma", func(r chi.Router) {
			r.Get("/village-fcst", weather.VillageForecast())
			r.Get("/ultra-srt-ncst", weather.UltraShortNowcast())
			r.Get("/ultra-srt-fcst", weather.UltraShortForecast())
		})

		r.Route("/v1/almanac", func(r chi.Router) {
			r.Get("/lunar-date", almanac.LunarDate())
			r.Get("/solar-date", almanac.SolarDate())
			r.Get("/sun", almanac.Sun())
		})

		r.Route("/v1/geo", func(r chi.Router) {
			r.Get("/resolve", places.Resolve())
		})
	}) // close authMW + ratelimit Group

	cleanup := func() {
		dataCache.Close()
		states.Close()
		cliCodes.Close()
		limiter.Close()
	}
	return r, cleanup
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}
