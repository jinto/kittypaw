package proxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jinto/kittypaw-api/internal/model"
	"github.com/jinto/kittypaw-api/internal/proxy"
)

// mockPlaceStore implements model.PlaceStore in-memory. typeHint filtering
// matches the real PostgresPlaceStore behavior (empty hint = any type).
type mockPlaceStore struct {
	aliasOverride map[string]*model.Place
	exact         map[string]*model.Place
	byAlias       map[string]*model.Place
	fuzzy         map[string]*model.Place
}

func newMockStore() *mockPlaceStore {
	return &mockPlaceStore{
		aliasOverride: map[string]*model.Place{},
		exact:         map[string]*model.Place{},
		byAlias:       map[string]*model.Place{},
		fuzzy:         map[string]*model.Place{},
	}
}

func (m *mockPlaceStore) FindAliasOverride(_ context.Context, alias string) (*model.Place, error) {
	if p, ok := m.aliasOverride[alias]; ok {
		return p, nil
	}
	return nil, model.ErrNotFound
}

func (m *mockPlaceStore) FindExact(_ context.Context, name, typeHint string) (*model.Place, error) {
	p, ok := m.exact[name]
	if !ok {
		return nil, model.ErrNotFound
	}
	if typeHint != "" && p.Type != typeHint {
		return nil, model.ErrNotFound
	}
	return p, nil
}

func (m *mockPlaceStore) FindByAlias(_ context.Context, alias, typeHint string) (*model.Place, error) {
	p, ok := m.byAlias[alias]
	if !ok {
		return nil, model.ErrNotFound
	}
	if typeHint != "" && p.Type != typeHint {
		return nil, model.ErrNotFound
	}
	return p, nil
}

func (m *mockPlaceStore) FindByFuzzy(_ context.Context, q, typeHint string, _ float64) (*model.Place, error) {
	p, ok := m.fuzzy[q]
	if !ok {
		return nil, model.ErrNotFound
	}
	if typeHint != "" && p.Type != typeHint {
		return nil, model.ErrNotFound
	}
	return p, nil
}

func (m *mockPlaceStore) Upsert(_ context.Context, _ *model.Place) error {
	return nil
}

func newHandler(s model.PlaceStore) http.HandlerFunc {
	return (&proxy.PlacesHandler{Store: s}).Resolve()
}

// serve builds a resolve request for q (raw, not percent-encoded) and runs it
// through the handler. Pass an already-encoded value via servePreEncoded when
// you need to test malformed input.
func serve(t *testing.T, s model.PlaceStore, q string) *httptest.ResponseRecorder {
	t.Helper()
	return servePreEncoded(t, s, url.QueryEscape(q))
}

func servePreEncoded(t *testing.T, s model.PlaceStore, encoded string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/v1/geo/resolve"
	if encoded != "" {
		target += "?q=" + encoded
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	newHandler(s).ServeHTTP(w, req)
	return w
}

func TestResolveAliasOverridePriority(t *testing.T) {
	s := newMockStore()
	// Both alias_override and places exact have an entry — alias_override must win.
	s.aliasOverride["코엑스"] = &model.Place{
		NameKo: "코엑스", Lat: 37.5119, Lon: 127.0589,
		Source: model.SourceKittypawAlias, Type: model.TypeAliasOverride,
	}
	s.exact["코엑스"] = &model.Place{
		NameKo: "코엑스 (Wikidata)", Lat: 0, Lon: 0,
		Source: model.SourceWikidata, Type: model.TypeLandmark,
	}

	w := serve(t, s, "코엑스")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"source":"kittypaw_alias"`) {
		t.Errorf("expected alias_override to win, body=%s", w.Body.String())
	}
}

func TestResolveExactMatch(t *testing.T) {
	s := newMockStore()
	s.exact["광화문"] = &model.Place{
		NameKo: "광화문", Lat: 37.576, Lon: 126.977,
		Source: model.SourceWikidata, Type: model.TypeLandmark,
	}

	w := serve(t, s, "광화문")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"lat":37.576`) || !strings.Contains(body, `"lon":126.977`) {
		t.Errorf("coords missing in body: %s", body)
	}
	if !strings.Contains(body, `"source":"wikidata"`) {
		t.Errorf("source missing: %s", body)
	}
}

func TestResolveFuzzyFallback(t *testing.T) {
	s := newMockStore()
	s.fuzzy["강남"] = &model.Place{
		NameKo: "강남역", Lat: 37.4979, Lon: 127.0276,
		Source: model.SourceSeoulMetro, Type: model.TypeSubwayStation,
	}

	w := serve(t, s, "강남")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"name_matched":"강남역"`) {
		t.Errorf("expected fuzzy match name in body, got %s", w.Body.String())
	}
}

func TestResolveSubwayTypeHint(t *testing.T) {
	s := newMockStore()
	s.exact["강남역"] = &model.Place{
		NameKo: "강남역", Lat: 37.4979, Lon: 127.0276,
		Source: model.SourceSeoulMetro, Type: model.TypeSubwayStation,
	}

	w := serve(t, s, "강남역")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"type":"subway_station"`) {
		t.Errorf("expected subway_station type, got %s", w.Body.String())
	}
}

func TestResolveMissingQ(t *testing.T) {
	w := serve(t, newMockStore(), "")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"error":"missing_q"`) {
		t.Errorf("expected missing_q, got %s", w.Body.String())
	}
}

func TestResolveEmptyQAfterTrim(t *testing.T) {
	w := servePreEncoded(t, newMockStore(), "%20%20%20")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace-only, got %d", w.Code)
	}
}

func TestResolveInputTooLong(t *testing.T) {
	long := strings.Repeat("a", 201)
	w := serve(t, newMockStore(), long)

	if w.Code != http.StatusRequestURITooLong {
		t.Fatalf("expected 414, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"error":"input_too_long"`) {
		t.Errorf("expected input_too_long, got %s", w.Body.String())
	}
}

func TestResolveUnsupported(t *testing.T) {
	w := serve(t, newMockStore(), "알수없는장소XYZ")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"error":"unsupported_input"`) {
		t.Errorf("expected unsupported_input, got %s", body)
	}
	if !strings.Contains(body, `"hint":`) {
		t.Errorf("expected hint field, got %s", body)
	}
}
