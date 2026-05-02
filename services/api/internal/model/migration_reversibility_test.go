//go:build integration

package model_test

import (
	"errors"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// TestMigrationReversibility_006_007_008 pins Plan 22 PR-C + Plan 24
// reversibility: `up 8 → down 3 → up 3` must complete without leaving
// the migrate state dirty. golang-migrate marks a migration `dirty=true`
// if the SQL fails mid-transaction; subsequent calls then refuse until
// `migrate force <ver>` is run by an operator. Failing here means a
// recent migration's down path is non-recoverable in CI/dev — block
// the merge until fixed.
func TestMigrationReversibility_006_007_008(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping migration reversibility test")
	}

	m, err := migrate.New("file://../../migrations", "pgx5://"+stripScheme(dbURL))
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}

	// Reset to ground truth — m.Drop wipes data AND schema_migrations, so
	// we sidestep 007's "refresh_tokens contains device_id rows" abort
	// guard, which would fire on m.Down() when prior tests in the package
	// left device-scoped refresh rows behind. Mirrors setupTestDB.
	if err := m.Drop(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("initial Drop: %v", err)
	}
	_, _ = m.Close()
	m, err = migrate.New("file://../../migrations", "pgx5://"+stripScheme(dbURL))
	if err != nil {
		t.Fatalf("migrate.New (post-drop): %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	// Up to latest (006 + 007 + 008 included).
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("Up to latest: %v", err)
	}
	v1, dirty1, err := m.Version()
	if err != nil {
		t.Fatalf("Version after first Up: %v", err)
	}
	if v1 != 8 || dirty1 {
		t.Fatalf("after Up: version=%d dirty=%v, want 8/false", v1, dirty1)
	}

	// Down 3 (back through 008 + 007 + 006).
	if err := m.Steps(-3); err != nil {
		t.Fatalf("Steps(-3): %v", err)
	}
	v2, dirty2, err := m.Version()
	if err != nil {
		t.Fatalf("Version after Steps(-3): %v", err)
	}
	if v2 != 5 || dirty2 {
		t.Fatalf("after Down 3: version=%d dirty=%v, want 5/false", v2, dirty2)
	}

	// Up 3 again — full re-application.
	if err := m.Steps(3); err != nil {
		t.Fatalf("Steps(3): %v", err)
	}
	v3, dirty3, err := m.Version()
	if err != nil {
		t.Fatalf("Version after Steps(3): %v", err)
	}
	if v3 != 8 || dirty3 {
		t.Fatalf("after Up 3: version=%d dirty=%v, want 8/false", v3, dirty3)
	}
}
