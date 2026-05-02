package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kittypaw-app/kittyportal/internal/model"
)

// CLILoginConfig holds the per-provider config and shared code store for CLI OAuth.
type CLILoginConfig struct {
	GoogleCfg GoogleConfig
	CodeStore *CLICodeStore
	BaseURL   string // e.g. "http://localhost:8080"
}

// issueTokenPair creates access + refresh tokens without writing to an http.ResponseWriter.
func (h *OAuthHandler) issueTokenPair(ctx context.Context, user *model.User) (*TokenResponse, error) {
	accessToken, err := SignForAudiences(user.ID, DefaultAPIClientAudiences, DefaultAPIClientScopes, h.JWTPrivateKey, h.JWTKID, AccessTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("jwt sign: %w", err)
	}

	rawRefresh, err := GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	hash := HashRefreshToken(rawRefresh)
	if err := h.RefreshTokenStore.Create(ctx, user.ID, hash, time.Now().Add(RefreshTokenTTL)); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	return &TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
	}, nil
}

// HandleCLILogin initiates OAuth for CLI clients.
// GET /auth/cli/{provider}?mode=http|code&port=PORT
func (h *OAuthHandler) HandleCLILogin(cfg CLILoginConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := chi.URLParam(r, "provider")
		if provider != "google" {
			http.Error(w, "unsupported provider", http.StatusBadRequest)
			return
		}

		mode := r.URL.Query().Get("mode")
		if mode != "http" && mode != "code" {
			http.Error(w, "mode must be 'http' or 'code'", http.StatusBadRequest)
			return
		}

		verifier, err := GenerateVerifier()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		meta := map[string]string{"mode": mode}
		if mode == "http" {
			port := r.URL.Query().Get("port")
			portNum, err := strconv.Atoi(port)
			if err != nil || portNum < 1024 || portNum > 65535 {
				http.Error(w, "port must be a number between 1024 and 65535", http.StatusBadRequest)
				return
			}
			meta["port"] = strconv.Itoa(portNum)
		}

		state, err := h.StateStore.CreateWithMeta(verifier, meta)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Use a CLI-specific callback URL.
		redirectURL := cfg.BaseURL + "/auth/cli/callback"

		params := url.Values{
			"client_id":             {cfg.GoogleCfg.ClientID},
			"redirect_uri":          {redirectURL},
			"response_type":         {"code"},
			"scope":                 {"openid email profile"},
			"state":                 {state},
			"code_challenge":        {ChallengeS256(verifier)},
			"code_challenge_method": {"S256"},
			"access_type":           {"offline"},
		}

		http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+params.Encode(), http.StatusFound)
	}
}

// HandleCLICallback receives the OAuth callback from Google.
// HTTP mode: redirects to localhost with tokens. Code mode: shows a one-time code.
// GET /auth/cli/callback?code=...&state=...
func (h *OAuthHandler) HandleCLICallback(cfg CLILoginConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		if code == "" || state == "" {
			http.Error(w, "missing code or state", http.StatusBadRequest)
			return
		}

		verifier, meta, err := h.StateStore.ConsumeMeta(state)
		if err != nil {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}

		// Exchange with Google using CLI-specific redirect URL.
		cliRedirectCfg := GoogleConfig{
			ClientID:     cfg.GoogleCfg.ClientID,
			ClientSecret: cfg.GoogleCfg.ClientSecret,
			RedirectURL:  cfg.BaseURL + "/auth/cli/callback",
		}

		token, err := h.exchangeGoogleCode(cliRedirectCfg, code, verifier)
		if err != nil {
			log.Printf("cli google code exchange: %v", err)
			http.Error(w, "authentication failed", http.StatusBadGateway)
			return
		}

		info, err := h.fetchGoogleUserInfo(token)
		if err != nil {
			log.Printf("cli google userinfo: %v", err)
			http.Error(w, "authentication failed", http.StatusBadGateway)
			return
		}

		user, err := h.UserStore.CreateOrUpdate(r.Context(), "google", info.ID, info.Email, info.Name, info.Picture)
		if err != nil {
			log.Printf("cli user upsert: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		tokens, err := h.issueTokenPair(r.Context(), user)
		if err != nil {
			log.Printf("cli issue tokens: %v", err)
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}

		mode := meta["mode"]
		switch mode {
		case "http":
			port := meta["port"]
			redirectURL := fmt.Sprintf("http://127.0.0.1:%s/callback?access_token=%s&refresh_token=%s&token_type=%s&expires_in=%d",
				port,
				url.QueryEscape(tokens.AccessToken),
				url.QueryEscape(tokens.RefreshToken),
				url.QueryEscape(tokens.TokenType),
				tokens.ExpiresIn,
			)
			http.Redirect(w, r, redirectURL, http.StatusFound)

		case "code":
			displayCode, err := cfg.CodeStore.Create(*tokens)
			if err != nil {
				log.Printf("cli code create: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, cliCodePageHTML, displayCode)

		default:
			http.Error(w, "invalid mode in state", http.StatusBadRequest)
		}
	}
}

// HandleCLIExchange exchanges a one-time code for tokens.
// POST /auth/cli/exchange  {"code": "XXXX-YYYY"}
func (h *OAuthHandler) HandleCLIExchange(cfg CLILoginConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
			http.Error(w, "code required", http.StatusBadRequest)
			return
		}

		tokens, err := cfg.CodeStore.Consume(req.Code)
		if err != nil {
			http.Error(w, "invalid or expired code", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokens)
	}
}

const cliCodePageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>KittyPaw Login</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; background: #f5f5f7; }
  .card { background: white; border-radius: 12px; padding: 48px; text-align: center; box-shadow: 0 4px 24px rgba(0,0,0,0.1); }
  .code { font-size: 48px; font-weight: 700; letter-spacing: 8px; color: #1d1d1f; margin: 24px 0; font-family: 'SF Mono', 'Fira Code', monospace; }
  .hint { color: #86868b; font-size: 14px; }
</style>
</head>
<body>
<div class="card">
  <h2>Enter this code in your terminal</h2>
  <div class="code">%s</div>
  <p class="hint">This code expires in 5 minutes.</p>
</div>
</body>
</html>`
