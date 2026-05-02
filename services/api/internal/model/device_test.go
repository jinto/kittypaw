//go:build integration

package model_test

import (
	"context"
	"errors"
	"testing"

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
