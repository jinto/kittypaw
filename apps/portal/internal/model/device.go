package model

import (
	"context"
	"time"
)

// Device represents a paired daemon instance authorized to maintain a
// long-lived WSS connection on behalf of a user. Plan 22 PR-C — schema
// + store layer only; endpoints land in PR-D.
//
// docs/specs/kittychat-credential-foundation.md (D5 device JWT shape).
type Device struct {
	ID              string         `json:"device_id"`
	UserID          string         `json:"-"`
	Name            string         `json:"name"`
	Capabilities    map[string]any `json:"capabilities"`
	PairedAt        time.Time      `json:"paired_at"`
	LastUsedAt      *time.Time     `json:"last_used_at,omitempty"`
	LastConnectedAt *time.Time     `json:"last_connected_at,omitempty"`
	RevokedAt       *time.Time     `json:"-"`
}

// DeviceStore is the persistence seam for devices.
//
// Error contracts:
//   - FindByID: missing → ErrNotFound; revoked → returned with RevokedAt set
//   - Revoke: missing → ErrNotFound; already-revoked → no-op (nil error,
//     revoked_at preserves the first revoke timestamp)
//   - Touch: missing or already-revoked → no-op (nil error, RowsAffected=0).
//     Best-effort; callers must not depend on it for security decisions.
//   - ReapIdle / DeleteRevokedOlderThan: janitor-only. LIMIT-batched DELETE
//     so a multi-million-row sweep never holds long locks.
type DeviceStore interface {
	Create(ctx context.Context, userID, name string, capabilities map[string]any) (*Device, error)
	FindByID(ctx context.Context, id string) (*Device, error)
	ListActiveForUser(ctx context.Context, userID string) ([]*Device, error)
	Revoke(ctx context.Context, id string) error

	// Touch updates last_used_at = now() for an active device. Used by
	// HandleDeviceRefresh as best-effort idle signal — not in the rotate
	// transaction, since a Touch failure must not roll back a successful
	// refresh. Plan 24 T1.
	Touch(ctx context.Context, id string) error

	// ReapIdle soft-revokes every active device whose latest activity
	// (COALESCE(last_used_at, paired_at)) predates olderThan. Returns the
	// count revoked. Plan 24 T2 — janitor 60-day idle policy.
	ReapIdle(ctx context.Context, olderThan time.Time) (int64, error)

	// DeleteRevokedOlderThan hard-deletes devices whose revoked_at is
	// older than olderThan. refresh_tokens.device_id ON DELETE CASCADE
	// reaps the orphan refresh rows automatically. Plan 24 T2 — janitor
	// 90-day revoked retention.
	DeleteRevokedOlderThan(ctx context.Context, olderThan time.Time) (int64, error)
}
