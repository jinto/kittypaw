package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jinto/kittypaw-api/internal/cache"
	"github.com/jinto/kittypaw-api/internal/proxy"
)

// almanacOKEnvelope is the minimal KASI response envelope that passes
// parseKMAError's resultCode == "00" check. Tests that don't care about
// body content but need the fetch path to succeed use this string.
const almanacOKEnvelope = `{"response":{"header":{"resultCode":"00","resultMsg":"NORMAL SERVICE."}}}`

// newAlmanacHandler is the test factory shared by all almanac sub-tests. It
// wires a fresh cache + an in-test HTTP client targeted at upstreamURL.
func newAlmanacHandler(upstreamURL string) (*proxy.AlmanacHandler, *cache.Cache) {
	c := cache.New()
	h := &proxy.AlmanacHandler{
		Cache:      c,
		HTTPClient: &http.Client{},
		APIKey:     "test-key",
		BaseURL:    upstreamURL,
	}
	return h, c
}

// ---- T1: LunarDate (양→음) ----

func TestLunarDate_Happy(t *testing.T) {
	upstream := fakeUpstream(`{"response":{"header":{"resultCode":"00"},"body":{"items":{"item":{"lunYear":2026,"lunMonth":"03","lunDay":15,"lunLeapmonth":"평"}}}}}`)
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01", nil)
	w := httptest.NewRecorder()
	h.LunarDate().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLunarDate_MissingParams(t *testing.T) {
	h, c := newAlmanacHandler("")
	defer c.Close()

	tests := []struct {
		name string
		url  string
	}{
		{"missing solYear", "/v1/almanac/lunar-date?solMonth=05&solDay=01"},
		{"missing solMonth", "/v1/almanac/lunar-date?solYear=2026&solDay=01"},
		{"missing solDay", "/v1/almanac/lunar-date?solYear=2026&solMonth=05"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			h.LunarDate().ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestLunarDate_CacheHit(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		_, _ = w.Write([]byte(almanacOKEnvelope))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	handler := h.LunarDate()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	upstreamCalled = false

	req = httptest.NewRequest(http.MethodGet, "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if upstreamCalled {
		t.Fatal("expected cache hit, but upstream was called")
	}
}

func TestLunarDate_ParamWhitelist(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(almanacOKEnvelope))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01&evil=drop_table", nil)
	w := httptest.NewRecorder()
	h.LunarDate().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if contains(receivedURL, "evil") {
		t.Fatalf("unknown param leaked to upstream: %s", receivedURL)
	}
}

// TestLunarDate_TypeJsonNotReturnType pins plan v3 D4/F10: KASI 음력 uses
// `_type=json`, NOT `returnType=json`. Future maintainers might "unify" with
// holiday.go and break this — this test catches the regression.
func TestLunarDate_TypeJsonNotReturnType(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(almanacOKEnvelope))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01", nil)
	w := httptest.NewRecorder()
	h.LunarDate().ServeHTTP(w, req)

	if !contains(receivedURL, "_type=json") {
		t.Fatalf("expected _type=json in upstream URL, got: %s", receivedURL)
	}
	if contains(receivedURL, "returnType=json") {
		t.Fatalf("returnType=json must NOT be added (KASI 음력 uses _type), got: %s", receivedURL)
	}
}

func TestLunarDate_UpstreamFailureWithStaleCache(t *testing.T) {
	c := cache.New()
	defer c.Close()

	// Pre-populate cache at the exact key the handler will compute, with a
	// 1ns TTL so the entry is immediately stale but still findable via
	// GetStale. Use AlmanacCacheKey so this stays in sync with handler logic.
	staleKey := proxy.AlmanacCacheKey("LrsrCldInfoService", "/getLunCalInfo", url.Values{
		"_type":    {"json"},
		"solYear":  {"2026"},
		"solMonth": {"05"},
		"solDay":   {"01"},
	})
	c.Set(staleKey, []byte(`{"stale":true}`), 1)

	upstream := failingUpstream()
	defer upstream.Close()

	h := &proxy.AlmanacHandler{
		Cache:      c,
		HTTPClient: &http.Client{},
		APIKey:     "test-key",
		BaseURL:    upstream.URL,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01", nil)
	w := httptest.NewRecorder()
	h.LunarDate().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (stale), got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Warning") != `110 - "Response is stale"` {
		t.Fatalf("expected Warning header, got %q", w.Header().Get("Warning"))
	}
}

func TestLunarDate_UpstreamFailureNoCache(t *testing.T) {
	upstream := failingUpstream()
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/lunar-date?solYear=2026&solMonth=05&solDay=01", nil)
	w := httptest.NewRecorder()
	h.LunarDate().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

// ---- T2: SolarDate (음→양) ----

func TestSolarDate_Happy(t *testing.T) {
	upstream := fakeUpstream(`{"response":{"header":{"resultCode":"00"},"body":{"items":{"item":{"solYear":2026,"solMonth":"05","solDay":"01"}}}}}`)
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/solar-date?lunYear=2026&lunMonth=03&lunDay=15", nil)
	w := httptest.NewRecorder()
	h.SolarDate().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSolarDate_MissingParams(t *testing.T) {
	h, c := newAlmanacHandler("")
	defer c.Close()

	tests := []struct {
		name string
		url  string
	}{
		{"missing lunYear", "/v1/almanac/solar-date?lunMonth=03&lunDay=15"},
		{"missing lunMonth", "/v1/almanac/solar-date?lunYear=2026&lunDay=15"},
		{"missing lunDay", "/v1/almanac/solar-date?lunYear=2026&lunMonth=03"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			h.SolarDate().ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestSolarDate_CacheHit(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		_, _ = w.Write([]byte(almanacOKEnvelope))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	handler := h.SolarDate()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/solar-date?lunYear=2026&lunMonth=03&lunDay=15", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	upstreamCalled = false

	req = httptest.NewRequest(http.MethodGet, "/v1/almanac/solar-date?lunYear=2026&lunMonth=03&lunDay=15", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if upstreamCalled {
		t.Fatal("expected cache hit, but upstream was called")
	}
}

// TestSolarDate_LeapMonthPassthrough pins plan v3 D11/F2: when KASI returns
// both regular and leap-month items (e.g. 음력 2025-06-01 → 양력 06-25 평달
// + 양력 07-25 윤달), we passthrough the body untouched and let the SDK
// filter on lunLeapmonth.
func TestSolarDate_LeapMonthPassthrough(t *testing.T) {
	twoItemBody := `{"response":{"header":{"resultCode":"00"},"body":{"items":{"item":[{"lunLeapmonth":"평","solDay":25,"solMonth":"06","solYear":2025},{"lunLeapmonth":"윤","solDay":25,"solMonth":"07","solYear":2025}]},"totalCount":2}}}`

	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(twoItemBody))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/solar-date?lunYear=2025&lunMonth=06&lunDay=01&leapMonth=%EC%9C%A4", nil)
	w := httptest.NewRecorder()
	h.SolarDate().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != twoItemBody {
		t.Fatalf("expected passthrough body, got: %s", w.Body.String())
	}
	if !contains(receivedURL, "leapMonth=") {
		t.Fatalf("expected leapMonth forwarded to upstream, got: %s", receivedURL)
	}
}

func TestSolarDate_ParamWhitelist(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(almanacOKEnvelope))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/solar-date?lunYear=2026&lunMonth=03&lunDay=15&evil=drop", nil)
	w := httptest.NewRecorder()
	h.SolarDate().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if contains(receivedURL, "evil") {
		t.Fatalf("unknown param leaked: %s", receivedURL)
	}
}

func TestSolarDate_UpstreamFailureWithStaleCache(t *testing.T) {
	c := cache.New()
	defer c.Close()

	staleKey := proxy.AlmanacCacheKey("LrsrCldInfoService", "/getSolCalInfo", url.Values{
		"_type":    {"json"},
		"lunYear":  {"2026"},
		"lunMonth": {"03"},
		"lunDay":   {"15"},
	})
	c.Set(staleKey, []byte(`{"stale":true}`), 1)

	upstream := failingUpstream()
	defer upstream.Close()

	h := &proxy.AlmanacHandler{
		Cache:      c,
		HTTPClient: &http.Client{},
		APIKey:     "test-key",
		BaseURL:    upstream.URL,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/solar-date?lunYear=2026&lunMonth=03&lunDay=15", nil)
	w := httptest.NewRecorder()
	h.SolarDate().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (stale), got %d", w.Code)
	}
	if w.Header().Get("Warning") != `110 - "Response is stale"` {
		t.Fatalf("expected Warning header, got %q", w.Header().Get("Warning"))
	}
}

func TestSolarDate_UpstreamFailureNoCache(t *testing.T) {
	upstream := failingUpstream()
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/solar-date?lunYear=2026&lunMonth=03&lunDay=15", nil)
	w := httptest.NewRecorder()
	h.SolarDate().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

// ---- T3+T4: Sun (좌표/지역 통합) ----

func TestSun_HappyCoords(t *testing.T) {
	upstream := fakeUpstream(`{"response":{"header":{"resultCode":"00"},"body":{"items":{"item":{"sunrise":"0537","sunset":"1922"}}}}}`)
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/sun?locdate=20260501&latitude=37.5665&longitude=126.9780", nil)
	w := httptest.NewRecorder()
	h.Sun().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSun_HappyArea(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(`{"response":{"header":{"resultCode":"00"}}}`))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/sun?locdate=20260501&location=%EC%84%9C%EC%9A%B8", nil)
	w := httptest.NewRecorder()
	h.Sun().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// area endpoint, not LC
	if !contains(receivedURL, "getAreaRiseSetInfo") {
		t.Fatalf("expected area endpoint, got: %s", receivedURL)
	}
}

func TestSun_MissingParams(t *testing.T) {
	h, c := newAlmanacHandler("")
	defer c.Close()

	tests := []struct {
		name string
		url  string
	}{
		{"missing locdate", "/v1/almanac/sun?latitude=37.5665&longitude=126.9780"},
		{"missing both lat+lon and location", "/v1/almanac/sun?locdate=20260501"},
		{"only latitude (no longitude)", "/v1/almanac/sun?locdate=20260501&latitude=37.5665"},
		{"only longitude (no latitude)", "/v1/almanac/sun?locdate=20260501&longitude=126.9780"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			w := httptest.NewRecorder()
			h.Sun().ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

// TestSun_OutOfPeninsula pins plan v3 D9: KASI silently remaps out-of-Korea
// coords to the nearest Korean region (lat=43,lon=128 → 양구). We must
// reject up front to avoid silent corruption.
func TestSun_OutOfPeninsula(t *testing.T) {
	h, c := newAlmanacHandler("")
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/sun?locdate=20260501&latitude=43.0&longitude=128.0", nil)
	w := httptest.NewRecorder()
	h.Sun().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (out of peninsula), got %d: %s", w.Code, w.Body.String())
	}
}

func TestSun_InvalidCoords(t *testing.T) {
	h, c := newAlmanacHandler("")
	defer c.Close()

	tests := []string{
		"/v1/almanac/sun?locdate=20260501&latitude=abc&longitude=126.9780",
		"/v1/almanac/sun?locdate=20260501&latitude=37.5665&longitude=def",
	}
	for _, u := range tests {
		t.Run(u, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, u, nil)
			w := httptest.NewRecorder()
			h.Sun().ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
		})
	}
}

// TestSun_DnYnSilentlyDropped pins plan v3 D6/F5/F6: user-supplied dnYn
// must NOT be forwarded upstream. dnYn=N would force DDDMMSS integer
// coords, breaking the decimal contract. The default (no dnYn) already
// returns sun+moon together.
func TestSun_DnYnSilentlyDropped(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(almanacOKEnvelope))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/sun?locdate=20260501&latitude=37.5665&longitude=126.9780&dnYn=N", nil)
	w := httptest.NewRecorder()
	h.Sun().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if contains(receivedURL, "dnYn") {
		t.Fatalf("dnYn must be silently dropped, got upstream URL: %s", receivedURL)
	}
}

func TestSun_CacheHit(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		_, _ = w.Write([]byte(almanacOKEnvelope))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	handler := h.Sun()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/sun?locdate=20260501&latitude=37.5665&longitude=126.9780", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	upstreamCalled = false

	req = httptest.NewRequest(http.MethodGet, "/v1/almanac/sun?locdate=20260501&latitude=37.5665&longitude=126.9780", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if upstreamCalled {
		t.Fatal("expected cache hit, but upstream was called")
	}
}

func TestSun_ParamWhitelist(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(almanacOKEnvelope))
	}))
	defer upstream.Close()

	h, c := newAlmanacHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/almanac/sun?locdate=20260501&latitude=37.5665&longitude=126.9780&evil=drop", nil)
	w := httptest.NewRecorder()
	h.Sun().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if contains(receivedURL, "evil") {
		t.Fatalf("unknown param leaked: %s", receivedURL)
	}
}
