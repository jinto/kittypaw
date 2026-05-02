package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/config"
)

func setupGitHubTest(t *testing.T, ghServer *httptest.Server) (*auth.OAuthHandler, auth.GitHubConfig) {
	t.Helper()

	appCfg := config.LoadForTest()
	states := auth.NewStateStore()
	t.Cleanup(states.Close)

	h := &auth.OAuthHandler{
		UserStore:         newMockUserStore(),
		RefreshTokenStore: &mockRefreshTokenStore{},
		StateStore:        states,
		JWTPrivateKey:     appCfg.JWTPrivateKey,
		JWTKID:            appCfg.JWTKID,
		HTTPClient:        ghServer.Client(),
		GitHubTokenURL:    ghServer.URL + "/login/oauth/access_token",
		GitHubUserURL:     ghServer.URL + "/user",
	}

	cfg := auth.GitHubConfig{
		ClientID:     "gh-client-id",
		ClientSecret: "gh-client-secret",
		RedirectURL:  "http://localhost:8080/auth/github/callback",
	}

	return h, cfg
}

func TestGitHubLoginRedirect(t *testing.T) {
	ghServer := httptest.NewServer(http.NotFoundHandler())
	defer ghServer.Close()

	h, cfg := setupGitHubTest(t, ghServer)

	req := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	w := httptest.NewRecorder()

	h.HandleGitHubLogin(cfg).ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	loc := w.Header().Get("Location")
	for _, param := range []string{"client_id=gh-client-id", "state=", "scope=read%3Auser", "code_challenge=", "code_challenge_method=S256"} {
		if !contains(loc, param) {
			t.Fatalf("redirect URL missing %q: %s", param, loc)
		}
	}
}

func TestGitHubCallbackSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gh-at"})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         42,
			"login":      "testuser",
			"email":      "test@github.com",
			"name":       "Test GitHub",
			"avatar_url": "https://avatars.example.com/42",
		})
	})
	ghServer := httptest.NewServer(mux)
	defer ghServer.Close()

	h, cfg := setupGitHubTest(t, ghServer)

	state, err := h.StateStore.Create("test-verifier")
	if err != nil {
		t.Fatalf("create state: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=test-code&state="+state, nil)
	w := httptest.NewRecorder()

	h.HandleGitHubCallback(cfg).ServeHTTP(w, req)

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

func TestGitHubCallbackInvalidState(t *testing.T) {
	ghServer := httptest.NewServer(http.NotFoundHandler())
	defer ghServer.Close()

	h, cfg := setupGitHubTest(t, ghServer)

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=test-code&state=invalid", nil)
	w := httptest.NewRecorder()

	h.HandleGitHubCallback(cfg).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
