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

// setupAddressesTestDB resets the schema (down → up through migration 005)
// and returns a pgxpool ready for addresses-table tests. Mirrors the
// places setup but truncates addresses too. T2/T3/T5 tests will reuse it
// once model/address.go lands.
func setupAddressesTestDB(t *testing.T) *pgxpool.Pool {
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

	// CASCADE intentionally omitted — addresses has no inbound FKs in
	// migration 005. If a future migration adds e.g. place_address_links
	// referencing addresses.id, this TRUNCATE will fail loudly — that's
	// the desired signal to revisit the test isolation strategy.
	if _, err := pool.Exec(ctx, "TRUNCATE addresses RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate addresses: %v", err)
	}
	return pool
}

// TestIntegration_AddressesTableExists verifies migration 005 created the
// addresses table with all expected columns. SELECT against an empty table
// with LIMIT 0 succeeds only when both the table and column references
// resolve.
func TestIntegration_AddressesTableExists(t *testing.T) {
	pool := setupAddressesTestDB(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
        SELECT id, road_address, road_address_normalized, jibun_address,
               building_name, lat, lon, pnu, region_sido, region_sigungu,
               imported_at
        FROM addresses
        LIMIT 0
    `)
	if err != nil {
		t.Fatalf("addresses table check: %v", err)
	}
	rows.Close()
}

// TestIntegration_AddressesIndexes verifies all indexes from migration 005
// exist. gin_trgm_ops indexes back fuzzy search; the composite index backs
// region-prefixed exact lookups.
func TestIntegration_AddressesIndexes(t *testing.T) {
	pool := setupAddressesTestDB(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
        SELECT indexname FROM pg_indexes
        WHERE tablename = 'addresses'
        ORDER BY indexname
    `)
	if err != nil {
		t.Fatalf("pg_indexes query: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = true
	}

	want := []string{
		"addresses_pkey",    // BIGSERIAL primary
		"addresses_pnu_key", // UNIQUE (pnu)
		"idx_addresses_road_address_normalized_trgm", // gin_trgm fuzzy
		"idx_addresses_building_name_trgm",           // gin_trgm partial
		"idx_addresses_region",                       // composite (sido, sigungu)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing index: %s (got=%v)", w, got)
		}
	}
}
