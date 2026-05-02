//go:build integration

package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/cache"
	"github.com/kittypaw-app/kittyapi/internal/proxy"
)

// TestAlmanac_LiveKASI hits the real KASI upstream when HOLIDAY_API_KEY is
// provided. The same key serves three KASI services (SpcdeInfoService for
// holiday.go, plus LrsrCldInfoService and RiseSetInfoService for almanac).
//
// Golden cases pinned in plan v3:
//   - 양력 2026-05-01 ↔ 음력 2026-03-15 평달
//   - 서울 (37.5665, 126.9780) 2026-05-01 → sunrise=0537, sunset=1922
func TestAlmanac_LiveKASI(t *testing.T) {
	key := os.Getenv("HOLIDAY_API_KEY")
	if key == "" {
		t.Skipf("HOLIDAY_API_KEY not set — skipping live KASI call")
	}

	c := cache.New()
	defer c.Close()
	h := &proxy.AlmanacHandler{
		Cache:      c,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		APIKey:     key,
	}

	t.Run("LunarDate 양력 2026-05-01 → 음력 2026-03-15 평달", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01", nil)
		w := httptest.NewRecorder()
		h.LunarDate().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var probe struct {
			Response struct {
				Body struct {
					Items struct {
						Item struct {
							LunMonth     string `json:"lunMonth"`
							LunLeapmonth string `json:"lunLeapmonth"`
							LunDay       any    `json:"lunDay"` // int or string per KASI quirks
						} `json:"item"`
					} `json:"items"`
				} `json:"body"`
			} `json:"response"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &probe); err != nil {
			t.Fatalf("decode KASI body: %v", err)
		}
		item := probe.Response.Body.Items.Item
		if item.LunMonth != "03" {
			t.Errorf("expected lunMonth=03, got %q", item.LunMonth)
		}
		// lunDay can be int or string depending on KASI's mood
		if d, ok := item.LunDay.(float64); ok && int(d) != 15 {
			t.Errorf("expected lunDay=15, got %v", item.LunDay)
		} else if s, ok := item.LunDay.(string); ok && s != "15" {
			t.Errorf("expected lunDay=15, got %q", s)
		}
		if item.LunLeapmonth != "평" {
			t.Errorf("expected lunLeapmonth=평, got %q", item.LunLeapmonth)
		}
	})

	t.Run("Sun 서울 2026-05-01 → sunrise=0537, sunset=1922", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/almanac/sun?locdate=20260501&latitude=37.5665&longitude=126.9780", nil)
		w := httptest.NewRecorder()
		h.Sun().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var probe struct {
			Response struct {
				Body struct {
					Items struct {
						Item struct {
							Sunrise string `json:"sunrise"`
							Sunset  string `json:"sunset"`
						} `json:"item"`
					} `json:"items"`
				} `json:"body"`
			} `json:"response"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &probe); err != nil {
			t.Fatalf("decode KASI body: %v", err)
		}
		// KASI pads with trailing spaces ("0537  ").
		sunrise := strings.TrimSpace(probe.Response.Body.Items.Item.Sunrise)
		sunset := strings.TrimSpace(probe.Response.Body.Items.Item.Sunset)
		if sunrise != "0537" {
			t.Errorf("expected sunrise=0537, got %q", sunrise)
		}
		if sunset != "1922" {
			t.Errorf("expected sunset=1922, got %q", sunset)
		}
	})

	t.Run("SolarDate round-trip 음력 2026-03-15 → 양력 2026-05-01", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/almanac/solar-date?lunYear=2026&lunMonth=03&lunDay=15", nil)
		w := httptest.NewRecorder()
		h.SolarDate().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var probe struct {
			Response struct {
				Body struct {
					Items struct {
						Item struct {
							SolMonth string `json:"solMonth"`
							SolDay   any    `json:"solDay"`
						} `json:"item"`
					} `json:"items"`
				} `json:"body"`
			} `json:"response"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &probe); err != nil {
			t.Fatalf("decode KASI body: %v", err)
		}
		if probe.Response.Body.Items.Item.SolMonth != "05" {
			t.Errorf("expected solMonth=05, got %q", probe.Response.Body.Items.Item.SolMonth)
		}
	})
}
