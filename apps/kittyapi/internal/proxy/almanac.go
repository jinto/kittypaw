package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/cache"
	"github.com/kittypaw-app/kittyapi/internal/proxy/kma"
)

const (
	almanacBaseURL  = "https://apis.data.go.kr/B090041/openapi/service"
	almanacCacheTTL = 24 * time.Hour
)

// AlmanacHandler proxies KASI (한국천문연구원) lunar calendar and rise/set
// services. Two ServiceNames sit under the same B090041 root with the same
// response envelope, so a single handler covers both:
//
//   - LrsrCldInfoService — getLunCalInfo (solar→lunar), getSolCalInfo (lunar→solar)
//   - RiseSetInfoService — getLCRiseSetInfo (by coord), getAreaRiseSetInfo (by region)
//
// Two patterns diverge from holiday.go (also a KASI proxy):
//   - `_type=json`. Every B090041 service — including SpcdeInfoService —
//     accepts `_type=json` and silently ignores `returnType=json`, falling
//     back to XML. holiday.go originally used `returnType` and shipped
//     broken; both proxies now use `_type`.
//   - fetch() propagates request context + suppresses URL leaks in errors,
//     matching weather.go. holiday.go predates these patterns.
//
// A future refactor may unify endpoint() across all three KASI proxies; tracked
// as a follow-up so this PR doesn't drag holiday.go's quirks into review scope.
type AlmanacHandler struct {
	Cache      *cache.Cache
	HTTPClient *http.Client
	APIKey     string
	BaseURL    string // overridable for testing
}

func (h *AlmanacHandler) baseURL() string {
	if h.BaseURL != "" {
		return h.BaseURL
	}
	return almanacBaseURL
}

// LunarDate returns the lunar date corresponding to a given solar date.
// KASI: LrsrCldInfoService/getLunCalInfo. Required: solYear+solMonth+solDay.
func (h *AlmanacHandler) LunarDate() http.HandlerFunc {
	return h.endpoint("LrsrCldInfoService", "/getLunCalInfo",
		[]string{"solYear", "solMonth", "solDay"},
		[]string{"solYear", "solMonth", "solDay"},
	)
}

// SolarDate returns the solar date corresponding to a given lunar date.
// KASI: LrsrCldInfoService/getSolCalInfo. Required: lunYear+lunMonth+lunDay.
// `leapMonth` is optional (값=윤). When the lunar month has both a regular
// and a leap variant, KASI returns both items in body.items.item — passed
// through unchanged for SDK-side filtering on lunLeapmonth (D11/F2).
func (h *AlmanacHandler) SolarDate() http.HandlerFunc {
	return h.endpoint("LrsrCldInfoService", "/getSolCalInfo",
		[]string{"lunYear", "lunMonth", "lunDay"},
		[]string{"lunYear", "lunMonth", "lunDay", "leapMonth"},
	)
}

// Sun returns sunrise/sunset (and moonrise/moonset) for a given date and
// either coordinate (latitude+longitude) or region name (location). Dispatch:
//
//   - latitude+longitude  → RiseSetInfoService/getLCRiseSetInfo
//   - location            → RiseSetInfoService/getAreaRiseSetInfo
//
// Out-of-peninsula coordinates are rejected up front (D9): KASI silently
// remaps them to the nearest Korean region (lat=43,lon=128 → 양구), which is
// silent corruption. The Korea-only bounding box check is reused from
// internal/proxy/kma — we don't actually need the grid coords, just the
// range validation.
//
// `dnYn` is intentionally NOT in the allowed list (D6/F5/F6): KASI's dnYn=N
// path requires DDDMMSS integer coords, which would force a conversion
// layer. The default (no dnYn) already returns sun + moon together, so we
// drop user-supplied dnYn silently.
func (h *AlmanacHandler) Sun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("locdate") == "" {
			http.Error(w, "locdate is required", http.StatusBadRequest)
			return
		}

		if q.Get("location") != "" {
			h.endpoint("RiseSetInfoService", "/getAreaRiseSetInfo",
				[]string{"locdate", "location"},
				[]string{"locdate", "location"},
			)(w, r)
			return
		}

		latStr, lonStr := q.Get("latitude"), q.Get("longitude")
		if latStr == "" || lonStr == "" {
			http.Error(w, "latitude+longitude or location is required", http.StatusBadRequest)
			return
		}
		lat, err := strconv.ParseFloat(latStr, 64)
		if err != nil {
			http.Error(w, "latitude must be a number", http.StatusBadRequest)
			return
		}
		lon, err := strconv.ParseFloat(lonStr, 64)
		if err != nil {
			http.Error(w, "longitude must be a number", http.StatusBadRequest)
			return
		}
		// Reuse KMA's bounding-box check — only the err matters; we discard
		// the grid result. KASI accepts decimal degrees directly (F3/T0).
		if _, _, err := kma.LatLngToGrid(lat, lon); err != nil {
			if errors.Is(err, kma.ErrOutOfKoreaPeninsula) {
				http.Error(w, "latitude/longitude out of Korea peninsula", http.StatusBadRequest)
				return
			}
			http.Error(w, "coordinate validation failed", http.StatusInternalServerError)
			return
		}

		h.endpoint("RiseSetInfoService", "/getLCRiseSetInfo",
			[]string{"locdate", "latitude", "longitude"},
			[]string{"locdate", "latitude", "longitude"},
		)(w, r)
	}
}

// endpoint is the shared handler for all four KASI endpoints. Differences
// (serviceName, path, required/allowed query params) are passed in; cache
// behavior, error handling, and `_type=json` enforcement are common.
func (h *AlmanacHandler) endpoint(serviceName, path string, required, allowed []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		for _, p := range required {
			if q.Get(p) == "" {
				http.Error(w, p+" is required", http.StatusBadRequest)
				return
			}
		}

		upstream := url.Values{}
		for _, p := range allowed {
			if v := q.Get(p); v != "" {
				upstream.Set(p, v)
			}
		}
		upstream.Set("serviceKey", h.APIKey)
		// KASI B090041 services accept `_type=json` and silently ignore
		// `returnType=json` — see package comment for the production incident.
		upstream.Set("_type", "json")

		key := AlmanacCacheKey(serviceName, path, upstream)

		if data, ok := h.Cache.Get(key); ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}

		data, err := h.fetch(r.Context(), serviceName, path, upstream)
		if err != nil {
			log.Printf("almanac upstream error (%s%s): %v", serviceName, path, err)
			if stale, isStale, found := h.Cache.GetStale(key); found && isStale {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Warning", `110 - "Response is stale"`)
				_, _ = w.Write(stale)
				return
			}
			http.Error(w, "upstream service unavailable", http.StatusBadGateway)
			return
		}

		h.Cache.Set(key, data, almanacCacheTTL)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

func (h *AlmanacHandler) fetch(ctx context.Context, serviceName, path string, params url.Values) ([]byte, error) {
	u := h.baseURL() + "/" + serviceName + path + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		// Do NOT wrap with %w — Go's *url.Error stringifies the full URL
		// including the serviceKey query parameter, which would leak the
		// upstream API key into operational logs (log.Printf above).
		return nil, fmt.Errorf("build request to %s%s failed", serviceName, path)
	}

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		// Same defense as above — *url.Error.Error() would leak serviceKey.
		// Future maintainers: keep this as plain Errorf, never %w.
		return nil, fmt.Errorf("request to %s%s failed", serviceName, path)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Drain so the connection is reusable; do not echo back — KASI
		// failure responses sometimes include the request URL.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))
		return nil, fmt.Errorf("response status %d from %s%s", resp.StatusCode, serviceName, path)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, err
	}

	// KASI returns HTTP 200 + non-"00" resultCode for upstream errors
	// (expired key, invalid params, NO_DATA, internal SQL errors with
	// Korean messages exposing column names). The envelope shape matches
	// KMA's, so we reuse parseKMAError from weather.go. Treating a non-"00"
	// code as a fetch error means: do NOT cache the bad payload (24h TTL
	// would freeze a transient error), fall back to stale cache, and
	// surface 502 if no stale exists.
	if resultCode, msg, isErr := parseKMAError(body); isErr {
		return nil, fmt.Errorf("kasi resultCode=%s msg=%s", resultCode, msg)
	}

	return body, nil
}

// AlmanacCacheKey builds a cache key that includes serviceName + path so
// the LrsrCldInfoService and RiseSetInfoService namespaces never collide.
// serviceKey is excluded so a key rotation doesn't invalidate every entry.
//
// Exported so tests can prime stale entries at the exact key the handler
// will compute, without duplicating the algorithm. Same pattern as
// WeatherCacheKey in weather.go.
func AlmanacCacheKey(serviceName, path string, params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "serviceKey" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("almanac:")
	b.WriteString(serviceName)
	b.WriteString(path)
	for _, k := range keys {
		b.WriteByte(':')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params.Get(k))
	}
	return b.String()
}
