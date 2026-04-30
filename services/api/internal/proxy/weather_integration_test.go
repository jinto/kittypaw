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
