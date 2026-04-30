package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jinto/kittypaw-api/internal/cache"
	"github.com/jinto/kittypaw-api/internal/proxy/kma"
)

// weatherKMABaseURL is the KMA village-forecast OpenAPI root.
const weatherKMABaseURL = "https://apis.data.go.kr/1360000/VilageFcstInfoService_2.0"

// weatherCacheTTL — KMA publishes a new run every 3 hours; 30 minutes in
// cache is safe because base_time is part of the key.
const weatherCacheTTL = 30 * time.Minute

// WeatherHandler proxies KMA 단기예보 (village forecast).
type WeatherHandler struct {
	Cache      *cache.Cache
	HTTPClient *http.Client
	APIKey     string
	BaseURL    string           // overridable for testing
	Now        func() time.Time // injectable clock; defaults to time.Now
}

func (h *WeatherHandler) baseURL() string {
	if h.BaseURL != "" {
		return h.BaseURL
	}
	return weatherKMABaseURL
}

func (h *WeatherHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// WeatherCacheKey is the canonical cache key for a KMA village forecast
// lookup. Exported so tests can prime the cache with stale entries at the
// exact key the handler will compute.
func WeatherCacheKey(nx, ny int, baseDate, baseTime string) string {
	return fmt.Sprintf("kma:village:base=%s%s&nx=%d&ny=%d", baseDate, baseTime, nx, ny)
}

// VillageForecast returns the KMA short-range forecast for a lat/lon point.
// The handler converts to KMA grid (nx, ny), maps the wall clock to the most
// recent published base_time, and proxies + caches the upstream JSON.
func (h *WeatherHandler) VillageForecast() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		lat, err := strconv.ParseFloat(q.Get("lat"), 64)
		if err != nil {
			http.Error(w, "lat is required (float)", http.StatusBadRequest)
			return
		}
		lon, err := strconv.ParseFloat(q.Get("lon"), 64)
		if err != nil {
			http.Error(w, "lon is required (float)", http.StatusBadRequest)
			return
		}

		nx, ny, err := kma.LatLngToGrid(lat, lon)
		if err != nil {
			if errors.Is(err, kma.ErrOutOfKoreaPeninsula) {
				http.Error(w, "lat/lon out of Korea peninsula", http.StatusBadRequest)
				return
			}
			http.Error(w, "grid conversion failed", http.StatusInternalServerError)
			return
		}

		baseDate, baseTime := kma.NowToBaseDateTime(h.now())
		key := WeatherCacheKey(nx, ny, baseDate, baseTime)

		if data, ok := h.Cache.Get(key); ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}

		params := url.Values{}
		params.Set("serviceKey", h.APIKey)
		params.Set("nx", strconv.Itoa(nx))
		params.Set("ny", strconv.Itoa(ny))
		params.Set("base_date", baseDate)
		params.Set("base_time", baseTime)
		params.Set("numOfRows", "1000")
		params.Set("pageNo", "1")
		params.Set("dataType", "JSON")

		data, err := h.fetch(r.Context(), "/getVilageFcst", params)
		if err != nil {
			log.Printf("kma weather upstream error: %v", err)
			if stale, isStale, found := h.Cache.GetStale(key); found && isStale {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Warning", `110 - "Response is stale"`)
				_, _ = w.Write(stale)
				return
			}
			http.Error(w, "upstream service unavailable", http.StatusBadGateway)
			return
		}

		h.Cache.Set(key, data, weatherCacheTTL)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

func (h *WeatherHandler) fetch(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := h.baseURL() + path + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		// Do NOT wrap with %w — Go's *url.Error stringifies the full URL
		// including the serviceKey query parameter, which would leak the
		// upstream API key into operational logs.
		return nil, fmt.Errorf("request to %s failed", path)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Drain the body so the connection is reusable, but do not include
		// it in the error — data.go.kr sometimes echoes request params back
		// in failure responses.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))
		return nil, fmt.Errorf("response status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, err
	}

	if resultCode, msg, isErr := parseKMAError(body); isErr {
		return nil, fmt.Errorf("kma resultCode=%s msg=%s", resultCode, msg)
	}

	return body, nil
}

// kmaResponse is the minimal envelope used to extract resultCode. KMA
// returns 200 OK + non-"00" resultCode for upstream errors (expired key,
// NO_DATA, etc.); we treat those as upstream failures, not as cacheable
// payloads.
type kmaResponse struct {
	Response struct {
		Header struct {
			ResultCode string `json:"resultCode"`
			ResultMsg  string `json:"resultMsg"`
		} `json:"header"`
	} `json:"response"`
}

// parseKMAError extracts (resultCode, resultMsg) and reports whether it
// represents an upstream error. Any non-"00" code (including "03 NO_DATA")
// is treated as an error so we don't cache empty/expired responses.
//
// A missing envelope (no `response.header.resultCode` field at all) is
// also flagged as an error: KMA always returns the envelope on success,
// so its absence means we got an HTML error page or a CDN-injected body
// that must NOT be cached for 30 minutes.
func parseKMAError(body []byte) (resultCode, resultMsg string, isError bool) {
	var r kmaResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", "malformed JSON envelope", true
	}
	rc := r.Response.Header.ResultCode
	if rc == "" {
		return "", "missing response.header.resultCode", true
	}
	if rc == "00" {
		return rc, r.Response.Header.ResultMsg, false
	}
	return rc, r.Response.Header.ResultMsg, true
}
