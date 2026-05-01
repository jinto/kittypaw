package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/model"
)

type mockUserStore struct {
	users map[string]*model.User
	seq   int
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{users: make(map[string]*model.User)}
}

func (m *mockUserStore) CreateOrUpdate(_ context.Context, provider, providerID, email, name, avatarURL string) (*model.User, error) {
	key := provider + ":" + providerID
	u, ok := m.users[key]
	if ok {
		u.Email = email
		u.Name = name
		u.AvatarURL = avatarURL
		u.UpdatedAt = time.Now()
		return u, nil
	}
	m.seq++
	u = &model.User{
		ID:         "user-" + provider + "-" + providerID,
		Provider:   provider,
		ProviderID: providerID,
		Email:      email,
		Name:       name,
		AvatarURL:  avatarURL,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.users[key] = u
	return u, nil
}

func (m *mockUserStore) FindByID(_ context.Context, id string) (*model.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, model.ErrNotFound
}

type mockRefreshTokenStore struct {
	tokens []model.RefreshToken
}

func (m *mockRefreshTokenStore) Create(_ context.Context, userID, tokenHash string, expiresAt time.Time) error {
	m.tokens = append(m.tokens, model.RefreshToken{
		ID:        "rt-1",
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	})
	return nil
}

func (m *mockRefreshTokenStore) FindByHash(_ context.Context, hash string) (*model.RefreshToken, error) {
	for i := range m.tokens {
		if m.tokens[i].TokenHash == hash {
			return &m.tokens[i], nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *mockRefreshTokenStore) RevokeIfActive(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func (m *mockRefreshTokenStore) RevokeAllForUser(_ context.Context, _ string) error { return nil }

func setupGoogleTest(t *testing.T, googleServer *httptest.Server) (*auth.OAuthHandler, auth.GoogleConfig) {
	t.Helper()

	states := auth.NewStateStore()
	t.Cleanup(states.Close)

	h := &auth.OAuthHandler{
		UserStore:         newMockUserStore(),
		RefreshTokenStore: &mockRefreshTokenStore{},
		StateStore:        states,
		JWTSecret:         testSecret,
		HTTPClient:        googleServer.Client(),
	}

	cfg := auth.GoogleConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/auth/google/callback",
	}

	return h, cfg
}

func TestGoogleLoginRedirect(t *testing.T) {
	// No actual Google server needed for login.
	h, cfg := setupGoogleTest(t, httptest.NewServer(http.NotFoundHandler()))

	req := httptest.NewRequest(http.MethodGet, "/auth/google", nil)
	w := httptest.NewRecorder()

	h.HandleGoogleLogin(cfg).ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header")
	}

	for _, param := range []string{"client_id=test-client-id", "code_challenge=", "code_challenge_method=S256", "state=", "scope=openid"} {
		if !contains(loc, param) {
			t.Fatalf("redirect URL missing %q: %s", param, loc)
		}
	}
}

func TestGoogleCallbackSuccess(t *testing.T) {
	// Mock Google token + userinfo endpoints.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "google-at"})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":      "g-user-1",
			"email":   "test@gmail.com",
			"name":    "Test User",
			"picture": "https://avatar.example.com/1",
		})
	})
	googleServer := httptest.NewServer(mux)
	defer googleServer.Close()

	h, cfg := setupGoogleTest(t, googleServer)

	// Override Google URLs for testing.
	h.GoogleTokenURL = googleServer.URL + "/token"
	h.GoogleUserInfoURL = googleServer.URL + "/userinfo"

	// Create a valid state.
	state, err := h.StateStore.Create("test-verifier")
	if err != nil {
		t.Fatalf("create state: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=test-code&state="+state, nil)
	w := httptest.NewRecorder()

	h.HandleGoogleCallback(cfg).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp auth.TokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("expected non-empty access_token")
	}
	if resp.RefreshToken == "" {
		t.Fatal("expected non-empty refresh_token")
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("expected Bearer, got %q", resp.TokenType)
	}
}

func TestGoogleCallbackInvalidState(t *testing.T) {
	googleServer := httptest.NewServer(http.NotFoundHandler())
	defer googleServer.Close()

	h, cfg := setupGoogleTest(t, googleServer)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback?code=test-code&state=invalid", nil)
	w := httptest.NewRecorder()

	h.HandleGoogleCallback(cfg).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGoogleCallbackMissingParams(t *testing.T) {
	googleServer := httptest.NewServer(http.NotFoundHandler())
	defer googleServer.Close()

	h, cfg := setupGoogleTest(t, googleServer)

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback", nil)
	w := httptest.NewRecorder()

	h.HandleGoogleCallback(cfg).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
