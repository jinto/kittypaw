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

// DeviceStore is the persistence seam for devices. UpdateLastUsed and
// UpdateLastConnected are deliberately omitted in PR-C (CEO scope cut)
// — daemon WSS touch is a future plan.
//
// Error contracts:
//   - FindByID: missing → ErrNotFound; revoked → returned with RevokedAt set
//   - Revoke: missing → ErrNotFound; already-revoked → no-op (nil error,
//     revoked_at preserves the first revoke timestamp)
type DeviceStore interface {
	Create(ctx context.Context, userID, name string, capabilities map[string]any) (*Device, error)
	FindByID(ctx context.Context, id string) (*Device, error)
	ListActiveForUser(ctx context.Context, userID string) ([]*Device, error)
	Revoke(ctx context.Context, id string) error
}
