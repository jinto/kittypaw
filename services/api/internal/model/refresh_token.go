package model

import (
	"context"
	"time"
)

type RefreshToken struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	DeviceID  *string    `json:"device_id,omitempty"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// RefreshTokenStore is the persistence seam for both user-scoped
// (device_id NULL) and device-scoped refresh tokens. Plan 22 PR-C —
// 결정 2: single-table design with nullable device_id; principal type
// is data-dependent. Revisit if a 3rd device-only column appears.
//
// Revocation semantics:
//   - RevokeIfActive(id): single-token rotation primitive
//   - RevokeAllForUser(userID): user-logout — covers both user AND
//     device refresh on this user (the existing WHERE user_id filter
//     was always principal-agnostic; column add doesn't change that)
//   - RevokeAllForDevice(deviceID): device delete or device-side reuse
//     detection — preserves user refresh
type RefreshTokenStore interface {
	Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	CreateForDevice(ctx context.Context, userID, deviceID, tokenHash string, expiresAt time.Time) error
	FindByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	RevokeIfActive(ctx context.Context, id string) (bool, error)
	RevokeAllForUser(ctx context.Context, userID string) error
	RevokeAllForDevice(ctx context.Context, deviceID string) error
}
