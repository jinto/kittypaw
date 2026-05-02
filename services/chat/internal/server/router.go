package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed manual/*
var manualAssets embed.FS

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
	r.Get("/manual", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/manual/", http.StatusMovedPermanently)
	})
	r.Handle("/manual/*", http.StripPrefix("/manual/", manualHandler()))
	if cfg.OpenAIHandler != nil {
		r.Mount("/", cfg.OpenAIHandler.Routes())
	}
	if cfg.DaemonHandler != nil {
		r.Mount("/daemon", cfg.DaemonHandler.Routes())
	}
	return r
}

func manualHandler() http.Handler {
	sub, err := fs.Sub(manualAssets, "manual")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "manual assets unavailable", http.StatusInternalServerError)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "" || r.URL.Path == "index.html" {
			w.Header().Set("Cache-Control", "no-store")
		}
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
	})
}
