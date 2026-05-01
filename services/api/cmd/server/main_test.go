package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittyapi/internal/config"
)

func testRouter() http.Handler {
	cfg := config.LoadForTest()
	cfg.JWTSecret = "test-secret"
	return NewRouter(cfg, nil, nil, nil)
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

// TestDiscoveryReturnsKakaoRelayURL pins the new discovery contract:
// the /discovery endpoint must return the Kakao relay URL under the
// "kakao_relay_url" key (renamed from "relay_url"), and the legacy key
// must NOT appear. The kittypaw daemon now reads kakao_relay_url only.
func TestDiscoveryReturnsKakaoRelayURL(t *testing.T) {
	cfg := config.LoadForTest()
	cfg.JWTSecret = "test-secret"
	cfg.KakaoRelayURL = "https://kakao.kittypaw.app"
	r := NewRouter(cfg, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/discovery", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got := body["kakao_relay_url"]; got != "https://kakao.kittypaw.app" {
		t.Fatalf("expected kakao_relay_url=https://kakao.kittypaw.app, got %q", got)
	}
	if _, ok := body["relay_url"]; ok {
		t.Fatalf("legacy relay_url key must not be present in discovery response: %v", body)
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

// TestAlmanacRouteWiredWithRateLimit confirms the new /v1/almanac/... route
// is registered and inherits the global rate limiter (anon = 5/min).
func TestAlmanacRouteWiredWithRateLimit(t *testing.T) {
	r := testRouter()

	const url = "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01"
	const peer = "192.0.2.43:1234"

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = peer
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("call #%d unexpectedly throttled: %d", i+1, w.Code)
		}
		if w.Code == http.StatusNotFound {
			t.Fatalf("call #%d hit 404 — almanac route not wired", i+1)
		}
	}

	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.RemoteAddr = peer
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th call, got %d", w.Code)
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
