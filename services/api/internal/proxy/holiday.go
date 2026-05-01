package proxy

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jinto/kittypaw-api/internal/cache"
)

const (
	holidayBaseURL  = "https://apis.data.go.kr/B090041/openapi/service/SpcdeInfoService"
	holidayCacheTTL = 24 * time.Hour
)

// HolidayHandler proxies requests to the KASI (한국천문연구원) special day API.
type HolidayHandler struct {
	Cache      *cache.Cache
	HTTPClient *http.Client
	APIKey     string
	BaseURL    string // overridable for testing
}

func (h *HolidayHandler) baseURL() string {
	if h.BaseURL != "" {
		return h.BaseURL
	}
	return holidayBaseURL
}

// Holidays proxies 공휴일 정보조회.
func (h *HolidayHandler) Holidays() http.HandlerFunc {
	return h.endpoint("/getHoliDeInfo",
		[]string{"solYear"},
		[]string{"solYear", "solMonth", "pageNo", "numOfRows"},
	)
}

// Anniversaries proxies 기념일 정보조회.
func (h *HolidayHandler) Anniversaries() http.HandlerFunc {
	return h.endpoint("/getAnniversaryInfo",
		[]string{"solYear"},
		[]string{"solYear", "solMonth", "pageNo", "numOfRows"},
	)
}

// SolarTerms proxies 24절기 정보조회.
func (h *HolidayHandler) SolarTerms() http.HandlerFunc {
	return h.endpoint("/get24DivisionsInfo",
		[]string{"solYear"},
		[]string{"solYear", "solMonth", "pageNo", "numOfRows"},
	)
}

func (h *HolidayHandler) endpoint(path string, required, allowed []string) http.HandlerFunc {
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
		upstream.Set("returnType", "json")

		key := holidayCacheKey(path, upstream)

		if data, ok := h.Cache.Get(key); ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
			return
		}

		data, err := h.fetch(path, upstream)
		if err != nil {
			log.Printf("holiday upstream error (%s): %v", path, err)
			if stale, isStale, found := h.Cache.GetStale(key); found && isStale {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Warning", `110 - "Response is stale"`)
				_, _ = w.Write(stale)
				return
			}
			http.Error(w, "upstream service unavailable", http.StatusBadGateway)
			return
		}

		h.Cache.Set(key, data, holidayCacheTTL)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

func (h *HolidayHandler) fetch(path string, params url.Values) ([]byte, error) {
	u := h.baseURL() + path + "?" + params.Encode()

	resp, err := h.HTTPClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed", path)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))
		return nil, fmt.Errorf("response status %d from %s", resp.StatusCode, path)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, err
	}

	// KASI returns HTTP 200 + non-"00" resultCode for upstream errors
	// (expired key, invalid params, NO_DATA). Treating as fetch error means:
	// don't cache the bad payload (24h TTL would freeze a transient error),
	// fall back to stale cache, and surface 502 if no stale exists. Same
	// envelope shape as KMA, so we reuse parseKMAError from weather.go.
	if resultCode, msg, isErr := parseKMAError(body); isErr {
		return nil, fmt.Errorf("kasi resultCode=%s msg=%s", resultCode, msg)
	}

	return body, nil
}

func holidayCacheKey(path string, params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "serviceKey" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("holiday:")
	b.WriteString(path)
	for _, k := range keys {
		b.WriteByte(':')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params.Get(k))
	}
	return b.String()
}
