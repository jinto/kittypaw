package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/kittypaw-app/kittyportal/internal/model"
)

// WebLoginConfig wires the per-flow knobs the web OAuth handlers need.
// Distinct from CLILoginConfig because the web flow's callback target is
// chat-supplied (allowlisted) rather than localhost-hardcoded.
type WebLoginConfig struct {
	GoogleCfg GoogleConfig
	CodeStore *WebCodeStore
	// RedirectURIAllowlist — exact-match list of allowed chat callback URLs.
	// Substring/prefix matching is forbidden: an open redirect would let
	// an attacker exfiltrate auth codes by pointing redirect_uri at their
	// domain after passing a sloppy matcher (e.g. "startsWith(allowed)").
	RedirectURIAllowlist []string
}

// stateMetaKeyMode marks a state entry as belonging to the web flow so
// HandleGoogleCallback knows to dispatch to the web branch instead of
// issuing tokens directly.
const (
	stateMetaKeyMode          = "mode"
	stateMetaKeyRedirectURI   = "redirect_uri"
	stateMetaKeyChatState     = "chat_state"
	stateMetaKeyCodeChallenge = "code_challenge"

	stateMetaModeWeb = "web"

	// pkceMethodS256 is the only PKCE method we accept on web flow.
	// The "plain" method is rejected — it offers no MITM protection
	// (challenge == verifier) and OAuth 2.1 deprecates it.
	pkceMethodS256 = "S256"

	// maxChatStateLen caps the chat-supplied CSRF state to defend
	// against a misbehaving / malicious client stuffing the state
	// store with multi-KB strings. Real CSRF states are ≤64 chars;
	// 1024 leaves comfortable headroom for chat to encode small
	// structured payloads if it wants.
	maxChatStateLen = 1024

	// maxCodeChallengeLen caps code_challenge. RFC 7636 S256 produces
	// a 43-char base64url string. 256 is far above any legitimate
	// value while still bounding abuse.
	maxCodeChallengeLen = 256
)

// HandleWebGoogleLogin starts the web OAuth flow used by chat.kittypaw.app.
// The chat server has already generated its own PKCE verifier; it sends
// us the code_challenge (S256 hash) and a CSRF state to echo back. We
// generate our OWN verifier for the Google round-trip — the two PKCE
// chains are independent (chat <-> portal vs portal <-> Google).
//
// GET /auth/web/google
//
//	?redirect_uri=<allowlisted chat callback>
//	&state=<chat-supplied CSRF state>
//	&code_challenge=<S256(chat_verifier)>
//	&code_challenge_method=S256
func (h *OAuthHandler) HandleWebGoogleLogin(cfg WebLoginConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		redirectURI := q.Get("redirect_uri")
		chatState := q.Get("state")
		codeChallenge := q.Get("code_challenge")
		codeChallengeMethod := q.Get("code_challenge_method")

		if redirectURI == "" || chatState == "" || codeChallenge == "" {
			http.Error(w, "missing redirect_uri, state, or code_challenge", http.StatusBadRequest)
			return
		}
		// Length caps before any allocation/lookup. A misbehaving client
		// shouldn't be able to grow the state-store entry size by sending
		// multi-KB query params.
		if len(chatState) > maxChatStateLen || len(codeChallenge) > maxCodeChallengeLen {
			http.Error(w, "state or code_challenge too long", http.StatusBadRequest)
			return
		}
		if codeChallengeMethod != pkceMethodS256 {
			http.Error(w, "code_challenge_method must be S256", http.StatusBadRequest)
			return
		}
		if !redirectURIAllowed(cfg.RedirectURIAllowlist, redirectURI) {
			http.Error(w, "redirect_uri not allowed", http.StatusBadRequest)
			return
		}

		verifier, err := GenerateVerifier()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		meta := map[string]string{
			stateMetaKeyMode:          stateMetaModeWeb,
			stateMetaKeyRedirectURI:   redirectURI,
			stateMetaKeyChatState:     chatState,
			stateMetaKeyCodeChallenge: codeChallenge,
		}
		state, err := h.StateStore.CreateWithMeta(verifier, meta)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		params := url.Values{
			"client_id":             {cfg.GoogleCfg.ClientID},
			"redirect_uri":          {cfg.GoogleCfg.RedirectURL},
			"response_type":         {"code"},
			"scope":                 {"openid email profile"},
			"state":                 {state},
			"code_challenge":        {ChallengeS256(verifier)},
			"code_challenge_method": {pkceMethodS256},
			"access_type":           {"offline"},
		}
		http.Redirect(w, r, h.googleAuthURL()+"?"+params.Encode(), http.StatusFound)
	}
}

// HandleWebExchange completes the web OAuth flow. Called server-to-server
// by the chat backend after it receives the redirect with code+state.
//
// POST /auth/web/exchange
//
//	{ "code", "code_verifier", "redirect_uri" }
//
// PKCE binding: stored code_challenge MUST equal S256(code_verifier).
// redirect_uri rebinding: stored redirect_uri MUST equal the request's —
// guards against an attacker swapping redirect_uri at exchange time.
func (h *OAuthHandler) HandleWebExchange() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Server-to-server only. Browsers always send Origin on cross-
		// origin POST; legitimate chat-server callers do not. This
		// enforces the BFF boundary independently of cors.AllowedOrigins
		// — an operator misconfiguring CORS_ORIGINS to include
		// chat.kittypaw.app must NOT reopen browser-direct exchange
		// (refresh_token would land in browser-side JS, defeating the
		// reason we chose Authorization Code with PKCE over implicit).
		if r.Header.Get("Origin") != "" {
			http.Error(w, "this endpoint is server-to-server only", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		var req struct {
			Code         string `json:"code"`
			CodeVerifier string `json:"code_verifier"`
			RedirectURI  string `json:"redirect_uri"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Code == "" || req.CodeVerifier == "" || req.RedirectURI == "" {
			http.Error(w, "code, code_verifier, redirect_uri required", http.StatusBadRequest)
			return
		}

		entry, err := h.WebCodeStore.Consume(req.Code)
		if err != nil {
			// Unknown / expired / replay — collapse to silent 401.
			http.Error(w, "invalid or expired code", http.StatusUnauthorized)
			return
		}

		if entry.RedirectURI != req.RedirectURI {
			http.Error(w, "redirect_uri mismatch", http.StatusBadRequest)
			return
		}
		if ChallengeS256(req.CodeVerifier) != entry.CodeChallenge {
			http.Error(w, "code_verifier invalid", http.StatusUnauthorized)
			return
		}

		user, err := h.UserStore.FindByID(r.Context(), entry.UserID)
		if err != nil {
			slog.Error("web exchange: FindByID failed", "user_id", entry.UserID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		tokens, err := h.issueTokenPair(r.Context(), user)
		if err != nil {
			slog.Error("web exchange: issueTokenPair failed", "user_id", user.ID)
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}

		// RFC 6749 §5.1 — token responses MUST NOT be cached.
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		_ = json.NewEncoder(w).Encode(tokens)
	}
}

// redirectURIAllowed performs exact-match lookup. Substring/prefix
// matching would be an open-redirect footgun — see the WebLoginConfig
// comment.
func redirectURIAllowed(allowlist []string, candidate string) bool {
	for _, allowed := range allowlist {
		if allowed == candidate {
			return true
		}
	}
	return false
}

// emitWebCallback completes the callback dispatch for the web flow.
// Called from HandleGoogleCallback when state metadata indicates web mode.
// Issues a one-time code bound to (userID, redirect_uri, code_challenge)
// and 302-redirects to the chat callback with code+state.
//
// Lives here (not in google.go) so the web-flow concerns stay together;
// google.go only needs to know "if web mode, hand off to this function."
func (h *OAuthHandler) emitWebCallback(w http.ResponseWriter, r *http.Request, user *model.User, meta map[string]string) {
	redirectURI := meta[stateMetaKeyRedirectURI]
	chatState := meta[stateMetaKeyChatState]
	codeChallenge := meta[stateMetaKeyCodeChallenge]
	if redirectURI == "" || chatState == "" || codeChallenge == "" {
		// Should be impossible — HandleWebGoogleLogin enforces all three.
		// Surfacing as 500 instead of silent redirect avoids leaking the
		// user to an unspecified destination.
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	code, err := h.WebCodeStore.Create(WebCodeEntry{
		UserID:        user.ID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
	})
	if err != nil {
		slog.Error("web callback: WebCodeStore.Create failed", "user_id", user.ID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Build redirect with code+state. url.Values escaping handles any
	// % / # / & in the chat_state (chat may have encoded structured data
	// in there). state is echoed verbatim — chat is responsible for
	// validating its own CSRF binding.
	target, err := url.Parse(redirectURI)
	if err != nil {
		// Allowlist already accepted it, so a parse failure here means
		// our allowlist accepted a malformed URL — operational bug.
		slog.Error("web callback: redirect_uri parse failed", "uri", redirectURI)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	q := target.Query()
	q.Set("code", code)
	q.Set("state", chatState)
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}
