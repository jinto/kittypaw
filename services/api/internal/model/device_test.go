//go:build integration

package model_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kittypaw-app/kittyapi/internal/model"
)

// seedDeviceUser creates a user that owns the test devices. Must run
// before any device CRUD because devices.user_id has FK to users(id).
func seedDeviceUser(t *testing.T, pool *pgxpool.Pool, name string) *model.User {
	t.Helper()
	users := model.NewUserStore(pool)
	u, err := users.CreateOrUpdate(context.Background(), "google", "test-"+name, name+"@test.com", name, "")
	if err != nil {
		t.Fatalf("seed user %s: %v", name, err)
	}
	return u
}

// TestDeviceStore_CreateAndFindByID_Integration pins the round-trip:
// Create returns a device with stable ID, paired_at set; FindByID
// returns the same row. Anchors interface design — a missing method
// or a wrong jsonb wire format surfaces here first.
func TestDeviceStore_CreateAndFindByID_Integration(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "alpha")

	store := model.NewDeviceStore(pool)
	ctx := context.Background()

	caps := map[string]any{"daemon_version": "0.1.0", "protocols": []string{"wss"}}
	created, err := store.Create(ctx, user.ID, "macbook", caps)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty device ID")
	}
	if created.UserID != user.ID {
		t.Fatalf("UserID = %q, want %q", created.UserID, user.ID)
	}
	if created.Name != "macbook" {
		t.Fatalf("Name = %q, want macbook", created.Name)
	}
	if created.PairedAt.IsZero() {
		t.Fatal("expected non-zero PairedAt")
	}

	found, err := store.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("FindByID ID = %q, want %q", found.ID, created.ID)
	}
	if got := found.Capabilities["daemon_version"]; got != "0.1.0" {
		t.Fatalf("Capabilities[daemon_version] = %v, want 0.1.0", got)
	}
}

// FindByID on a missing UUID must surface model.ErrNotFound. PR-D
// pair/list/delete handlers map this to a 404; a generic pgx error
// would 500 instead.
func TestDeviceStore_FindByID_NotFound(t *testing.T) {
	pool := setupTestDB(t)
	store := model.NewDeviceStore(pool)

	_, err := store.FindByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Revoked devices remain queryable through FindByID — PR-D needs to
// distinguish "missing" (404) from "already revoked" (409 or 200 with
// revoked_at body field). Returning ErrNotFound for revoked rows would
// collapse both cases into a 404 and lose audit visibility.
func TestDeviceStore_FindByID_RevokedDevice_Returns(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "rev")
	store := model.NewDeviceStore(pool)
	ctx := context.Background()

	dev, err := store.Create(ctx, user.ID, "phone", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Revoke(ctx, dev.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	got, err := store.FindByID(ctx, dev.ID)
	if err != nil {
		t.Fatalf("FindByID after Revoke: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set after Revoke")
	}
}

// ListActiveForUser must filter on revoked_at IS NULL. The partial
// index on devices(user_id) WHERE revoked_at IS NULL backs this query
// — if the WHERE clause drifts, the index stops being used and this
// test catches the silent regression.
func TestDeviceStore_ListActiveForUser_FiltersRevoked(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "list")
	store := model.NewDeviceStore(pool)
	ctx := context.Background()

	active, err := store.Create(ctx, user.ID, "active", nil)
	if err != nil {
		t.Fatalf("Create active: %v", err)
	}
	revoked, err := store.Create(ctx, user.ID, "revoked", nil)
	if err != nil {
		t.Fatalf("Create revoked: %v", err)
	}
	if err := store.Revoke(ctx, revoked.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	list, err := store.ListActiveForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListActiveForUser: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d devices, want 1 (active only)", len(list))
	}
	if list[0].ID != active.ID {
		t.Fatalf("got device %q, want %q", list[0].ID, active.ID)
	}
}

// Revoke on a missing UUID returns ErrNotFound, distinct from
// already-revoked which is idempotent nil.
func TestDeviceStore_Revoke_NotFound(t *testing.T) {
	pool := setupTestDB(t)
	store := model.NewDeviceStore(pool)

	err := store.Revoke(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Revoke is idempotent. Two calls on the same device both return nil
// (no false ErrNotFound on the second call), and revoked_at preserves
// the FIRST timestamp — overwriting would corrupt the audit signal
// "when did this device first lose authorization".
func TestDeviceStore_Revoke_Idempotent(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "idem")
	store := model.NewDeviceStore(pool)
	ctx := context.Background()

	dev, err := store.Create(ctx, user.ID, "tablet", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Revoke(ctx, dev.ID); err != nil {
		t.Fatalf("first Revoke: %v", err)
	}
	first, err := store.FindByID(ctx, dev.ID)
	if err != nil {
		t.Fatalf("FindByID after first Revoke: %v", err)
	}
	firstRevokedAt := *first.RevokedAt

	if err := store.Revoke(ctx, dev.ID); err != nil {
		t.Fatalf("second Revoke (idempotent): %v", err)
	}
	second, err := store.FindByID(ctx, dev.ID)
	if err != nil {
		t.Fatalf("FindByID after second Revoke: %v", err)
	}
	if !second.RevokedAt.Equal(firstRevokedAt) {
		t.Fatalf("second Revoke overwrote revoked_at: %v → %v", firstRevokedAt, *second.RevokedAt)
	}
}

// TestDeviceStore_Touch_SetsLastUsedAt pins the Plan 24 T1 contract:
// Touch on an active device must set last_used_at to the current
// time. The janitor's idle-reaping logic depends on this column being
// fresh — if Touch silently failed to write, every active device
// would look idle and get reaped on day 60.
func TestDeviceStore_Touch_SetsLastUsedAt(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "touch")
	store := model.NewDeviceStore(pool)
	ctx := context.Background()

	dev, err := store.Create(ctx, user.ID, "n1", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dev.LastUsedAt != nil {
		t.Fatalf("Create: expected last_used_at NULL, got %v", *dev.LastUsedAt)
	}

	if err := store.Touch(ctx, dev.ID); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got, err := store.FindByID(ctx, dev.ID)
	if err != nil {
		t.Fatalf("FindByID after Touch: %v", err)
	}
	if got.LastUsedAt == nil {
		t.Fatal("Touch did not set last_used_at")
	}
}

// TestDeviceStore_Touch_NoOpForRevoked: Touch on a revoked device
// must NOT resurrect last_used_at — that would partially un-revoke
// in the eyes of the janitor and confuse forensic queries. The
// `WHERE revoked_at IS NULL` clause in Touch's UPDATE is the guard.
func TestDeviceStore_Touch_NoOpForRevoked(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "touch-rev")
	store := model.NewDeviceStore(pool)
	ctx := context.Background()

	dev, err := store.Create(ctx, user.ID, "n1", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Revoke(ctx, dev.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Touch must succeed (nil error) but write nothing.
	if err := store.Touch(ctx, dev.ID); err != nil {
		t.Fatalf("Touch on revoked: %v", err)
	}

	got, _ := store.FindByID(ctx, dev.ID)
	if got.LastUsedAt != nil {
		t.Fatalf("Touch on revoked device wrote last_used_at = %v", *got.LastUsedAt)
	}
}

// TestDeviceStore_ReapIdle pins the 60-day idle policy boundary:
// devices whose latest activity (last_used_at, or paired_at as
// fallback) predates the cutoff get soft-revoked; everything newer
// stays active. Off-by-one bugs here would either reap recently-paired
// devices or leak truly-idle ones — both fail-silent in production.
func TestDeviceStore_ReapIdle(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "reap")
	store := model.NewDeviceStore(pool)
	ctx := context.Background()

	idle, _ := store.Create(ctx, user.ID, "idle", nil)
	fresh, _ := store.Create(ctx, user.ID, "fresh", nil)
	alreadyRevoked, _ := store.Create(ctx, user.ID, "rev", nil)
	if err := store.Revoke(ctx, alreadyRevoked.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revokedSnapshot, _ := store.FindByID(ctx, alreadyRevoked.ID)

	// Push idle's paired_at way back; bump fresh's last_used_at to now.
	// Bypass DeviceStore — direct SQL is needed to forge old timestamps.
	now := time.Now().UTC()
	farPast := now.Add(-100 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE devices SET paired_at = $1, last_used_at = NULL WHERE id = $2`, farPast, idle.ID); err != nil {
		t.Fatalf("forge idle: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE devices SET last_used_at = $1 WHERE id = $2`, now, fresh.ID); err != nil {
		t.Fatalf("forge fresh: %v", err)
	}

	cutoff := now.Add(-60 * 24 * time.Hour)
	reaped, err := store.ReapIdle(ctx, cutoff)
	if err != nil {
		t.Fatalf("ReapIdle: %v", err)
	}
	if reaped != 1 {
		t.Errorf("reaped count = %d, want 1", reaped)
	}

	idleAfter, _ := store.FindByID(ctx, idle.ID)
	if idleAfter.RevokedAt == nil {
		t.Error("idle device was not revoked")
	}
	freshAfter, _ := store.FindByID(ctx, fresh.ID)
	if freshAfter.RevokedAt != nil {
		t.Error("fresh device was incorrectly revoked")
	}
	revokedAfter, _ := store.FindByID(ctx, alreadyRevoked.ID)
	if !revokedAfter.RevokedAt.Equal(*revokedSnapshot.RevokedAt) {
		// ReapIdle must skip already-revoked rows (WHERE revoked_at IS NULL).
		// Touching them would overwrite the original revoke timestamp,
		// breaking forensic timelines.
		t.Error("ReapIdle clobbered an already-revoked row's timestamp")
	}
}

// TestDeviceStore_DeleteRevokedOlderThan pins the 90-day retention
// hard-delete boundary. Rows past cutoff get deleted; rows still in
// the retention window survive. CASCADE behavior on
// refresh_tokens.device_id is exercised by the janitor end-to-end
// integration test.
func TestDeviceStore_DeleteRevokedOlderThan(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "delrev")
	store := model.NewDeviceStore(pool)
	ctx := context.Background()

	old, _ := store.Create(ctx, user.ID, "old-revoked", nil)
	recent, _ := store.Create(ctx, user.ID, "recent-revoked", nil)
	active, _ := store.Create(ctx, user.ID, "active", nil)

	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `UPDATE devices SET revoked_at = $1 WHERE id = $2`, now.Add(-100*24*time.Hour), old.ID); err != nil {
		t.Fatalf("forge old: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE devices SET revoked_at = $1 WHERE id = $2`, now.Add(-30*24*time.Hour), recent.ID); err != nil {
		t.Fatalf("forge recent: %v", err)
	}

	cutoff := now.Add(-90 * 24 * time.Hour)
	deleted, err := store.DeleteRevokedOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteRevokedOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted count = %d, want 1", deleted)
	}

	if _, err := store.FindByID(ctx, old.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("old revoked device not deleted: %v", err)
	}
	if _, err := store.FindByID(ctx, recent.ID); err != nil {
		t.Errorf("recent revoked device incorrectly deleted: %v", err)
	}
	if _, err := store.FindByID(ctx, active.ID); err != nil {
		t.Errorf("active device incorrectly affected: %v", err)
	}
}

// The CHECK (jsonb_typeof = 'object') constraint on devices.capabilities
// must reject non-object jsonb (arrays, strings, numbers). Tested by
// raw SQL since DeviceStore.Create takes map[string]any and can't
// produce a non-object value. PostgreSQL signals constraint violation
// with SQLSTATE 23514.
func TestDeviceStore_CapabilitiesCheckRejectsNonObject(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "chk")
	ctx := context.Background()

	cases := []struct {
		name  string
		jsonb string
	}{
		{"array", `'[]'`},
		{"string", `'"foo"'`},
		{"number", `'42'`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := pool.Exec(ctx,
				`INSERT INTO devices (user_id, capabilities) VALUES ($1, `+c.jsonb+`::jsonb)`,
				user.ID,
			)
			if err == nil {
				t.Fatalf("expected CHECK violation, got nil")
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("expected SQLSTATE 23514, got %v", err)
			}
		})
	}
}
