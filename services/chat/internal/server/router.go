package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed manual/* web/*
var staticAssets embed.FS

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
	r.Get("/", serveStaticFile("web/index.html"))
	r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusMovedPermanently)
	})
	r.Get("/app/", serveStaticFile("web/app.html"))
	r.Get("/auth/callback", serveStaticFile("web/auth-callback.html"))
	r.Handle("/assets/*", http.StripPrefix("/assets/", webAssetHandler()))
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

func serveStaticFile(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.ServeFileFS(w, r, staticAssets, name)
	}
}

func webAssetHandler() http.Handler {
	sub, err := fs.Sub(staticAssets, "web")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "assets unavailable", http.StatusInternalServerError)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" || strings.Contains(name, "/") || (!strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".css")) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
	})
}

func manualHandler() http.Handler {
	sub, err := fs.Sub(staticAssets, "manual")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "manual assets unavailable", http.StatusInternalServerError)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
	})
}
