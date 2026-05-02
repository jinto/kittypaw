//go:build calendar_integration

package proxy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/cache"
	"github.com/kittypaw-app/kittyapi/internal/proxy"
)

// TestHoliday_LiveKASI hits real KASI SpcdeInfoService when HOLIDAY_API_KEY
// is set. Direct trigger: commit 3688453 (_type=json regression — prod 502
// ~4 days). Pinned by L1.A AC1~AC5 in .claude/plans/smoke-3-layer.md.
//
// Build tag `calendar_integration` isolates from DB-backed model integration
// tests. Run with `make test-integration-calendar` (Plan 8 T2).
func TestHoliday_LiveKASI(t *testing.T) {
	key := os.Getenv("HOLIDAY_API_KEY")
	if key == "" {
		t.Skipf("HOLIDAY_API_KEY not set — skipping live KASI call")
	}

	c := cache.New()
	defer c.Close()
	h := &proxy.HolidayHandler{
		Cache:      c,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     key,
	}

	// One retry on 502 dampens transient KASI hiccups; double-fail = real bug.
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

	t.Run("Holidays solYear=2025 (envelope + 신정 골든)", func(t *testing.T) {
		body, code := call(h.Holidays(), "/v1/calendar/holidays?solYear=2025")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}

		var probe struct {
			Response struct {
				Header struct {
					ResultCode string `json:"resultCode"`
					ResultMsg  string `json:"resultMsg"`
				} `json:"header"`
				Body struct {
					Items struct {
						Item []struct {
							DateName string `json:"dateName"`
							Locdate  any    `json:"locdate"`
						} `json:"item"`
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
			t.Skipf("AC2: KASI rate-limited (resultCode=%s)", rc)
		default:
			t.Fatalf("AC2: non-success resultCode=%s msg=%s", rc, probe.Response.Header.ResultMsg)
		}

		if len(probe.Response.Body.Items.Item) == 0 {
			t.Fatalf("AC3: items.item empty for 2025 holidays")
		}

		var found bool
		for _, it := range probe.Response.Body.Items.Item {
			// KASI returns locdate as JSON number → float64 in `any`.
			// fmt.Sprint of float64(20250101) yields "2.0250101e+07" — use %.0f.
			var ld string
			switch v := it.Locdate.(type) {
			case float64:
				ld = fmt.Sprintf("%.0f", v)
			case string:
				ld = v
			}
			if ld == "20250101" && (it.DateName == "1월1일" || it.DateName == "신정") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AC4: 2025-01-01 (신정/1월1일) not found in holidays list")
		}
	})

	t.Run("Anniversaries solYear=2025 (envelope only)", func(t *testing.T) {
		body, code := call(h.Anniversaries(), "/v1/calendar/anniversaries?solYear=2025")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertEnvelopeOK(t, body)
	})

	t.Run("SolarTerms solYear=2025 (envelope only)", func(t *testing.T) {
		body, code := call(h.SolarTerms(), "/v1/calendar/solar-terms?solYear=2025")
		if code != http.StatusOK {
			t.Fatalf("AC1: expected 200, got %d: %s", code, body)
		}
		assertEnvelopeOK(t, body)
	})
}

// assertEnvelopeOK validates response.header.resultCode == "00" and items.item ≥ 1.
// Skipf on KASI rate-limit; Fatalf on any other non-success.
func assertEnvelopeOK(t *testing.T, body []byte) {
	t.Helper()
	var probe struct {
		Response struct {
			Header struct {
				ResultCode string `json:"resultCode"`
				ResultMsg  string `json:"resultMsg"`
			} `json:"header"`
			Body struct {
				Items struct {
					Item json.RawMessage `json:"item"`
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
		t.Skipf("AC2: KASI rate-limited (resultCode=%s)", rc)
	default:
		t.Fatalf("AC2: non-success resultCode=%s msg=%s", rc, probe.Response.Header.ResultMsg)
	}
	if len(probe.Response.Body.Items.Item) == 0 {
		t.Fatalf("AC3: items.item empty for required endpoint")
	}
}
