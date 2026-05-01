//go:build integration

package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jinto/kittypaw-api/internal/cache"
	"github.com/jinto/kittypaw-api/internal/proxy"
)

// TestVillageForecast_LiveKMA hits the real KMA upstream when WEATHER_API_KEY
// is provided. Skipped automatically in CI (key absent) so the build stays
// hermetic; run via `make test-integration` after the operator provisions
// the data.go.kr 단기예보 service key.
func TestVillageForecast_LiveKMA(t *testing.T) {
	key := os.Getenv("WEATHER_API_KEY")
	if key == "" {
		t.Skipf("WEATHER_API_KEY not set — skipping live KMA call")
	}

	c := cache.New()
	defer c.Close()
	h := &proxy.WeatherHandler{
		Cache:      c,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     key,
	}

	// Seoul City Hall coordinates.
	req := httptest.NewRequest(http.MethodGet, "/v1/weather/kma/village-fcst?lat=37.5665&lon=126.978", nil)
	w := httptest.NewRecorder()
	h.VillageForecast().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("live call expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Sanity check — response must contain the KMA envelope.
	var probe struct {
		Response struct {
			Body struct {
				Items struct {
					Item []map[string]any `json:"item"`
				} `json:"items"`
			} `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &probe); err != nil {
		t.Fatalf("decode KMA body: %v", err)
	}
	if len(probe.Response.Body.Items.Item) == 0 {
		t.Errorf("expected at least one forecast item, got empty list")
	}
}

// TestUltraShort_LiveKMA covers KMA's UltraShortNowcast (실황) and
// UltraShortForecast (초단기예보) — same envelope as VillageForecast,
// different paths. Smoke 3-layer L1.C addition (2 endpoint).
//
// Both handlers auto-compute base_date/base_time from current clock — caller
// supplies only lat/lon. Envelope-level check (resultCode=00 + items.item ≥ 1)
// matches the Plan 8 L1.A pattern; failure modes shared with KASI/holiday.
func TestUltraShort_LiveKMA(t *testing.T) {
	key := os.Getenv("WEATHER_API_KEY")
	if key == "" {
		t.Skipf("WEATHER_API_KEY not set — skipping live KMA call")
	}

	c := cache.New()
	defer c.Close()
	h := &proxy.WeatherHandler{
		Cache:      c,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     key,
	}

	call := func(handler http.HandlerFunc, target string) ([]byte, int) {
		exec := func() ([]byte, int) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			return w.Body.Bytes(), w.Code
		}
		body, code := exec()
		if code == http.StatusBadGateway {
			body, code = exec()
		}
		return body, code
	}

	t.Run("UltraShortNowcast 서울", func(t *testing.T) {
		body, code := call(h.UltraShortNowcast(), "/v1/weather/kma/ultra-srt-ncst?lat=37.5665&lon=126.978")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertKMAEnvelopeOK(t, body)
	})

	t.Run("UltraShortForecast 서울", func(t *testing.T) {
		body, code := call(h.UltraShortForecast(), "/v1/weather/kma/ultra-srt-fcst?lat=37.5665&lon=126.978")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertKMAEnvelopeOK(t, body)
	})
}

func assertKMAEnvelopeOK(t *testing.T, body []byte) {
	t.Helper()
	var probe struct {
		Response struct {
			Header struct {
				ResultCode string `json:"resultCode"`
				ResultMsg  string `json:"resultMsg"`
			} `json:"header"`
			Body struct {
				Items struct {
					Item []map[string]any `json:"item"`
				} `json:"items"`
			} `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("AC2: malformed envelope: %v (body=%s)", err, body)
	}
	switch rc := probe.Response.Header.ResultCode; rc {
	case "00":
	case "22", "99", "LIMITED_NUMBER_OF_SERVICE_REQUESTS_EXCEEDS_ERROR":
		t.Skipf("AC2: KMA rate-limited (resultCode=%s)", rc)
	default:
		t.Fatalf("AC2: non-success resultCode=%s msg=%s", rc, probe.Response.Header.ResultMsg)
	}
	if len(probe.Response.Body.Items.Item) == 0 {
		t.Fatalf("AC3: items.item empty for required endpoint")
	}
}
