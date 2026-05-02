package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittyapi/internal/config"
)

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.LoadForTest()
	r, cleanup := NewRouter(cfg, nil)
	t.Cleanup(cleanup)
	return r
}

func TestHealthEndpoint(t *testing.T) {
	r := testRouter(t)

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
	if body["version"] == "" {
		t.Fatalf("expected non-empty version in health body: %v", body)
	}
	if body["commit"] == "" {
		t.Fatalf("expected non-empty commit in health body: %v", body)
	}
}

func TestAPIDoesNotServePortalRoutes(t *testing.T) {
	r := testRouter(t)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/discovery"},
		{method: http.MethodGet, path: "/.well-known/jwks.json"},
		{method: http.MethodGet, path: "/auth/google"},
		{method: http.MethodPost, path: "/auth/devices/refresh"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", w.Code)
			}
		})
	}
}

func TestNewRouterCleanupReleasesStores(t *testing.T) {
	cfg := config.LoadForTest()
	r, cleanup := NewRouter(cfg, nil)
	if r == nil {
		t.Fatal("expected non-nil router")
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	cleanup()
	cleanup()
}

func TestNotFound(t *testing.T) {
	r := testRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAlmanacRouteWiredWithRateLimit(t *testing.T) {
	r := testRouter(t)

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
			t.Fatalf("call #%d hit 404; almanac route not wired", i+1)
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

func TestRouterTrueClientIPHeaderDoesNotBypassRateLimit(t *testing.T) {
	r := testRouter(t)

	const url = "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01"
	const peer = "192.0.2.71:1234"

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = peer
		req.Header.Set("True-Client-IP", fmt.Sprintf("198.51.100.%d", i+1))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("call #%d unexpectedly throttled: %d", i+1, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.RemoteAddr = peer
	req.Header.Set("True-Client-IP", "198.51.100.99")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th call, got %d", w.Code)
	}
}

func TestRouterXForwardedForHeaderDoesNotBypassRateLimit(t *testing.T) {
	r := testRouter(t)

	const url = "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01"
	const peer = "192.0.2.72:1234"

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d, 10.0.0.1", i+1))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("call #%d unexpectedly throttled: %d", i+1, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.RemoteAddr = peer
	req.Header.Set("X-Forwarded-For", "198.51.100.99, 10.0.0.1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th call, got %d", w.Code)
	}
}

func TestRouterXRealIPHeaderTrustedForRateLimit(t *testing.T) {
	r := testRouter(t)

	const url = "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01"
	const nginxPeer = "127.0.0.1:8443"

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = nginxPeer
		req.Header.Set("X-Real-IP", fmt.Sprintf("198.51.100.%d", i+1))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("user %d throttled", i+1)
		}
	}

	const samePeer = "198.51.100.42"
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = nginxPeer
		req.Header.Set("X-Real-IP", samePeer)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.RemoteAddr = nginxPeer
	req.Header.Set("X-Real-IP", samePeer)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th X-Real-IP=%s call, got %d", samePeer, w.Code)
	}
}

func TestWeatherRouteWiredWithRateLimit(t *testing.T) {
	r := testRouter(t)

	const url = "/v1/weather/kma/village-fcst?lat=37.5665&lon=126.978"
	const peer = "192.0.2.42:1234"

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = peer
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("call #%d unexpectedly throttled: %d", i+1, w.Code)
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
