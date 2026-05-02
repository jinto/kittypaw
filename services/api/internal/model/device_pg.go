package model

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresDeviceStore struct {
	pool *pgxpool.Pool
}

func NewDeviceStore(pool *pgxpool.Pool) *PostgresDeviceStore {
	return &PostgresDeviceStore{pool: pool}
}

// scanDevice consumes the standard SELECT column list and decodes the
// jsonb capabilities. pgx/v5 has no pgtype.JSONB — jsonb columns
// round-trip as []byte, then we json.Unmarshal into map[string]any.
func scanDevice(row pgx.Row) (*Device, error) {
	var d Device
	var capsBytes []byte
	if err := row.Scan(
		&d.ID, &d.UserID, &d.Name, &capsBytes,
		&d.PairedAt, &d.LastUsedAt, &d.LastConnectedAt, &d.RevokedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(capsBytes, &d.Capabilities); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PostgresDeviceStore) Create(ctx context.Context, userID, name string, capabilities map[string]any) (*Device, error) {
	if capabilities == nil {
		capabilities = map[string]any{}
	}
	capsJSON, err := json.Marshal(capabilities)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO devices (user_id, name, capabilities)
		VALUES ($1, $2, $3::jsonb)
		RETURNING id, user_id, name, capabilities, paired_at, last_used_at, last_connected_at, revoked_at
	`, userID, name, capsJSON)
	return scanDevice(row)
}

func (s *PostgresDeviceStore) FindByID(ctx context.Context, id string) (*Device, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, capabilities, paired_at, last_used_at, last_connected_at, revoked_at
		FROM devices WHERE id = $1
	`, id)
	d, err := scanDevice(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d, nil
}

func (s *PostgresDeviceStore) ListActiveForUser(ctx context.Context, userID string) ([]*Device, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, capabilities, paired_at, last_used_at, last_connected_at, revoked_at
		FROM devices
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY paired_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Revoke soft-deletes by setting revoked_at = now(). Idempotent: a
// second call on an already-revoked row returns nil (revoked_at
// preserves the first timestamp). Distinguishes missing (ErrNotFound)
// from already-revoked via an explicit EXISTS check.
func (s *PostgresDeviceStore) Revoke(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE devices SET revoked_at = now()
		WHERE id = $1 AND revoked_at IS NULL
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	// Either missing or already revoked — disambiguate.
	var exists bool
	err = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1)`, id).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil // already revoked, idempotent success
}
