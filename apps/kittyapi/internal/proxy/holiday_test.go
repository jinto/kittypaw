package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittyapi/internal/cache"
	"github.com/kittypaw-app/kittyapi/internal/proxy"
)

func newHolidayHandler(upstreamURL string) (*proxy.HolidayHandler, *cache.Cache) {
	c := cache.New()
	h := &proxy.HolidayHandler{
		Cache:      c,
		HTTPClient: &http.Client{},
		APIKey:     "test-key",
		BaseURL:    upstreamURL,
	}
	return h, c
}

func TestHolidays(t *testing.T) {
	upstream := fakeUpstream(`{"response":{"header":{"resultCode":"00"},"body":{"items":{"item":[]}}}}`)
	defer upstream.Close()

	h, c := newHolidayHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays?solYear=2026", nil)
	w := httptest.NewRecorder()
	h.Holidays().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHolidaysMissingSolYear(t *testing.T) {
	h, c := newHolidayHandler("")
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays", nil)
	w := httptest.NewRecorder()
	h.Holidays().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHolidaysWithMonth(t *testing.T) {
	upstream := fakeUpstream(`{"response":{"header":{"resultCode":"00"},"body":{"items":{"item":[]}}}}`)
	defer upstream.Close()

	h, c := newHolidayHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays?solYear=2026&solMonth=05", nil)
	w := httptest.NewRecorder()
	h.Holidays().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAnniversaries(t *testing.T) {
	upstream := fakeUpstream(`{"response":{"header":{"resultCode":"00"}}}`)
	defer upstream.Close()

	h, c := newHolidayHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/anniversaries?solYear=2026", nil)
	w := httptest.NewRecorder()
	h.Anniversaries().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSolarTerms(t *testing.T) {
	upstream := fakeUpstream(`{"response":{"header":{"resultCode":"00"}}}`)
	defer upstream.Close()

	h, c := newHolidayHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/solar-terms?solYear=2026", nil)
	w := httptest.NewRecorder()
	h.SolarTerms().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHolidayCacheHit(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		_, _ = w.Write([]byte(`{"response":{"header":{"resultCode":"00"}}}`))
	}))
	defer upstream.Close()

	h, c := newHolidayHandler(upstream.URL)
	defer c.Close()

	handler := h.Holidays()

	// First request fills cache.
	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays?solYear=2026", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	upstreamCalled = false

	// Second request should hit cache.
	req = httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays?solYear=2026", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if upstreamCalled {
		t.Fatal("expected cache hit, but upstream was called")
	}
}

func TestHolidayUpstreamFailureWithStaleCache(t *testing.T) {
	c := cache.New()
	defer c.Close()

	// Pre-populate with stale data (TTL=1ns → immediately stale).
	c.Set("holiday:/getHoliDeInfo:_type=json:solYear=2026", []byte(`{"stale":true}`), 1)

	upstream := failingUpstream()
	defer upstream.Close()

	h := &proxy.HolidayHandler{
		Cache:      c,
		HTTPClient: &http.Client{},
		APIKey:     "test-key",
		BaseURL:    upstream.URL,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays?solYear=2026", nil)
	w := httptest.NewRecorder()
	h.Holidays().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (stale), got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Warning") != `110 - "Response is stale"` {
		t.Fatalf("expected Warning header, got %q", w.Header().Get("Warning"))
	}
}

func TestHolidayUpstreamFailureNoCache(t *testing.T) {
	upstream := failingUpstream()
	defer upstream.Close()

	h, c := newHolidayHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays?solYear=2026", nil)
	w := httptest.NewRecorder()
	h.Holidays().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

// TestHolidayUsesUnderscoreType pins the outbound query parameter name.
// KASI's SpcdeInfoService accepts `_type=json` and silently ignores
// `returnType=json`, returning XML by default — which broke the JSON
// envelope parser in production. Regression guard.
func TestHolidayUsesUnderscoreType(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(`{"response":{"header":{"resultCode":"00"}}}`))
	}))
	defer upstream.Close()

	h, c := newHolidayHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays?solYear=2026", nil)
	w := httptest.NewRecorder()
	h.Holidays().ServeHTTP(w, req)

	if !contains(receivedURL, "_type=json") {
		t.Fatalf("expected _type=json in upstream URL, got: %s", receivedURL)
	}
	if contains(receivedURL, "returnType=json") {
		t.Fatalf("returnType=json must NOT be added (KASI SpcdeInfoService uses _type), got: %s", receivedURL)
	}
}

func TestHolidayParamsNotLeaked(t *testing.T) {
	var receivedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		_, _ = w.Write([]byte(`{"response":{"header":{"resultCode":"00"}}}`))
	}))
	defer upstream.Close()

	h, c := newHolidayHandler(upstream.URL)
	defer c.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/calendar/holidays?solYear=2026&evil=drop_table", nil)
	w := httptest.NewRecorder()
	h.Holidays().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if contains(receivedURL, "evil") {
		t.Fatalf("unknown param leaked to upstream: %s", receivedURL)
	}
}
