package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinto/kittypaw-api/internal/config"
)

func testRouter() http.Handler {
	cfg := config.LoadForTest()
	cfg.JWTSecret = "test-secret"
	return NewRouter(cfg, nil, nil)
}

func TestHealthEndpoint(t *testing.T) {
	r := testRouter()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if body["status"] != "healthy" {
		t.Fatalf("expected status=healthy, got %q", body["status"])
	}
}

func TestNotFound(t *testing.T) {
	r := testRouter()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestWeatherRouteWiredWithRateLimit confirms the new /v1/weather/kma/...
// route is registered and inherits the global rate limiter (anon = 5/min).
// The handler will return 502 (no upstream/key) for the first calls — what
// we care about is that the 6th request short-circuits with 429.
func TestWeatherRouteWiredWithRateLimit(t *testing.T) {
	r := testRouter()

	const url = "/v1/weather/kma/village-fcst?lat=37.5665&lon=126.978"
	const peer = "192.0.2.42:1234"

	// First five anonymous calls land on the handler (any non-429 status is
	// fine — we don't have an upstream wired up).
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = peer
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("call #%d unexpectedly throttled: %d", i+1, w.Code)
		}
	}

	// Sixth call must trip the limiter.
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.RemoteAddr = peer
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th call, got %d", w.Code)
	}
}
