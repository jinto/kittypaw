package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kittypaw-app/kittykakao/internal/relay"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	db.SetMaxOpenConns(1)

	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("sqlite pragma %q: %w", stmt, err)
		}
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS tokens (
	token TEXT PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS user_mappings (
	kakao_id TEXT PRIMARY KEY,
	token TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS killswitch (
	id INTEGER PRIMARY KEY CHECK(id = 1),
	enabled INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS pending_callbacks (
	action_id TEXT PRIMARY KEY,
	callback_url TEXT NOT NULL,
	user_id TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS rate_counters (
	key TEXT PRIMARY KEY,
	count INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO killswitch (id, enabled) VALUES (1, 0);
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite init: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) TokenExists(ctx context.Context, token string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tokens WHERE token = ?)`, token).Scan(&exists)
	return exists, err
}

func (s *Store) PutToken(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO tokens (token) VALUES (?)`, token)
	return err
}

func (s *Store) GetUserMapping(ctx context.Context, kakaoID string) (string, bool, error) {
	var token string
	err := s.db.QueryRowContext(ctx, `SELECT token FROM user_mappings WHERE kakao_id = ?`, kakaoID).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

func (s *Store) PutUserMapping(ctx context.Context, kakaoID, token string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO user_mappings (kakao_id, token) VALUES (?, ?)`, kakaoID, token)
	return err
}

func (s *Store) DeleteUserMapping(ctx context.Context, kakaoID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_mappings WHERE kakao_id = ?`, kakaoID)
	return err
}

func (s *Store) GetKillswitch(ctx context.Context) (bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT enabled FROM killswitch WHERE id = 1`).Scan(&enabled)
	return enabled != 0, err
}

func (s *Store) SetKillswitch(ctx context.Context, enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE killswitch SET enabled = ? WHERE id = 1`, value)
	return err
}

func (s *Store) PutPending(ctx context.Context, actionID string, pending relay.PendingContext) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO pending_callbacks (action_id, callback_url, user_id, created_at) VALUES (?, ?, ?, ?)`,
		actionID,
		pending.CallbackURL,
		pending.UserID,
		pending.CreatedAt,
	)
	return err
}

func (s *Store) TakePending(ctx context.Context, actionID string) (relay.PendingContext, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return relay.PendingContext{}, false, err
	}
	defer tx.Rollback()

	var pending relay.PendingContext
	err = tx.QueryRowContext(
		ctx,
		`SELECT callback_url, user_id, created_at FROM pending_callbacks WHERE action_id = ?`,
		actionID,
	).Scan(&pending.CallbackURL, &pending.UserID, &pending.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return relay.PendingContext{}, false, err
		}
		return relay.PendingContext{}, false, nil
	}
	if err != nil {
		return relay.PendingContext{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pending_callbacks WHERE action_id = ?`, actionID); err != nil {
		return relay.PendingContext{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return relay.PendingContext{}, false, err
	}
	return pending, true, nil
}

func (s *Store) CheckRateLimit(ctx context.Context, dailyLimit, monthlyLimit uint64) (relay.RateLimitResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return relay.RateLimitResult{}, err
	}
	defer tx.Rollback()

	dailyKey := "d:" + time.Now().UTC().Format("2006-01-02")
	monthlyKey := "m:" + time.Now().UTC().Format("2006-01")
	for _, key := range []string{dailyKey, monthlyKey} {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO rate_counters (key, count) VALUES (?, 0)`, key); err != nil {
			return relay.RateLimitResult{}, err
		}
	}

	daily, err := counter(ctx, tx, dailyKey)
	if err != nil {
		return relay.RateLimitResult{}, err
	}
	monthly, err := counter(ctx, tx, monthlyKey)
	if err != nil {
		return relay.RateLimitResult{}, err
	}
	if daily >= dailyLimit || monthly >= monthlyLimit {
		if err := tx.Commit(); err != nil {
			return relay.RateLimitResult{}, err
		}
		return relay.RateLimitResult{OK: false, Daily: daily, Monthly: monthly}, nil
	}

	if _, err := tx.ExecContext(ctx, `UPDATE rate_counters SET count = count + 1 WHERE key = ?`, dailyKey); err != nil {
		return relay.RateLimitResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rate_counters SET count = count + 1 WHERE key = ?`, monthlyKey); err != nil {
		return relay.RateLimitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return relay.RateLimitResult{}, err
	}
	return relay.RateLimitResult{OK: true, Daily: daily + 1, Monthly: monthly + 1}, nil
}

func (s *Store) GetStats(ctx context.Context) (relay.Stats, error) {
	dailyKey := "d:" + time.Now().UTC().Format("2006-01-02")
	monthlyKey := "m:" + time.Now().UTC().Format("2006-01")

	daily, err := counterOrZero(ctx, s.db, dailyKey)
	if err != nil {
		return relay.Stats{}, err
	}
	monthly, err := counterOrZero(ctx, s.db, monthlyKey)
	if err != nil {
		return relay.Stats{}, err
	}
	return relay.Stats{Daily: daily, Monthly: monthly}, nil
}

func (s *Store) CleanupExpiredPending(ctx context.Context, maxAgeSeconds int64) (uint64, error) {
	cutoff := time.Now().Unix() - maxAgeSeconds
	result, err := s.db.ExecContext(ctx, `DELETE FROM pending_callbacks WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return uint64(rows), nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func counter(ctx context.Context, q queryer, key string) (uint64, error) {
	var count uint64
	err := q.QueryRowContext(ctx, `SELECT count FROM rate_counters WHERE key = ?`, key).Scan(&count)
	return count, err
}

func counterOrZero(ctx context.Context, q queryer, key string) (uint64, error) {
	count, err := counter(ctx, q, key)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}
