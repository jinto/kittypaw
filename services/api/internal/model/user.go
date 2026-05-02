package model

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrRotationAborted signals that RotateForDevice's old-row revoke
	// matched 0 rows — the row was either never present or already
	// revoked. Caller's reuse-detection branch should have handled the
	// already-revoked case before reaching here; this sentinel exists so
	// the handler can map a race-loser cleanly to a silent 401 rather
	// than a generic 500.
	ErrRotationAborted = errors.New("rotation aborted: old refresh not active")
)

type User struct {
	ID         string    `json:"id"`
	Provider   string    `json:"provider"`
	ProviderID string    `json:"-"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	AvatarURL  string    `json:"avatar_url"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type UserStore interface {
	CreateOrUpdate(ctx context.Context, provider, providerID, email, name, avatarURL string) (*User, error)
	FindByID(ctx context.Context, id string) (*User, error)
}

type PostgresUserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *PostgresUserStore {
	return &PostgresUserStore{pool: pool}
}

func (s *PostgresUserStore) CreateOrUpdate(ctx context.Context, provider, providerID, email, name, avatarURL string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (provider, provider_id, email, name, avatar_url)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider, provider_id)
		DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name, avatar_url = EXCLUDED.avatar_url, updated_at = now()
		RETURNING id, provider, provider_id, email, name, avatar_url, created_at, updated_at
	`, provider, providerID, email, name, avatarURL).Scan(
		&u.ID, &u.Provider, &u.ProviderID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PostgresUserStore) FindByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, provider, provider_id, email, name, avatar_url, created_at, updated_at
		FROM users WHERE id = $1
	`, id).Scan(
		&u.ID, &u.Provider, &u.ProviderID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}
