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

// TestMigrationReversibility_006_007 pins Plan 22 PR-C reversibility:
// `up 7 → down 2 → up 7` must complete without leaving the migrate
// state dirty. golang-migrate marks a migration `dirty=true` if the
// SQL fails mid-transaction; subsequent calls then refuse until
// `migrate force <ver>` is run by an operator. Failing here means the
// 006 or 007 migration's down path is non-recoverable in CI/dev — block
// the merge until fixed.
func TestMigrationReversibility_006_007(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping migration reversibility test")
	}

	m, err := migrate.New("file://../../migrations", "pgx5://"+stripScheme(dbURL))
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	// Reset to ground truth — fully down (ignore ErrNoChange when DB is
	// already empty from a prior run).
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("initial Down: %v", err)
	}

	// Up to 7 (006 + 007 included).
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("Up to 7: %v", err)
	}
	v1, dirty1, err := m.Version()
	if err != nil {
		t.Fatalf("Version after first Up: %v", err)
	}
	if v1 != 7 || dirty1 {
		t.Fatalf("after Up: version=%d dirty=%v, want 7/false", v1, dirty1)
	}

	// Down 2 (back through 007 + 006).
	if err := m.Steps(-2); err != nil {
		t.Fatalf("Steps(-2): %v", err)
	}
	v2, dirty2, err := m.Version()
	if err != nil {
		t.Fatalf("Version after Steps(-2): %v", err)
	}
	if v2 != 5 || dirty2 {
		t.Fatalf("after Down 2: version=%d dirty=%v, want 5/false", v2, dirty2)
	}

	// Up 2 again — full re-application.
	if err := m.Steps(2); err != nil {
		t.Fatalf("Steps(2): %v", err)
	}
	v3, dirty3, err := m.Version()
	if err != nil {
		t.Fatalf("Version after Steps(2): %v", err)
	}
	if v3 != 7 || dirty3 {
		t.Fatalf("after Up 2: version=%d dirty=%v, want 7/false", v3, dirty3)
	}
}
