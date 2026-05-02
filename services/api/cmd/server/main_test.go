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
	cfg.JWTSecret = "test-secret"
	r, cleanup := NewRouter(cfg, nil, nil, nil)
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
}

// TestDiscoveryReturnsKakaoRelayURL pins the new discovery contract:
// the /discovery endpoint must return the Kakao relay URL under the
// "kakao_relay_url" key (renamed from "relay_url"), and the legacy key
// must NOT appear. The kittypaw daemon now reads kakao_relay_url only.
func TestDiscoveryReturnsKakaoRelayURL(t *testing.T) {
	cfg := config.LoadForTest()
	cfg.JWTSecret = "test-secret"
	cfg.KakaoRelayURL = "https://kakao.kittypaw.app"
	r, cleanup := NewRouter(cfg, nil, nil, nil)
	t.Cleanup(cleanup)

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

// TestDiscoveryReturnsChatRelayURL pins the chat_relay_url discovery key.
// Same daemon-outbound-WSS pattern as kakao_relay_url — chat.kittypaw.app
// is the remote relay control plane (per kittypaw spec
// 2026-04-30-remote-relay-control-plane-design.md).
func TestDiscoveryReturnsChatRelayURL(t *testing.T) {
	cfg := config.LoadForTest()
	cfg.JWTSecret = "test-secret"
	cfg.ChatRelayURL = "https://chat.kittypaw.app"
	r, cleanup := NewRouter(cfg, nil, nil, nil)
	t.Cleanup(cleanup)

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
	if got := body["chat_relay_url"]; got != "https://chat.kittypaw.app" {
		t.Fatalf("expected chat_relay_url=https://chat.kittypaw.app, got %q", got)
	}
}

// TestDiscoveryReturnsAuthBaseURL pins Plan 13's auth_base_url derive logic.
// auth_base_url = BaseURL + "/auth" (trailing slash on BaseURL must be
// trimmed — Plan 13 R6). This is the only key whose value is *derived*
// from server config rather than a separate env var, so changes to the
// derive logic must surface here.
func TestDiscoveryReturnsAuthBaseURL(t *testing.T) {
	cfg := config.LoadForTest()
	cfg.JWTSecret = "test-secret"
	cfg.BaseURL = "http://localhost:8080/" // trailing slash — TrimRight defends
	r, cleanup := NewRouter(cfg, nil, nil, nil)
	t.Cleanup(cleanup)

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
	if got := body["auth_base_url"]; got != "http://localhost:8080/auth" {
		t.Fatalf("expected auth_base_url=http://localhost:8080/auth (no double slash), got %q", got)
	}
}

// TestNewRouter_CleanupReleasesStores pins the (router, cleanup) contract
// added in Plan 19. cleanup must be non-nil and idempotent — each underlying
// store guards close(stop) with sync.Once so a panic-recovery path or a
// duplicated cleanup hook can't trip "close of closed channel". Goroutine
// leak detection itself is out of scope for a unit test.
func TestNewRouter_CleanupReleasesStores(t *testing.T) {
	cfg := config.LoadForTest()
	cfg.JWTSecret = "test-secret"
	r, cleanup := NewRouter(cfg, nil, nil, nil)
	if r == nil {
		t.Fatal("expected non-nil router")
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	// Two calls must be safe — Phase 2 sync.Once contract on each store.
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

// TestAlmanacRouteWiredWithRateLimit confirms the new /v1/almanac/... route
// is registered and inherits the global rate limiter (anon = 5/min).
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

// TestRouter_TrueClientIPHeaderDoesNotBypassRateLimit pins the rate-limit
// key against client-supplied headers. chi.middleware.RealIP trusted the
// True-Client-IP / X-Real-IP / X-Forwarded-For headers and overwrote
// r.RemoteAddr — but standard nginx proxy_params only override X-Real-IP
// (it appends to X-Forwarded-For and ignores True-Client-IP), leaving the
// attacker-supplied value at index 0 and letting them rotate the rate-limit
// key per request. The fix: trust only X-Real-IP (which nginx canonically
// overrides) and otherwise fall back to the actual TCP peer (r.RemoteAddr).
func TestRouter_TrueClientIPHeaderDoesNotBypassRateLimit(t *testing.T) {
	r := testRouter(t)

	const url = "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01"
	const peer = "192.0.2.71:1234"

	// Five anonymous calls from the same TCP peer, each rotating the
	// True-Client-IP header — would defeat the limit if the middleware
	// trusted the header.
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

	// 6th call from the same peer (rotated header again) must trip the
	// limiter — the rate-limit key follows the TCP peer, not the header.
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.RemoteAddr = peer
	req.Header.Set("True-Client-IP", "198.51.100.99")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th call (True-Client-IP rotation must not bypass), got %d", w.Code)
	}
}

// TestRouter_XForwardedForHeaderDoesNotBypassRateLimit pins the same
// guarantee for the X-Forwarded-For header. nginx proxy_params APPENDS the
// real peer to any client-supplied X-Forwarded-For, leaving the attacker
// value at index 0 — which chi.RealIP took as the canonical IP.
func TestRouter_XForwardedForHeaderDoesNotBypassRateLimit(t *testing.T) {
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
		t.Fatalf("expected 429 on 6th call (X-Forwarded-For rotation must not bypass), got %d", w.Code)
	}
}

// TestRouter_XRealIPHeaderTrustedForRateLimit pins the dual side: the
// X-Real-IP header IS trusted because nginx canonically overrides it
// (proxy_set_header X-Real-IP $remote_addr;). Without this, all anonymous
// traffic behind nginx would share the loopback IP key and trip the limit
// after 5 total requests across the entire user base.
func TestRouter_XRealIPHeaderTrustedForRateLimit(t *testing.T) {
	r := testRouter(t)

	const url = "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01"
	const nginxPeer = "127.0.0.1:8443"

	// Five distinct end users (different X-Real-IP) all coming through the
	// same nginx loopback — each must get their own bucket.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = nginxPeer
		req.Header.Set("X-Real-IP", fmt.Sprintf("198.51.100.%d", i+1))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("user %d throttled — distinct X-Real-IP must get distinct buckets", i+1)
		}
	}

	// And the 6th request from the SAME X-Real-IP must trip the limit
	// (proves we actually used the header, not just ignored it). The
	// outer loop above already populated 5 distinct user buckets; here
	// we top up bucket 198.51.100.42 with 5 hits, then assert the 6th.
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

// TestWeatherRouteWiredWithRateLimit confirms the new /v1/weather/kma/...
// route is registered and inherits the global rate limiter (anon = 5/min).
// The handler will return 502 (no upstream/key) for the first calls — what
// we care about is that the 6th request short-circuits with 429.
func TestWeatherRouteWiredWithRateLimit(t *testing.T) {
	r := testRouter(t)

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
