package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Config struct {
	Version       string
	DaemonHandler interface {
		Routes() http.Handler
	}
	OpenAIHandler interface {
		Routes() http.Handler
	}
}

func NewRouter(cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		version := cfg.Version
		if version == "" {
			version = "dev"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "healthy",
			"version": version,
		})
	})
	if cfg.OpenAIHandler != nil {
		r.Mount("/", cfg.OpenAIHandler.Routes())
	}
	if cfg.DaemonHandler != nil {
		r.Mount("/daemon", cfg.DaemonHandler.Routes())
	}
	return r
}
