package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var webFS embed.FS

// staticHandler serves embedded web assets with SPA fallback.
// Any path that doesn't match a real file serves index.html,
// enabling client-side routing.
func staticHandler() http.Handler {
	sub, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve root directly.
		if r.URL.Path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Try the exact file.
		name := r.URL.Path[1:] // strip leading /
		if _, err := fs.Stat(sub, name); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: rewrite to / and serve index.html.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
