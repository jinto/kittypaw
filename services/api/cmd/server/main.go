package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/jinto/kittypaw-api/internal/auth"
	"github.com/jinto/kittypaw-api/internal/cache"
	"github.com/jinto/kittypaw-api/internal/config"
	"github.com/jinto/kittypaw-api/internal/model"
	"github.com/jinto/kittypaw-api/internal/proxy"
	"github.com/jinto/kittypaw-api/internal/ratelimit"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := model.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	userStore := model.NewUserStore(pool)
	refreshStore := model.NewRefreshTokenStore(pool)

	r := NewRouter(cfg, userStore, refreshStore)

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func NewRouter(cfg *config.Config, userStore model.UserStore, refreshStore model.RefreshTokenStore) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "Retry-After", "Warning"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Auth middleware — sets *User in context (nil for anonymous).
	authMW := auth.Middleware(cfg.JWTSecret, userStore)
	r.Use(authMW)

	// Rate limiting — after auth MW so it knows if user is authenticated.
	limiter := ratelimit.New()
	r.Use(ratelimit.Middleware(limiter))

	// OAuth handler.
	states := auth.NewStateStore()
	oauthHandler := &auth.OAuthHandler{
		UserStore:         userStore,
		RefreshTokenStore: refreshStore,
		StateStore:        states,
		JWTSecret:         cfg.JWTSecret,
		HTTPClient:        &http.Client{Timeout: 10 * time.Second},
	}

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
	airHandler := &proxy.AirHandler{
		Cache:      dataCache,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}

	// Routes.
	r.Get("/health", handleHealth)

	r.Route("/auth", func(r chi.Router) {
		r.Get("/google", oauthHandler.HandleGoogleLogin(googleCfg))
		r.Get("/google/callback", oauthHandler.HandleGoogleCallback(googleCfg))
		r.Get("/github", oauthHandler.HandleGitHubLogin(githubCfg))
		r.Get("/github/callback", oauthHandler.HandleGitHubCallback(githubCfg))
		r.Post("/token/refresh", oauthHandler.HandleTokenRefresh())
		r.Get("/me", auth.HandleMe)
	})

	r.Get("/v1/air", airHandler.ServeHTTP)

	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}
