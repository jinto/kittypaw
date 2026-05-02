//go:build integration

package janitor_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kittypaw-app/kittyportal/internal/janitor"
	"github.com/kittypaw-app/kittyportal/internal/model"
)

// setupJanitorDB mirrors model.setupTestDB but lives in this package so
// the janitor integration test doesn't depend on internal model helpers.
// Cost: ~25 lines duplicated; benefit: package boundary respected.
func setupJanitorDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://kittypaw:kittypaw@localhost:5432/kittypaw_api_test?sslmode=disable"
	}
	if !strings.Contains(dbURL, "_test") {
		t.Fatalf("DATABASE_URL must point at a test DB; got %q", dbURL)
	}

	m, err := migrate.New("file://../../migrations", "pgx5://"+stripScheme(dbURL))
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Drop(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate drop: %v", err)
	}
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

	_, _ = pool.Exec(ctx, "DELETE FROM refresh_tokens")
	_, _ = pool.Exec(ctx, "DELETE FROM devices")
	_, _ = pool.Exec(ctx, "DELETE FROM users")

	return pool
}

func stripScheme(url string) string {
	for i, c := range url {
		if c == ':' && i > 0 {
			return url[i+3:]
		}
	}
	return url
}

// frozenIntegrationClock returns a fixed time so the test controls
// every cutoff without relying on time.Now's drift.
type frozenIntegrationClock struct{ t time.Time }

func (c frozenIntegrationClock) Now() time.Time                         { return c.t }
func (c frozenIntegrationClock) After(_ time.Duration) <-chan time.Time { return nil }

// TestJanitor_Tick_EndToEnd is the contract test that pins everything
// from "schema is wired" through "ON DELETE CASCADE actually fires
// when DeleteRevokedOlderThan removes a device row." Failure here
// means the daily janitor run won't do what main.go thinks it does —
// silent prod data accumulation, the kind that surfaces only when
// the table size graph blows up months later.
//
// Fixture (now = 2026-05-01 UTC):
//   - device A: paired 100d ago, last_used_at NULL (idle ≥ 60d)
//   - device B: paired 10d ago, last_used_at now (fresh)
//   - device C: paired 200d ago, revoked 100d ago (past 90d retention)
//   - refresh α: device A, expires 1d in future (active)
//   - refresh β: device B, expires 60d ago (long-expired ≥ 30d)
//   - refresh γ: device C, expires 50d ago (long-expired AND on a
//     hard-delete-target device — exercises CASCADE)
//   - refresh δ: device B, expires 5d ago (recently expired, in window)
//
// Expected after Tick:
//   - device A revoked (idle reaped)
//   - device B still active
//   - device C hard-deleted
//   - refresh α still present (its device A is still around, just revoked)
//   - refresh β deleted (long-expired)
//   - refresh γ gone via CASCADE (its device C was deleted)
//   - refresh δ still present (within 30d retention)
func TestJanitor_Tick_EndToEnd(t *testing.T) {
	pool := setupJanitorDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// Seed user.
	users := model.NewUserStore(pool)
	user, err := users.CreateOrUpdate(ctx, "google", "test-jan", "j@t.com", "Janitor", "")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	devs := model.NewDeviceStore(pool)
	refresh := model.NewRefreshTokenStore(pool)

	// Devices.
	devA, _ := devs.Create(ctx, user.ID, "idle", nil)
	devB, _ := devs.Create(ctx, user.ID, "fresh", nil)
	devC, _ := devs.Create(ctx, user.ID, "old-revoked", nil)

	// Forge timestamps. paired_at + last_used_at + revoked_at all set
	// directly because Create stamps now() — only direct SQL forges old
	// fixture state.
	if _, err := pool.Exec(ctx, `UPDATE devices SET paired_at=$1, last_used_at=NULL WHERE id=$2`,
		now.Add(-100*24*time.Hour), devA.ID); err != nil {
		t.Fatalf("forge devA: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE devices SET paired_at=$1, last_used_at=$2 WHERE id=$3`,
		now.Add(-10*24*time.Hour), now, devB.ID); err != nil {
		t.Fatalf("forge devB: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE devices SET paired_at=$1, revoked_at=$2 WHERE id=$3`,
		now.Add(-200*24*time.Hour), now.Add(-100*24*time.Hour), devC.ID); err != nil {
		t.Fatalf("forge devC: %v", err)
	}

	// Refresh tokens.
	if err := refresh.CreateForDevice(ctx, user.ID, devA.ID, "alpha", now.Add(24*time.Hour)); err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	if err := refresh.CreateForDevice(ctx, user.ID, devB.ID, "beta", now.Add(-60*24*time.Hour)); err != nil {
		t.Fatalf("seed beta: %v", err)
	}
	if err := refresh.CreateForDevice(ctx, user.ID, devC.ID, "gamma", now.Add(-50*24*time.Hour)); err != nil {
		t.Fatalf("seed gamma: %v", err)
	}
	if err := refresh.CreateForDevice(ctx, user.ID, devB.ID, "delta", now.Add(-5*24*time.Hour)); err != nil {
		t.Fatalf("seed delta: %v", err)
	}

	// Run.
	j := janitor.New(devs, refresh, janitor.DefaultPolicy, frozenIntegrationClock{t: now})
	j.Tick(ctx)

	// Devices: A revoked, B intact, C deleted.
	a, err := devs.FindByID(ctx, devA.ID)
	if err != nil {
		t.Fatalf("devA lookup after tick: %v", err)
	}
	if a.RevokedAt == nil {
		t.Error("devA was not reaped (idle ≥ 60d)")
	}
	b, err := devs.FindByID(ctx, devB.ID)
	if err != nil {
		t.Fatalf("devB lookup after tick: %v", err)
	}
	if b.RevokedAt != nil {
		t.Error("devB was incorrectly reaped (fresh)")
	}
	if _, err := devs.FindByID(ctx, devC.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("devC was not hard-deleted (revoked > 90d): %v", err)
	}

	// Refresh tokens: α/δ present, β deleted (long-expired), γ gone via CASCADE.
	if _, err := refresh.FindByHash(ctx, "alpha"); err != nil {
		t.Errorf("alpha token incorrectly deleted: %v", err)
	}
	if _, err := refresh.FindByHash(ctx, "beta"); !errors.Is(err, model.ErrNotFound) {
		t.Error("beta token (long-expired) was not deleted")
	}
	if _, err := refresh.FindByHash(ctx, "gamma"); !errors.Is(err, model.ErrNotFound) {
		t.Error("gamma token was not removed via CASCADE on devC delete")
	}
	if _, err := refresh.FindByHash(ctx, "delta"); err != nil {
		t.Errorf("delta token (recently expired, in retention) incorrectly deleted: %v", err)
	}
}
