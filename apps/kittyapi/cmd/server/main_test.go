package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestServeHTTPListensOnUnixSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/private/tmp", "kittyapi-socket-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "kittyapi.sock")
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveHTTP(srv, socketPath)
	}()
	waitForSocket(t, socketPath)

	client := &http.Client{Transport: unixSocketTransport(socketPath)}
	resp, err := client.Get("http://unix/health")
	if err != nil {
		t.Fatalf("GET over unix socket: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serveHTTP returned %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed after shutdown, stat err=%v", err)
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

func waitForSocket(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created", socketPath)
}

func unixSocketTransport(socketPath string) http.RoundTripper {
	return &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
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
