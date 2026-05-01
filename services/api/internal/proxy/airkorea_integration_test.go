//go:build air_integration

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

// TestAirKorea_LiveAPI hits real AirKorea (한국환경공단) API when
// AIRKOREA_API_KEY is set. Smoke 3-layer L1.B (sibling of Plan 8 L1.A).
//
// Same mechanism as L1.A: HTTP 200 + envelope `response.header.resultCode == "00"`.
// AirKorea also uses `returnType=json` (airkorea.go:95) — same quirk class as
// the holiday _type=json regression (commit 3688453). Integration test catches
// silent XML fallback if data.go.kr migrates the parameter convention.
//
// Build tag `air_integration` isolates from DB-backed model tests.
// Run with `make test-integration-air`.
func TestAirKorea_LiveAPI(t *testing.T) {
	key := os.Getenv("AIRKOREA_API_KEY")
	if key == "" {
		t.Skipf("AIRKOREA_API_KEY not set — skipping live AirKorea call")
	}

	c := cache.New()
	defer c.Close()
	h := &proxy.AirKoreaHandler{
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

	t.Run("RealtimeByCity sidoName=서울", func(t *testing.T) {
		body, code := call(h.RealtimeByCity(), "/v1/air/realtime/city?sidoName=%EC%84%9C%EC%9A%B8")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertAirEnvelopeOK(t, body)
	})

	t.Run("RealtimeByStation stationName=종로구&dataTerm=DAILY", func(t *testing.T) {
		body, code := call(h.RealtimeByStation(), "/v1/air/realtime/station?stationName=%EC%A2%85%EB%A1%9C%EA%B5%AC&dataTerm=DAILY")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertAirEnvelopeOK(t, body)
	})

	t.Run("Forecast informCode=PM10", func(t *testing.T) {
		body, code := call(h.Forecast(), "/v1/air/forecast?informCode=PM10")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertAirEnvelopeOK(t, body)
	})

	t.Run("WeeklyForecast", func(t *testing.T) {
		body, code := call(h.WeeklyForecast(), "/v1/air/forecast/weekly")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertAirEnvelopeOK(t, body)
	})

	t.Run("UnhealthyStations", func(t *testing.T) {
		body, code := call(h.UnhealthyStations(), "/v1/air/unhealthy")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertAirEnvelopeOK(t, body)
	})
}

// assertAirEnvelopeOK validates response.header.resultCode == "00".
// AirKorea's body.items can be either object or array — RawMessage avoids
// the polymorphism issue. Skipf on rate-limit; Fatalf on any other failure.
func assertAirEnvelopeOK(t *testing.T, body []byte) {
	t.Helper()
	var probe struct {
		Response struct {
			Header struct {
				ResultCode string `json:"resultCode"`
				ResultMsg  string `json:"resultMsg"`
			} `json:"header"`
			Body struct {
				Items json.RawMessage `json:"items"`
			} `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("AC2: malformed envelope: %v (body=%s)", err, body)
	}
	switch rc := probe.Response.Header.ResultCode; rc {
	case "00":
	case "22", "99", "LIMITED_NUMBER_OF_SERVICE_REQUESTS_EXCEEDS_ERROR":
		t.Skipf("AC2: AirKorea rate-limited (resultCode=%s)", rc)
	default:
		t.Fatalf("AC2: non-success resultCode=%s msg=%s", rc, probe.Response.Header.ResultMsg)
	}
}
