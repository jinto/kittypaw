package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestHandleHealth(t *testing.T) {
	srv := &Server{config: &core.Config{}}
	router := srv.setupRoutes()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestHandleHealthNoAuth(t *testing.T) {
	cfg := &core.Config{}
	cfg.Server.APIKey = "secret-key"
	srv := &Server{config: cfg}
	router := srv.setupRoutes()

	// /health should work without API key even when API key is configured.
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without API key, got %d", w.Code)
	}
}
