package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinto/kittypaw-api/internal/cache"
	"github.com/jinto/kittypaw-api/internal/proxy"
)

func TestAirCacheMiss(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pm25":15}`))
	}))
	defer upstream.Close()

	c := cache.New()
	defer c.Close()

	h := &proxy.AirHandler{
		Cache:      c,
		HTTPClient: upstream.Client(),
		BaseURL:    upstream.URL,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/air?lat=37.57&lon=126.98", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != `{"pm25":15}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestAirCacheHit(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		_, _ = w.Write([]byte(`{"pm25":15}`))
	}))
	defer upstream.Close()

	c := cache.New()
	defer c.Close()

	h := &proxy.AirHandler{
		Cache:      c,
		HTTPClient: upstream.Client(),
		BaseURL:    upstream.URL,
	}

	// First request — fills cache.
	req := httptest.NewRequest(http.MethodGet, "/v1/air?lat=37.57&lon=126.98", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	upstreamCalled = false

	// Second request — should hit cache.
	req = httptest.NewRequest(http.MethodGet, "/v1/air?lat=37.57&lon=126.98", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if upstreamCalled {
		t.Fatal("expected no upstream call on cache hit")
	}
}

func TestAirUpstreamFailureWithStaleCache(t *testing.T) {
	c := cache.New()
	defer c.Close()

	// Pre-populate with stale data (very short TTL).
	c.Set("air:37.57,126.98", []byte(`{"pm25":10}`), 1)

	// Upstream that fails.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	h := &proxy.AirHandler{
		Cache:      c,
		HTTPClient: upstream.Client(),
		BaseURL:    upstream.URL,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/air?lat=37.57&lon=126.98", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (stale), got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Warning") != `110 - "Response is stale"` {
		t.Fatalf("expected Warning header, got %q", w.Header().Get("Warning"))
	}
}

func TestAirUpstreamFailureNoCache(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	c := cache.New()
	defer c.Close()

	h := &proxy.AirHandler{
		Cache:      c,
		HTTPClient: upstream.Client(),
		BaseURL:    upstream.URL,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/air?lat=37.57&lon=126.98", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestAirMissingParams(t *testing.T) {
	c := cache.New()
	defer c.Close()

	h := &proxy.AirHandler{
		Cache:      c,
		HTTPClient: http.DefaultClient,
	}

	tests := []struct {
		name string
		url  string
	}{
		{"no params", "/v1/air"},
		{"missing lon", "/v1/air?lat=37.57"},
		{"missing lat", "/v1/air?lon=126.98"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}
