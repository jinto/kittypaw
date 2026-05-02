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
