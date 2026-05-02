package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/kittypaw-app/kittyportal/internal/model"
)

// TestEmitWebCallback_RedirectShape pins the chat-side contract: the
// callback redirect MUST land at exactly redirect_uri (no path drift),
// MUST carry a non-empty code, and MUST echo the original chat_state
// verbatim. Any drift here breaks chat's CSRF check or sends the user
// to a wrong destination.
//
// Lives as an internal test (package auth) because emitWebCallback is
// unexported and exporting it just for tests would widen the API
// surface unnecessarily.
func TestEmitWebCallback_RedirectShape(t *testing.T) {
	h := &OAuthHandler{
		WebCodeStore: NewWebCodeStore(),
	}
	t.Cleanup(h.WebCodeStore.Close)

	user := &model.User{ID: "user-1"}
	meta := map[string]string{
		stateMetaKeyRedirectURI:   "https://chat.kittypaw.app/auth/callback",
		stateMetaKeyChatState:     "chat-csrf-xyz",
		stateMetaKeyCodeChallenge: "challenge-abc",
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/google/callback", nil)
	w := httptest.NewRecorder()

	h.emitWebCallback(w, req, user, meta)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}

	gotBase := parsed.Scheme + "://" + parsed.Host + parsed.Path
	if gotBase != "https://chat.kittypaw.app/auth/callback" {
		t.Errorf("redirect base = %q, want https://chat.kittypaw.app/auth/callback", gotBase)
	}
	if got := parsed.Query().Get("state"); got != "chat-csrf-xyz" {
		t.Errorf("state = %q, want chat-csrf-xyz (verbatim echo)", got)
	}
	if parsed.Query().Get("code") == "" {
		t.Error("redirect missing code parameter")
	}
}

// TestEmitWebCallback_MissingMeta: a meta dict without one of the three
// required keys MUST surface as 500 rather than redirect the user to an
// unspecified destination. HandleWebGoogleLogin enforces all three at
// entry, so reaching this path means a state-store corruption or test
// regression — fail loudly.
func TestEmitWebCallback_MissingMeta(t *testing.T) {
	h := &OAuthHandler{
		WebCodeStore: NewWebCodeStore(),
	}
	t.Cleanup(h.WebCodeStore.Close)

	user := &model.User{ID: "user-1"}
	cases := []map[string]string{
		{stateMetaKeyChatState: "s", stateMetaKeyCodeChallenge: "c"},   // missing redirect_uri
		{stateMetaKeyRedirectURI: "u", stateMetaKeyCodeChallenge: "c"}, // missing chat_state
		{stateMetaKeyRedirectURI: "u", stateMetaKeyChatState: "s"},     // missing code_challenge
	}
	for i, meta := range cases {
		req := httptest.NewRequest(http.MethodGet, "/auth/google/callback", nil)
		w := httptest.NewRecorder()
		h.emitWebCallback(w, req, user, meta)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("case %d: expected 500, got %d", i, w.Code)
		}
	}
}
