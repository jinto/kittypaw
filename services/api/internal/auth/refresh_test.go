package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jinto/kittypaw-api/internal/auth"
	"github.com/jinto/kittypaw-api/internal/model"
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

func setupRefreshTest(t *testing.T) (*auth.OAuthHandler, *refreshTestRefreshStore) {
	t.Helper()
	rtStore := newRefreshTestRefreshStore()
	userStore := newMockUserStore()

	// Pre-create a user.
	_, _ = userStore.CreateOrUpdate(context.Background(), "google", "123", "t@t.com", "Test", "")

	h := &auth.OAuthHandler{
		UserStore:         userStore,
		RefreshTokenStore: rtStore,
		StateStore:        auth.NewStateStore(),
		JWTSecret:         testSecret,
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
