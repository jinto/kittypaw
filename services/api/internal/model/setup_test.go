//go:build integration

package model_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kittypaw-app/kittyapi/internal/model"
)

// setupTestDB resets the schema (down → up through the latest migration)
// and returns a pgxpool ready for user/refresh_token/device tests in
// this package. Other test groups (places, addresses) maintain their
// own setup helpers because their fixture-clean steps differ.
//
// Plan 22 PR-C: signature changed from *PostgresUserStore to *pgxpool.Pool
// so the same helper covers DeviceStore + RefreshTokenStore tests.
// Cleanup order matches FK direction: refresh_tokens → devices → users.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kittypaw:kittypaw@localhost:5432/kittypaw_api_test?sslmode=disable"
	}

	// Refuse to run m.Drop() unless the DSN names a test database.
	// Mirrors the convention from /me + refresh integration tests
	// (me_integration_test.go:50, refresh_rotation_integration_test.go:47):
	// a misconfigured DATABASE_URL pointing at staging/prod must abort,
	// not silently nuke schema_migrations and every table.
	if !strings.Contains(dbURL, "_test") {
		t.Fatalf("DATABASE_URL must point at a test DB (must contain \"_test\"); got %q", dbURL)
	}

	// Hard reset: nuke every object the database holds, then migrate up
	// from scratch. Plan 22 PR-C added 007's `RAISE EXCEPTION` abort
	// guard, which makes a sequence of `migrate down → up` per test
	// brittle: any prior test that left `device_id` rows trips the
	// guard and marks schema_migrations dirty. `m.Drop()` sidesteps
	// the down-migration entirely, drops every table including
	// schema_migrations, and lets m.Up() rebuild deterministically.
	// Cost: a few ms per test for a fresh schema.
	m, err := migrate.New("file://../../migrations", "pgx5://"+stripScheme(dbURL))
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Drop(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate drop: %v", err)
	}
	// `Drop()` closes the underlying database connection — re-create
	// the migrate instance for the subsequent Up().
	_, _ = m.Close()
	m, err = migrate.New("file://../../migrations", "pgx5://"+stripScheme(dbURL))
	if err != nil {
		t.Fatalf("migrate new (post-drop): %v", err)
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

	// Clean tables for test isolation.
	_, _ = pool.Exec(ctx, "DELETE FROM refresh_tokens")
	_, _ = pool.Exec(ctx, "DELETE FROM devices")
	_, _ = pool.Exec(ctx, "DELETE FROM users")

	return pool
}

func stripScheme(url string) string {
	for i, c := range url {
		if c == ':' && i > 0 {
			return url[i+3:] // skip "://"
		}
	}
	return url
}
