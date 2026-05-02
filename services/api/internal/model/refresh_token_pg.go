package model

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRefreshTokenStore struct {
	pool *pgxpool.Pool
}

func NewRefreshTokenStore(pool *pgxpool.Pool) *PostgresRefreshTokenStore {
	return &PostgresRefreshTokenStore{pool: pool}
}

// Create writes a user-scoped refresh token (device_id NULL). The
// only caller is the OAuth callback path (cli.go's issueTokenPair),
// which inserts/updates the user immediately before calling this —
// stale userID is not a real failure mode here. We deliberately do
// NOT map 23503 → ErrNotFound: that mapping exists only for
// CreateForDevice where PR-D handlers need a clean 404 response.
// Symmetry between Create and CreateForDevice would be cosmetic and
// hide a genuine race (a 23503 from this call means the user table
// was concurrently mutated under us — surface it as the real error).
func (s *PostgresRefreshTokenStore) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, tokenHash, expiresAt)
	return err
}

// CreateForDevice inserts a device-scoped refresh token. A stale
// deviceID (no row in devices table) hits the FK constraint — pgx
// surfaces this as SQLSTATE 23503 which we map to ErrNotFound so PR-D
// handlers respond 404 instead of 500. We pin to the device_id FK by
// constraint name; a 23503 from the user_id FK (rare race: user just
// hard-deleted) bubbles up untranslated so PR-D doesn't 404 with
// "device not found" when the actual cause is a missing user.
func (s *PostgresRefreshTokenStore) CreateForDevice(ctx context.Context, userID, deviceID, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, device_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, userID, deviceID, tokenHash, expiresAt)
	if err != nil {
		if isFKViolation(err, "refresh_tokens_device_id_fkey") {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// isFKViolation reports whether err is a pgx 23503 (FK violation) on
// the named constraint. Lets stores distinguish "stale device" from
// "stale user" without conflating them under a single ErrNotFound.
func isFKViolation(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503" && pgErr.ConstraintName == constraintName
}

func (s *PostgresRefreshTokenStore) FindByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var rt RefreshToken
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, device_id, token_hash, expires_at, created_at, revoked_at
		FROM refresh_tokens WHERE token_hash = $1
	`, tokenHash).Scan(&rt.ID, &rt.UserID, &rt.DeviceID, &rt.TokenHash, &rt.ExpiresAt, &rt.CreatedAt, &rt.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &rt, nil
}

func (s *PostgresRefreshTokenStore) RevokeIfActive(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE id = $1 AND revoked_at IS NULL
	`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *PostgresRefreshTokenStore) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	return err
}

func (s *PostgresRefreshTokenStore) RevokeAllForDevice(ctx context.Context, deviceID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE device_id = $1 AND revoked_at IS NULL
	`, deviceID)
	return err
}

// RotateForDevice atomically revokes the old refresh row and inserts
// a new device-scoped one in a single transaction. If either step
// fails the whole rotation rolls back — the daemon's retry then sees
// the original active row and succeeds, instead of self-locking via
// reuse detection.
//
// Failure modes:
//   - oldID not found OR already revoked → returns "rotation aborted"
//     error (caller's reuse-detection branch should have caught those
//     cases first; this is a belt-and-suspenders consistency guard).
//   - new INSERT FK violation on device_id → ErrNotFound (PR-D contract).
//   - other pgx errors propagate as-is.
func (s *PostgresRefreshTokenStore) RotateForDevice(ctx context.Context, oldID, userID, deviceID, newHash string, newExpiresAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE id = $1 AND revoked_at IS NULL
	`, oldID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrRotationAborted
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, device_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, userID, deviceID, newHash, newExpiresAt); err != nil {
		if isFKViolation(err, "refresh_tokens_device_id_fkey") {
			return ErrNotFound
		}
		return err
	}

	return tx.Commit(ctx)
}
