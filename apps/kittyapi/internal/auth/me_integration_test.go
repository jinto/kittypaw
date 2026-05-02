//go:build integration

package auth_test

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/auth/testfixture"
	"github.com/kittypaw-app/kittyapi/internal/config"
	"github.com/kittypaw-app/kittyapi/internal/model"
)

// Plan 13 — auth /me handler-layer integration tests.
// .claude/plans/plan-13-auth-me-refresh-contract-revision.md (T2)
//
// Pattern: Plan 12 setupGeoIntegration. Differences:
//   - no advisory_lock (auth has no cross-package TRUNCATE race target)
//   - middleware-wrapped httptest server (HandleMe needs *User in context,
//     populated by auth.Middleware from the Bearer token)
//   - testfixture (Plan 11) seeds the user; users table is the only DB
//     dependency (no places / alias_overrides involvement).

type authSetup struct {
	pool   *pgxpool.Pool
	store  model.UserStore
	server *httptest.Server
	jwtKey *rsa.PrivateKey
	jwtKID string
}

func setupAuthIntegration(t *testing.T) *authSetup {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	if !strings.Contains(dsn, "_test") {
		t.Fatalf("DATABASE_URL must point at a test DB (must contain \"_test\"); got %q", dsn)
	}

	cfg := config.LoadForTest()
	provider := auth.NewSingleKeyProvider(&cfg.JWTPrivateKey.PublicKey, cfg.JWTKID)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}

	store := model.NewUserStore(pool)
	mw := auth.Middleware(provider, auth.AudienceAPI, store)
	server := httptest.NewServer(mw(http.HandlerFunc(auth.HandleMe)))

	t.Cleanup(func() {
		bg := context.Background()
		// testfixture.SeedTestUser inserts users with provider_id "test-<UnixNano>".
		// Best-effort cleanup to keep the test DB tidy across runs.
		_, _ = pool.Exec(bg, "DELETE FROM users WHERE provider_id LIKE 'test-%'")
		server.Close()
		pool.Close()
	})

	return &authSetup{pool: pool, store: store, server: server, jwtKey: cfg.JWTPrivateKey, jwtKID: cfg.JWTKID}
}

func meRequest(t *testing.T, server *httptest.Server, bearer string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

func TestMe_Integration_NoToken(t *testing.T) {
	s := setupAuthIntegration(t)
	resp, _ := meRequest(t, s.server, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMe_Integration_ValidJWT(t *testing.T) {
	s := setupAuthIntegration(t)

	user := testfixture.SeedTestUser(t, s.store)
	token := testfixture.IssueTestJWT(t, s.jwtKey, s.jwtKID, user.ID, 15*time.Minute)

	resp, body := meRequest(t, s.server, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
	}

	var got model.User
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, string(body))
	}
	if got.ID != user.ID {
		t.Fatalf("id = %q, want %q", got.ID, user.ID)
	}
	if got.Email != user.Email {
		t.Fatalf("email = %q, want %q", got.Email, user.Email)
	}
	if got.Provider != user.Provider {
		t.Fatalf("provider = %q, want %q", got.Provider, user.Provider)
	}
}

func TestMe_Integration_ExpiredJWT(t *testing.T) {
	s := setupAuthIntegration(t)

	user := testfixture.SeedTestUser(t, s.store)
	// TTL well past leeway — the token is too old, so Verify rejects with
	// "token is expired" and the middleware returns 401. Must exceed the
	// 60s leeway window or this case silently passes verification.
	token := testfixture.IssueTestJWT(t, s.jwtKey, s.jwtKID, user.ID, -2*time.Minute)

	resp, _ := meRequest(t, s.server, token)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (expired)", resp.StatusCode)
	}
}
