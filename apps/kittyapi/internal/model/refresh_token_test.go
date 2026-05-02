//go:build integration

package model_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kittypaw-app/kittyapi/internal/model"
)

// seedDevice creates a paired device for refresh tests. Reuses
// seedDeviceUser from device_test.go (same package).
func seedDevice(t *testing.T, pool *pgxpool.Pool, userID, name string) *model.Device {
	t.Helper()
	store := model.NewDeviceStore(pool)
	dev, err := store.Create(context.Background(), userID, name, nil)
	if err != nil {
		t.Fatalf("Create device %s: %v", name, err)
	}
	return dev
}

// CreateForDevice round-trips a device-scoped refresh token. FindByHash
// must surface device_id so PR-D's reuse-detect logic can call
// RevokeAllForDevice (not RevokeAllForUser) on a device principal.
func TestRefreshTokenStore_CreateForDevice_Integration(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "rfd")
	dev := seedDevice(t, pool, user.ID, "macbook")
	store := model.NewRefreshTokenStore(pool)
	ctx := context.Background()

	hash := "device-hash-1"
	exp := time.Now().Add(30 * 24 * time.Hour)
	if err := store.CreateForDevice(ctx, user.ID, dev.ID, hash, exp); err != nil {
		t.Fatalf("CreateForDevice: %v", err)
	}

	rt, err := store.FindByHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if rt.UserID != user.ID {
		t.Fatalf("UserID = %q, want %q", rt.UserID, user.ID)
	}
	if rt.DeviceID == nil || *rt.DeviceID != dev.ID {
		t.Fatalf("DeviceID = %v, want %q", rt.DeviceID, dev.ID)
	}
}

// CreateForDevice with a deviceID that doesn't exist must surface
// model.ErrNotFound, not a generic FK error. PR-D handler maps this
// to a 404 instead of 500.
func TestRefreshTokenStore_CreateForDevice_StaleDeviceID_NotFound(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "stale")
	store := model.NewRefreshTokenStore(pool)

	err := store.CreateForDevice(
		context.Background(),
		user.ID,
		"00000000-0000-0000-0000-000000000000",
		"hash-stale",
		time.Now().Add(time.Hour),
	)
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (FK 23503 mapping)", err)
	}
}

// RevokeAllForDevice must revoke every active refresh on the target
// device and leave other devices' refresh untouched. This is the core
// reuse-detection primitive PR-D will call.
func TestRefreshTokenStore_RevokeAllForDevice_Integration(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "rev-dev")
	devA := seedDevice(t, pool, user.ID, "A")
	devB := seedDevice(t, pool, user.ID, "B")
	store := model.NewRefreshTokenStore(pool)
	ctx := context.Background()

	for _, h := range []string{"A1", "A2"} {
		if err := store.CreateForDevice(ctx, user.ID, devA.ID, h, time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("seed A: %v", err)
		}
	}
	if err := store.CreateForDevice(ctx, user.ID, devB.ID, "B1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	if err := store.RevokeAllForDevice(ctx, devA.ID); err != nil {
		t.Fatalf("RevokeAllForDevice: %v", err)
	}

	for _, h := range []string{"A1", "A2"} {
		rt, err := store.FindByHash(ctx, h)
		if err != nil {
			t.Fatalf("FindByHash %s: %v", h, err)
		}
		if rt.RevokedAt == nil {
			t.Fatalf("device A refresh %s should be revoked", h)
		}
	}
	rtB, err := store.FindByHash(ctx, "B1")
	if err != nil {
		t.Fatalf("FindByHash B1: %v", err)
	}
	if rtB.RevokedAt != nil {
		t.Fatal("device B refresh must NOT be revoked")
	}
}

// RevokeAllForDevice on a missing deviceID is a no-op (nil error).
// UPDATE matching 0 rows is not a pgx error; we keep that contract.
func TestRefreshTokenStore_RevokeAllForDevice_MissingDeviceID(t *testing.T) {
	pool := setupTestDB(t)
	store := model.NewRefreshTokenStore(pool)

	err := store.RevokeAllForDevice(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("err = %v, want nil (no-op)", err)
	}
}

// Characterization — RevokeAllForDevice preserves user (NULL device_id)
// refresh tokens. The principal types must not bleed into each other,
// or web-side reuse detection would force a daemon re-pair.
func TestRevokeAllForDevice_PreservesUserRefresh(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "preserve")
	dev := seedDevice(t, pool, user.ID, "D")
	store := model.NewRefreshTokenStore(pool)
	ctx := context.Background()

	if err := store.Create(ctx, user.ID, "user-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create user refresh: %v", err)
	}
	if err := store.CreateForDevice(ctx, user.ID, dev.ID, "dev-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateForDevice: %v", err)
	}

	if err := store.RevokeAllForDevice(ctx, dev.ID); err != nil {
		t.Fatalf("RevokeAllForDevice: %v", err)
	}

	userRT, err := store.FindByHash(ctx, "user-hash")
	if err != nil {
		t.Fatalf("FindByHash user-hash: %v", err)
	}
	if userRT.RevokedAt != nil {
		t.Fatal("user refresh must NOT be revoked by device-scoped revoke")
	}
	devRT, err := store.FindByHash(ctx, "dev-hash")
	if err != nil {
		t.Fatalf("FindByHash dev-hash: %v", err)
	}
	if devRT.RevokedAt == nil {
		t.Fatal("device refresh must be revoked")
	}
}

// Characterization — RevokeAllForUser is the user-logout primitive
// and SHOULD revoke both user-scoped (device_id NULL) and device-scoped
// refresh tokens. Plan 22 결정 5 — semantic note that the existing
// query "WHERE user_id = $1 AND revoked_at IS NULL" naturally covers
// both because device_id was added without changing the existing
// REVOKE filter.
func TestRevokeAllForUser_RevokesBoth(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "logout")
	dev := seedDevice(t, pool, user.ID, "L")
	store := model.NewRefreshTokenStore(pool)
	ctx := context.Background()

	if err := store.Create(ctx, user.ID, "user-rev", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create user refresh: %v", err)
	}
	if err := store.CreateForDevice(ctx, user.ID, dev.ID, "dev-rev", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateForDevice: %v", err)
	}

	if err := store.RevokeAllForUser(ctx, user.ID); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}

	for _, h := range []string{"user-rev", "dev-rev"} {
		rt, err := store.FindByHash(ctx, h)
		if err != nil {
			t.Fatalf("FindByHash %s: %v", h, err)
		}
		if rt.RevokedAt == nil {
			t.Fatalf("refresh %s must be revoked by user-scoped revoke", h)
		}
	}
}

// RotateForDevice atomicity — Plan 23 follow-up review HIGH 0.85 fix.
//
// The pre-fix sequence (RevokeIfActive then CreateForDevice as separate
// pool operations) had a window where the old refresh got revoked but
// the new one failed to insert; the daemon's retry then presented the
// already-revoked token, tripping reuse detection and self-locking the
// device. RotateForDevice runs both operations in a single transaction
// so partial failure rolls back — the daemon retry sees the original
// active row and succeeds.

// TestRotateForDevice_Happy: old row revoked, new row inserted, both
// observable in a single FindByHash round-trip.
func TestRotateForDevice_Happy(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "rot1")
	dev := seedDevice(t, pool, user.ID, "D")
	store := model.NewRefreshTokenStore(pool)
	ctx := context.Background()

	if err := store.CreateForDevice(ctx, user.ID, dev.ID, "old-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	old, _ := store.FindByHash(ctx, "old-hash")

	if err := store.RotateForDevice(ctx, old.ID, user.ID, dev.ID, "new-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("RotateForDevice: %v", err)
	}

	// Old should be revoked.
	got, _ := store.FindByHash(ctx, "old-hash")
	if got.RevokedAt == nil {
		t.Fatal("old refresh must be revoked")
	}
	// New should be active.
	newRT, err := store.FindByHash(ctx, "new-hash")
	if err != nil {
		t.Fatalf("FindByHash new-hash: %v", err)
	}
	if newRT.RevokedAt != nil {
		t.Fatal("new refresh must be active")
	}
	if newRT.DeviceID == nil || *newRT.DeviceID != dev.ID {
		t.Fatalf("new refresh device_id = %v, want %q", newRT.DeviceID, dev.ID)
	}
}

// TestRotateForDevice_StaleDeviceID_OldRowPreserved: when the new
// INSERT fails (FK violation here — non-existent deviceID for the new
// row), the transaction rolls back. Old row must remain ACTIVE so the
// daemon's retry succeeds. This is the actual fix — pre-fix code
// would have left the old row revoked.
func TestRotateForDevice_StaleDeviceID_OldRowPreserved(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "rot2")
	dev := seedDevice(t, pool, user.ID, "D")
	store := model.NewRefreshTokenStore(pool)
	ctx := context.Background()

	if err := store.CreateForDevice(ctx, user.ID, dev.ID, "preserve-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	old, _ := store.FindByHash(ctx, "preserve-hash")

	// Trigger FK 23503 on the new row by passing a non-existent deviceID.
	stale := "00000000-0000-0000-0000-000000000000"
	err := store.RotateForDevice(ctx, old.ID, user.ID, stale, "new-hash", time.Now().Add(time.Hour))
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (FK violation rollback)", err)
	}

	// Old row MUST remain active — transaction rolled back.
	got, _ := store.FindByHash(ctx, "preserve-hash")
	if got.RevokedAt != nil {
		t.Fatal("old refresh must remain active after rollback (atomicity)")
	}
	// New row must NOT exist.
	if _, err := store.FindByHash(ctx, "new-hash"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("new refresh found despite rollback: %v", err)
	}
}

// TestRotateForDevice_OldAlreadyRevoked: the rotation primitive itself
// should fail when the old row was already revoked — the caller's
// reuse-detection branch handles that case before reaching here.
func TestRotateForDevice_OldAlreadyRevoked(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "rot3")
	dev := seedDevice(t, pool, user.ID, "D")
	store := model.NewRefreshTokenStore(pool)
	ctx := context.Background()

	if err := store.CreateForDevice(ctx, user.ID, dev.ID, "rev-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	old, _ := store.FindByHash(ctx, "rev-hash")
	_, _ = store.RevokeIfActive(ctx, old.ID)

	err := store.RotateForDevice(ctx, old.ID, user.ID, dev.ID, "after-rev", time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected error when rotating an already-revoked row")
	}
	// New row must NOT have been inserted.
	if _, err := store.FindByHash(ctx, "after-rev"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("new refresh must not exist when old was already revoked")
	}
}

// TestRefreshTokenStore_DeleteExpiredOlderThan pins the 30-day expired
// retention boundary. Both expired-revoked and expired-active rows
// (revoked_at IS NULL but expires_at past) get deleted — once a row
// is past its expiry, retention is purely forensic regardless of
// revoke state. Rows whose expires_at is still in the future MUST be
// untouched, even if revoked.
func TestRefreshTokenStore_DeleteExpiredOlderThan(t *testing.T) {
	pool := setupTestDB(t)
	user := seedDeviceUser(t, pool, "delexp")
	dev := seedDevice(t, pool, user.ID, "D")
	store := model.NewRefreshTokenStore(pool)
	ctx := context.Background()

	now := time.Now().UTC()

	// Seed three rows: expired 60 days ago / expired 5 days ago /
	// not yet expired (1 day from now).
	if err := store.CreateForDevice(ctx, user.ID, dev.ID, "long-expired", now.Add(-60*24*time.Hour)); err != nil {
		t.Fatalf("seed long-expired: %v", err)
	}
	if err := store.CreateForDevice(ctx, user.ID, dev.ID, "recently-expired", now.Add(-5*24*time.Hour)); err != nil {
		t.Fatalf("seed recently-expired: %v", err)
	}
	if err := store.CreateForDevice(ctx, user.ID, dev.ID, "active", now.Add(24*time.Hour)); err != nil {
		t.Fatalf("seed active: %v", err)
	}

	cutoff := now.Add(-30 * 24 * time.Hour)
	deleted, err := store.DeleteExpiredOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteExpiredOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted count = %d, want 1", deleted)
	}

	// long-expired should be gone.
	if _, err := store.FindByHash(ctx, "long-expired"); !errors.Is(err, model.ErrNotFound) {
		t.Error("long-expired row not deleted")
	}
	// recently-expired and active must remain.
	if _, err := store.FindByHash(ctx, "recently-expired"); err != nil {
		t.Errorf("recently-expired row incorrectly deleted: %v", err)
	}
	if _, err := store.FindByHash(ctx, "active"); err != nil {
		t.Errorf("active row incorrectly deleted: %v", err)
	}
}
