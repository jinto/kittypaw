package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinto/kittypaw-api/internal/cache"
	"github.com/jinto/kittypaw-api/internal/proxy"
)

// fixedNow returns a deterministic time so cache keys are stable across calls.
// 2026-04-30 06:00 KST → KMA base_time = 0500.
func fixedNow() time.Time {
	loc, _ := time.LoadLocation("Asia/Seoul")
	return time.Date(2026, 4, 30, 6, 0, 0, 0, loc)
}

func newWeatherHandler(upstreamURL string, client *http.Client) (*proxy.WeatherHandler, *cache.Cache) {
	c := cache.New()
	if client == nil {
		client = &http.Client{}
	}
	h := &proxy.WeatherHandler{
		Cache:      c,
		HTTPClient: client,
		APIKey:     "test-key",
		BaseURL:    upstreamURL,
		Now:        fixedNow,
	}
	return h, c
}

const kmaSuccessBody = `{"response":{"header":{"resultCode":"00","resultMsg":"NORMAL_SERVICE"},"body":{"items":{"item":[]}}}}`

// TestVillageForecast_HappyPathAndCache verifies the second request is served
// from cache (upstream call counter stays at 1).
func TestVillageForecast_HappyPathAndCache(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kmaSuccessBody))
	}))
	defer upstream.Close()

	h, c := newWeatherHandler(upstream.URL, nil)
	defer c.Close()

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/weather/kma/village-fcst?lat=37.5665&lon=126.978", nil)
		w := httptest.NewRecorder()
		h.VillageForecast().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("call #%d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("expected upstream hit count 1 (cache served second), got %d", got)
	}
}

// TestVillageForecast_ResultCodeError treats KMA's 200-OK + non-"00"
// resultCode as an upstream error (no caching).
func TestVillageForecast_ResultCodeError(t *testing.T) {
	body := `{"response":{"header":{"resultCode":"03","resultMsg":"NO_DATA"}}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()

	h, c := newWeatherHandler(upstream.URL, nil)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/weather/kma/village-fcst?lat=37.5665&lon=126.978", nil)
	w := httptest.NewRecorder()
	h.VillageForecast().ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on resultCode=03, got %d", w.Code)
	}
}

// TestVillageForecast_StaleFallback verifies upstream 503 falls back to
// the last cached body with a "Warning: 110" header.
func TestVillageForecast_StaleFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	h, c := newWeatherHandler(upstream.URL, nil)
	defer c.Close()

	// Pre-populate cache with stale data at the exact key the handler will
	// compute for fixedNow (06:00 KST → base_time 0500) and Seoul (60, 127).
	// TTL=1ns expires immediately; GetStale still returns it.
	staleKey := proxy.WeatherCacheKey(60, 127, "20260430", "0500")
	c.Set(staleKey, []byte(kmaSuccessBody), 1)

	req := httptest.NewRequest(http.MethodGet, "/v1/weather/kma/village-fcst?lat=37.5665&lon=126.978", nil)
	w := httptest.NewRecorder()
	h.VillageForecast().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from stale cache, got %d", w.Code)
	}
	if got := w.Header().Get("Warning"); got == "" {
		t.Errorf("expected Warning header on stale response, got none")
	}
}

// TestVillageForecast_Timeout fires a slow upstream and verifies the
// per-request context cancellation maps to 502 (no goroutine left behind).
func TestVillageForecast_Timeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(kmaSuccessBody))
	}))
	defer upstream.Close()

	client := &http.Client{Timeout: 50 * time.Millisecond}
	h, c := newWeatherHandler(upstream.URL, client)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/weather/kma/village-fcst?lat=37.5665&lon=126.978", nil)
	w := httptest.NewRecorder()
	h.VillageForecast().ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on timeout, got %d", w.Code)
	}
}

func TestVillageForecast_MissingParams(t *testing.T) {
	h, c := newWeatherHandler("", nil)
	defer c.Close()

	tests := []struct {
		name, url string
	}{
		{"no params", "/v1/weather/kma/village-fcst"},
		{"only lat", "/v1/weather/kma/village-fcst?lat=37.5"},
		{"only lon", "/v1/weather/kma/village-fcst?lon=127.0"},
		{"non-numeric lat", "/v1/weather/kma/village-fcst?lat=abc&lon=127"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			h.VillageForecast().ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestVillageForecast_OutOfPeninsula(t *testing.T) {
	h, c := newWeatherHandler("", nil)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/weather/kma/village-fcst?lat=0&lon=0", nil)
	w := httptest.NewRecorder()
	h.VillageForecast().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on out-of-peninsula, got %d", w.Code)
	}
}
