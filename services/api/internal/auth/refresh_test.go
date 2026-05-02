package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/config"
	"github.com/kittypaw-app/kittyapi/internal/model"
)

type refreshTestRefreshStore struct {
	tokens     map[string]*model.RefreshToken
	revokedAll bool
}

func newRefreshTestRefreshStore() *refreshTestRefreshStore {
	return &refreshTestRefreshStore{tokens: make(map[string]*model.RefreshToken)}
}

func (s *refreshTestRefreshStore) Create(_ context.Context, userID, tokenHash string, expiresAt time.Time) error {
	s.tokens[tokenHash] = &model.RefreshToken{
		ID:        "rt-" + tokenHash[:8],
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	return nil
}

func (s *refreshTestRefreshStore) FindByHash(_ context.Context, hash string) (*model.RefreshToken, error) {
	rt, ok := s.tokens[hash]
	if !ok {
		return nil, model.ErrNotFound
	}
	return rt, nil
}

func (s *refreshTestRefreshStore) RevokeIfActive(_ context.Context, id string) (bool, error) {
	for _, rt := range s.tokens {
		if rt.ID == id {
			if rt.RevokedAt != nil {
				return false, nil
			}
			now := time.Now()
			rt.RevokedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (s *refreshTestRefreshStore) RevokeAllForUser(_ context.Context, _ string) error {
	s.revokedAll = true
	return nil
}

func (s *refreshTestRefreshStore) CreateForDevice(_ context.Context, userID, deviceID, tokenHash string, expiresAt time.Time) error {
	dev := deviceID
	s.tokens[tokenHash] = &model.RefreshToken{
		ID:        "rt-dev-" + tokenHash[:min(8, len(tokenHash))],
		UserID:    userID,
		DeviceID:  &dev,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	return nil
}

func (s *refreshTestRefreshStore) RevokeAllForDevice(_ context.Context, _ string) error {
	return nil
}

// RotateForDevice — not exercised by user-refresh tests, but the
// interface requires it. No-op implementation.
func (s *refreshTestRefreshStore) RotateForDevice(_ context.Context, _, _, _, _ string, _ time.Time) error {
	return nil
}

func setupRefreshTest(t *testing.T) (*auth.OAuthHandler, *refreshTestRefreshStore) {
	t.Helper()
	cfg := config.LoadForTest()
	rtStore := newRefreshTestRefreshStore()
	userStore := newMockUserStore()

	// Pre-create a user.
	_, _ = userStore.CreateOrUpdate(context.Background(), "google", "123", "t@t.com", "Test", "")

	h := &auth.OAuthHandler{
		UserStore:         userStore,
		RefreshTokenStore: rtStore,
		StateStore:        auth.NewStateStore(),
		JWTPrivateKey:     cfg.JWTPrivateKey,
		JWTKID:            cfg.JWTKID,
	}
	t.Cleanup(h.StateStore.Close)
	return h, rtStore
}

func TestTokenRefreshValid(t *testing.T) {
	h, rtStore := setupRefreshTest(t)

	raw := "test-refresh-token-raw"
	hash := auth.HashRefreshToken(raw)
	_ = rtStore.Create(context.Background(), "user-google-123", hash, time.Now().Add(7*24*time.Hour))

	body, _ := json.Marshal(map[string]string{"refresh_token": raw})
	req := httptest.NewRequest(http.MethodPost, "/auth/token/refresh", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleTokenRefresh().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp auth.TokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("expected non-empty tokens")
	}
}

func TestTokenRefreshExpired(t *testing.T) {
	h, rtStore := setupRefreshTest(t)

	raw := "expired-refresh"
	hash := auth.HashRefreshToken(raw)
	_ = rtStore.Create(context.Background(), "user-google-123", hash, time.Now().Add(-time.Hour))

	body, _ := json.Marshal(map[string]string{"refresh_token": raw})
	req := httptest.NewRequest(http.MethodPost, "/auth/token/refresh", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleTokenRefresh().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestTokenRefreshReuseDetection(t *testing.T) {
	h, rtStore := setupRefreshTest(t)

	raw := "reused-refresh"
	hash := auth.HashRefreshToken(raw)
	_ = rtStore.Create(context.Background(), "user-google-123", hash, time.Now().Add(7*24*time.Hour))

	// Simulate already-revoked token.
	now := time.Now()
	rtStore.tokens[hash].RevokedAt = &now

	body, _ := json.Marshal(map[string]string{"refresh_token": raw})
	req := httptest.NewRequest(http.MethodPost, "/auth/token/refresh", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleTokenRefresh().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !rtStore.revokedAll {
		t.Fatal("expected RevokeAllForUser to be called")
	}
}

// TestTokenRefreshBodyTooLarge pins the body-size cap on
// /auth/token/refresh. Without a MaxBytesReader, json.Decode happily
// parses an arbitrarily large refresh_token string before the handler
// even reaches the lookup — letting an unauthenticated caller burn
// memory by streaming a multi-MB body. Cap is matched to the existing
// /auth/cli/exchange handler (1 KiB) since both endpoints take a single
// short opaque token in JSON.
//
// Verification mechanic: a 10 KiB body must be rejected at the body
// reader (400 from the MaxBytesReader-wrapped Decode error), NOT at the
// FindByHash step (which would 401 with "invalid refresh token"). The
// distinction is what tells us the cap is the actual gate.
func TestTokenRefreshBodyTooLarge(t *testing.T) {
	h, _ := setupRefreshTest(t)

	// Build a syntactically valid JSON whose refresh_token value is 10 KiB —
	// would parse fine without the cap, then 401 on FindByHash lookup.
	huge := make([]byte, 10*1024)
	for i := range huge {
		huge[i] = 'a'
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": string(huge)})
	req := httptest.NewRequest(http.MethodPost, "/auth/token/refresh", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleTokenRefresh().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (oversize body must trip MaxBytesReader before lookup), got %d", w.Code)
	}
}

func TestTokenRefreshUnknown(t *testing.T) {
	h, _ := setupRefreshTest(t)

	body, _ := json.Marshal(map[string]string{"refresh_token": "nonexistent"})
	req := httptest.NewRequest(http.MethodPost, "/auth/token/refresh", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.HandleTokenRefresh().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
