package proxy

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jinto/kittypaw-api/internal/cache"
)

const airCacheTTL = 1 * time.Hour

type AirHandler struct {
	Cache      *cache.Cache
	HTTPClient *http.Client
	APIKey     string
	BaseURL    string // overridable for testing; default: 에어코리아 API
}

func (h *AirHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")

	if latStr == "" || lonStr == "" {
		http.Error(w, "lat and lon are required", http.StatusBadRequest)
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		http.Error(w, "invalid lat", http.StatusBadRequest)
		return
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		http.Error(w, "invalid lon", http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("air:%.2f,%.2f", lat, lon)

	// Try fresh cache.
	if data, ok := h.Cache.Get(key); ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
		return
	}

	// Fetch upstream.
	data, err := h.fetchUpstream(lat, lon)
	if err != nil {
		log.Printf("air upstream error: %v", err)

		// Stale-while-revalidate: return stale data if available.
		if staleData, stale, found := h.Cache.GetStale(key); found && stale {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Warning", `110 - "Response is stale"`)
			_, _ = w.Write(staleData)
			return
		}

		http.Error(w, "upstream service unavailable", http.StatusBadGateway)
		return
	}

	h.Cache.Set(key, data, airCacheTTL)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (h *AirHandler) fetchUpstream(lat, lon float64) ([]byte, error) {
	url := h.baseURL() + fmt.Sprintf("?lat=%.6f&lon=%.6f", lat, lon)
	if h.APIKey != "" {
		url += "&serviceKey=" + h.APIKey
	}

	resp, err := h.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxBody = 1 << 20 // 1MB
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		return nil, fmt.Errorf("response %d: %s", resp.StatusCode, body)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

func (h *AirHandler) baseURL() string {
	if h.BaseURL != "" {
		return h.BaseURL
	}
	return "https://apis.data.go.kr/B552584/ArpltnInforInqireSvc/getMsrstnAcctoRltmMesureDnsty"
}
