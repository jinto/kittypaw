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

// weatherCacheTTL caps cache lifetime across all 3 KMA endpoints. base_time
// is part of every cache key, so slot rollover (every 30 min for ultra-srt-
// fcst, every hour for ultra-srt-ncst, every 3 hours for village-fcst)
// forces a fresh fetch — TTL is just a burst absorber. The hour/30-min
// endpoints will see lower hit rates than village-fcst at this TTL, but
// never serve stale data.
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

// UltraShortNowcastCacheKey is the cache key for KMA's getUltraSrtNcst.
// Distinct namespace from WeatherCacheKey — same nx/ny/base_time across
// endpoints must NEVER collide.
func UltraShortNowcastCacheKey(nx, ny int, baseDate, baseTime string) string {
	return fmt.Sprintf("kma:nowcast:base=%s%s&nx=%d&ny=%d", baseDate, baseTime, nx, ny)
}

// UltraShortForecastCacheKey is the cache key for KMA's getUltraSrtFcst.
func UltraShortForecastCacheKey(nx, ny int, baseDate, baseTime string) string {
	return fmt.Sprintf("kma:fcst-ultra:base=%s%s&nx=%d&ny=%d", baseDate, baseTime, nx, ny)
}

// kmaForecastEndpoint encapsulates per-endpoint differences for the three
// KMA forecast types served by this handler. The shared serveKMAForecast
// implementation reads from this struct.
type kmaForecastEndpoint struct {
	Path       string                                             // upstream path under VilageFcstInfoService_2.0
	BaseTimeFn func(time.Time) (string, string)                   // maps wall clock → base_date/base_time
	CacheKeyFn func(nx, ny int, baseDate, baseTime string) string // namespace-isolated cache key
}

// serveKMAForecast is the shared implementation for VillageForecast,
// UltraShortNowcast, and UltraShortForecast — they differ only in upstream
// path, base_time mapping, and cache key namespace.
func (h *WeatherHandler) serveKMAForecast(w http.ResponseWriter, r *http.Request, ep kmaForecastEndpoint) {
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

	baseDate, baseTime := ep.BaseTimeFn(h.now())
	key := ep.CacheKeyFn(nx, ny, baseDate, baseTime)

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

	data, err := h.fetch(r.Context(), ep.Path, params)
	if err != nil {
		log.Printf("kma upstream error (%s): %v", ep.Path, err)
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

// VillageForecast returns the KMA short-range forecast (3-day, 3-hour grain)
// for a lat/lon point.
func (h *WeatherHandler) VillageForecast() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.serveKMAForecast(w, r, kmaForecastEndpoint{
			Path:       "/getVilageFcst",
			BaseTimeFn: kma.NowToBaseDateTime,
			CacheKeyFn: WeatherCacheKey,
		})
	}
}

// UltraShortNowcast returns KMA's hourly *nowcast* — the most recent
// observed conditions (HH:00 published, ~40 min delay before usable).
// Best for "지금 / 현재 / 방금" intent.
func (h *WeatherHandler) UltraShortNowcast() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.serveKMAForecast(w, r, kmaForecastEndpoint{
			Path:       "/getUltraSrtNcst",
			BaseTimeFn: kma.NowToUltraShortNowcastBaseDateTime,
			CacheKeyFn: UltraShortNowcastCacheKey,
		})
	}
}

// UltraShortForecast returns KMA's ultra-short forecast — 6-hour outlook
// at hourly grain (HH:30 published, ~45 min delay before usable). Best
// for "1시간 후 / 임박 / 오후엔" intent.
func (h *WeatherHandler) UltraShortForecast() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.serveKMAForecast(w, r, kmaForecastEndpoint{
			Path:       "/getUltraSrtFcst",
			BaseTimeFn: kma.NowToUltraShortForecastBaseDateTime,
			CacheKeyFn: UltraShortForecastCacheKey,
		})
	}
}

func (h *WeatherHandler) fetch(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := h.baseURL() + path + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		// Same defense as the Do() error below — *url.Error stringifies
		// the full URL including ?serviceKey=. Sanitize on this branch
		// too even if it's currently unreachable for our hardcoded URL,
		// in case h.BaseURL ever takes external input.
		return nil, fmt.Errorf("build request to %s failed", path)
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
