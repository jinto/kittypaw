//go:build integration

package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kittypaw-app/kittyportal/internal/auth"
	"github.com/kittypaw-app/kittyportal/internal/auth/testfixture"
	"github.com/kittypaw-app/kittyportal/internal/config"
	"github.com/kittypaw-app/kittyportal/internal/model"
)

// Plan 13 — refresh rotation handler-layer integration tests.
// .claude/plans/plan-13-auth-me-refresh-contract-revision.md (T3)
//
// Differs from setupAuthIntegration in two ways:
//   1. needs both UserStore + RefreshTokenStore (the store seam exercised
//      by HandleTokenRefresh).
//   2. server wraps OAuthHandler.HandleTokenRefresh, not HandleMe.

type refreshSetup struct {
	pool         *pgxpool.Pool
	users        model.UserStore
	refreshStore model.RefreshTokenStore
	server       *httptest.Server
}

func setupRefreshIntegration(t *testing.T) *refreshSetup {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatalf("DATABASE_URL must point at a test DB (must contain \"_test\"); got %q", dsn)
	}

	cfg := config.LoadForTest()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}

	users := model.NewUserStore(pool)
	refreshStore := model.NewRefreshTokenStore(pool)

	h := &auth.OAuthHandler{
		UserStore:         users,
		RefreshTokenStore: refreshStore,
		StateStore:        auth.NewStateStore(),
		JWTPrivateKey:     cfg.JWTPrivateKey,
		JWTKID:            cfg.JWTKID,
		HTTPClient:        &http.Client{Timeout: 5 * time.Second},
	}
	server := httptest.NewServer(h.HandleTokenRefresh())

	t.Cleanup(func() {
		bg := context.Background()
		// Refresh tokens cascade-delete with their user (FK ON DELETE CASCADE
		// in the migrations); cleaning up users is sufficient. Belt-and-
		// suspenders: also DELETE refresh_tokens by user prefix.
		_, _ = pool.Exec(bg, `
			DELETE FROM refresh_tokens
			WHERE user_id IN (SELECT id FROM users WHERE provider_id LIKE 'test-%')
		`)
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE provider_id LIKE 'test-%'")
		server.Close()
		pool.Close()
	})

	return &refreshSetup{pool: pool, users: users, refreshStore: refreshStore, server: server}
}

func seedRefreshToken(t *testing.T, store model.RefreshTokenStore, userID string) (raw string) {
	t.Helper()
	raw, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	hash := auth.HashRefreshToken(raw)
	if err := store.Create(context.Background(), userID, hash, time.Now().Add(7*24*time.Hour)); err != nil {
		t.Fatalf("RefreshTokenStore.Create: %v", err)
	}
	return raw
}

func postRefresh(t *testing.T, server *httptest.Server, raw string) (*http.Response, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"refresh_token": raw})
	resp, err := http.Post(server.URL+"/", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, respBody
}

// activeRefreshCount counts non-revoked refresh tokens for a user.
// Used for reuse-detect verification — RevokeAllForUser must zero this out.
func activeRefreshCount(t *testing.T, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID).Scan(&n)
	if err != nil {
		t.Fatalf("count active refresh: %v", err)
	}
	return n
}

func TestRefresh_Integration_HappyRotation(t *testing.T) {
	s := setupRefreshIntegration(t)
	user := testfixture.SeedTestUser(t, s.users)
	rawOld := seedRefreshToken(t, s.refreshStore, user.ID)

	resp, body := postRefresh(t, s.server, rawOld)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
	}

	var tokens auth.TokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Fatal("expected non-empty access_token")
	}
	if tokens.RefreshToken == "" {
		t.Fatal("expected non-empty refresh_token")
	}
	if tokens.RefreshToken == rawOld {
		t.Fatal("rotation did not produce a new refresh token")
	}

	// Old refresh must be marked revoked in DB.
	var revokedCount int
	err := s.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM refresh_tokens
		WHERE user_id = $1 AND token_hash = $2 AND revoked_at IS NOT NULL
	`, user.ID, auth.HashRefreshToken(rawOld)).Scan(&revokedCount)
	if err != nil {
		t.Fatalf("query revoked: %v", err)
	}
	if revokedCount != 1 {
		t.Fatalf("old refresh revoked rows = %d, want 1", revokedCount)
	}
}

func TestRefresh_Integration_ReuseDetect(t *testing.T) {
	s := setupRefreshIntegration(t)
	user := testfixture.SeedTestUser(t, s.users)
	rawOld := seedRefreshToken(t, s.refreshStore, user.ID)

	// First rotation — succeeds and issues a new pair.
	resp1, body1 := postRefresh(t, s.server, rawOld)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first rotation status = %d, want 200; body=%s", resp1.StatusCode, string(body1))
	}

	// Second use of the SAME old refresh — reuse detection must fire,
	// returning 401 AND revoking ALL active refresh tokens for the user.
	resp2, body2 := postRefresh(t, s.server, rawOld)
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want 401; body=%s", resp2.StatusCode, string(body2))
	}

	// Critic ITERATE C2: RevokeAllForUser must zero out active tokens —
	// asserting only the 401 would miss a regression where the handler
	// returns 401 but forgets to revoke siblings.
	if n := activeRefreshCount(t, s.pool, user.ID); n != 0 {
		t.Fatalf("active refresh tokens after reuse-detect = %d, want 0 (RevokeAllForUser must wipe)", n)
	}
}
