//go:build integration

package model_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jinto/kittypaw-api/internal/model"
)

const defaultPlacesTestDB = "postgres://kittypaw:kittypaw@localhost:15432/kittypaw_api_test?sslmode=disable"

func setupPlacesTestDB(t *testing.T) (*model.PostgresPlaceStore, *pgxpool.Pool) {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = defaultPlacesTestDB
	}

	m, err := migrate.New("file://../../migrations", "pgx5://"+stripScheme(dbURL))
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		// ignore — first run with empty schema
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	ctx := context.Background()
	pool, err := model.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// Truncate places + alias_overrides for test isolation.
	if _, err := pool.Exec(ctx, "TRUNCATE places, alias_overrides RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return model.NewPlaceStore(pool), pool
}

func TestIntegration_ExactMatch(t *testing.T) {
	store, _ := setupPlacesTestDB(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, &model.Place{
		NameKo: "코엑스", Lat: 37.5119, Lon: 127.0589,
		Type: model.TypeLandmark, Source: model.SourceWikidata,
		SourceRef: "Q485389", SourcePriority: model.PriorityWikidata,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.FindExact(ctx, "코엑스", "")
	if err != nil {
		t.Fatalf("FindExact: %v", err)
	}
	if got.Lat != 37.5119 || got.Lon != 127.0589 {
		t.Errorf("coords mismatch: got (%v, %v)", got.Lat, got.Lon)
	}
	if got.Source != model.SourceWikidata {
		t.Errorf("source: got %q", got.Source)
	}
}

func TestIntegration_AliasArrayMatch(t *testing.T) {
	store, _ := setupPlacesTestDB(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, &model.Place{
		NameKo:  "동대문디자인플라자",
		Aliases: []string{"DDP", "동대문 DDP"},
		Lat:     37.5673, Lon: 127.0095,
		Type: model.TypeLandmark, Source: model.SourceWikidata,
		SourceRef: "Q5825379", SourcePriority: model.PriorityWikidata,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.FindByAlias(ctx, "DDP", "")
	if err != nil {
		t.Fatalf("FindByAlias: %v", err)
	}
	if got.NameKo != "동대문디자인플라자" {
		t.Errorf("name_ko: got %q", got.NameKo)
	}
}

func TestIntegration_FuzzyMatch(t *testing.T) {
	store, _ := setupPlacesTestDB(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, &model.Place{
		NameKo: "강남역", Lat: 37.4979, Lon: 127.0276,
		Type: model.TypeSubwayStation, Source: model.SourceSeoulMetro,
		SourceRef: "seoul-metro:2호선:강남역", SourcePriority: model.PrioritySeoulMetro,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Exact substring still matches via similarity > 0.7.
	got, err := store.FindByFuzzy(ctx, "강남역", "", 0.7)
	if err != nil {
		t.Fatalf("FindByFuzzy exact: %v", err)
	}
	if got.NameKo != "강남역" {
		t.Errorf("expected 강남역, got %q", got.NameKo)
	}

	// Unrelated input must miss.
	if _, err := store.FindByFuzzy(ctx, "전혀무관한장소XYZ", "", 0.7); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("expected ErrNotFound for unrelated input, got %v", err)
	}
}

func TestIntegration_TypeHintFiltersExact(t *testing.T) {
	store, _ := setupPlacesTestDB(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, &model.Place{
		NameKo: "강남역", Lat: 37.4979, Lon: 127.0276,
		Type: model.TypeSubwayStation, Source: model.SourceSeoulMetro,
		SourceRef: "seoul:2:강남역", SourcePriority: model.PrioritySeoulMetro,
	}); err != nil {
		t.Fatalf("upsert station: %v", err)
	}
	if err := store.Upsert(ctx, &model.Place{
		NameKo: "강남역", Lat: 37.5000, Lon: 127.0300,
		Type: model.TypeLandmark, Source: model.SourceWikidata,
		SourceRef: "Q17469", SourcePriority: model.PriorityWikidata,
	}); err != nil {
		t.Fatalf("upsert landmark: %v", err)
	}

	stationHit, err := store.FindExact(ctx, "강남역", model.TypeSubwayStation)
	if err != nil {
		t.Fatalf("FindExact station: %v", err)
	}
	if stationHit.Source != model.SourceSeoulMetro {
		t.Errorf("type hint should filter to subway_station, got source %q", stationHit.Source)
	}

	// Without hint, ORDER BY CASE prefers landmark.
	noHit, err := store.FindExact(ctx, "강남역", "")
	if err != nil {
		t.Fatalf("FindExact no hint: %v", err)
	}
	if noHit.Type != model.TypeLandmark {
		t.Errorf("no-hint should prefer landmark, got %q", noHit.Type)
	}
}

func TestIntegration_AliasOverride(t *testing.T) {
	store, pool := setupPlacesTestDB(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO alias_overrides (alias, target_lat, target_lon, target_name)
		VALUES ('코엑스몰', 37.5119, 127.0589, '코엑스')
	`); err != nil {
		t.Fatalf("seed alias_override: %v", err)
	}

	got, err := store.FindAliasOverride(ctx, "코엑스몰")
	if err != nil {
		t.Fatalf("FindAliasOverride: %v", err)
	}
	if got.NameKo != "코엑스" {
		t.Errorf("name_ko: got %q", got.NameKo)
	}
	if got.Source != model.SourceKittypawAlias {
		t.Errorf("source: got %q", got.Source)
	}
	if got.Type != model.TypeAliasOverride {
		t.Errorf("type: got %q", got.Type)
	}
}

// TestIntegration_TypeHintMissDoesNotStarve guards against the production
// regression discovered in PR-1: detectTypeHint returns "subway_station"
// for "강남역" but Wikidata seeds 2k+ "*역" rows as type='landmark'. A
// strict WHERE filter on type would starve all those rows. The fix moves
// typeHint to an ORDER BY tiebreaker; a hint *miss* must still return the
// row, not ErrNotFound.
func TestIntegration_TypeHintMissDoesNotStarve(t *testing.T) {
	store, _ := setupPlacesTestDB(t)
	ctx := context.Background()

	// Wikidata-style: name ends with "역" but type is landmark (Wikidata
	// default — Wikidata doesn't classify these as subway_station).
	if err := store.Upsert(ctx, &model.Place{
		NameKo: "강남역", Lat: 37.4979, Lon: 127.0276,
		Type: model.TypeLandmark, Source: model.SourceWikidata,
		SourceRef: "Q17469", SourcePriority: model.PriorityWikidata,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Caller passes typeHint='subway_station' (matches the *역$ regex);
	// pre-fix this would strict-filter to zero rows and return ErrNotFound.
	got, err := store.FindExact(ctx, "강남역", model.TypeSubwayStation)
	if err != nil {
		t.Fatalf("typeHint miss should fall back, got %v", err)
	}
	if got.NameKo != "강남역" || got.Source != model.SourceWikidata {
		t.Errorf("expected wikidata 강남역, got %q (source=%s)", got.NameKo, got.Source)
	}
}

// TestIntegration_TypeHintHitWinsOverMiss confirms that when both a hint-
// matching row and a hint-mismatching row exist, the hint-matching one is
// returned first. typeHint is a soft preference, not a strict filter, but
// it must still influence ordering.
func TestIntegration_TypeHintHitWinsOverMiss(t *testing.T) {
	store, _ := setupPlacesTestDB(t)
	ctx := context.Background()

	// landmark-typed row (Wikidata) — same name.
	if err := store.Upsert(ctx, &model.Place{
		NameKo: "강남역", Lat: 37.5000, Lon: 127.0300,
		Type: model.TypeLandmark, Source: model.SourceWikidata,
		SourceRef: "Q17469", SourcePriority: model.PriorityWikidata,
	}); err != nil {
		t.Fatalf("upsert wikidata: %v", err)
	}
	// subway_station-typed row (Seoul Metro) — same name, different coords.
	if err := store.Upsert(ctx, &model.Place{
		NameKo: "강남역", Lat: 37.4979, Lon: 127.0276,
		Type: model.TypeSubwayStation, Source: model.SourceSeoulMetro,
		SourceRef: "seoul:2:강남역", SourcePriority: model.PrioritySeoulMetro,
	}); err != nil {
		t.Fatalf("upsert station: %v", err)
	}

	got, err := store.FindExact(ctx, "강남역", model.TypeSubwayStation)
	if err != nil {
		t.Fatalf("FindExact: %v", err)
	}
	if got.Source != model.SourceSeoulMetro {
		t.Errorf("typeHint=subway_station should prefer kogl_seoul_metro row, got source=%s lat=%v",
			got.Source, got.Lat)
	}
}

func TestIntegration_UpsertConflict(t *testing.T) {
	store, _ := setupPlacesTestDB(t)
	ctx := context.Background()

	first := &model.Place{
		NameKo: "광화문", Lat: 37.5760, Lon: 126.9770,
		Type: model.TypeLandmark, Source: model.SourceWikidata,
		SourceRef: "Q485034", SourcePriority: model.PriorityWikidata,
	}
	if err := store.Upsert(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updated := *first
	updated.Lat = 37.5765
	updated.Lon = 126.9775
	if err := store.Upsert(ctx, &updated); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := store.FindExact(ctx, "광화문", "")
	if err != nil {
		t.Fatalf("FindExact: %v", err)
	}
	if got.Lat != 37.5765 || got.Lon != 126.9775 {
		t.Errorf("upsert did not update coords: got (%v, %v)", got.Lat, got.Lon)
	}
}
