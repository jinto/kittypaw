package connect

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kittypaw-app/kittyportal/internal/auth"
)

const maxConnectBodyBytes = 1024

type Handler struct {
	Gmail      *GmailProvider
	StateStore *auth.StateStore
	CodeStore  *CodeStore
}

func NewHandler(gmail *GmailProvider, states *auth.StateStore, codes *CodeStore) *Handler {
	return &Handler{Gmail: gmail, StateStore: states, CodeStore: codes}
}

func (h *Handler) HandleGmailLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		if mode != "http" && mode != "code" {
			http.Error(w, "mode must be 'http' or 'code'", http.StatusBadRequest)
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

		verifier, err := auth.GenerateVerifier()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		state, err := h.StateStore.CreateWithMeta(verifier, meta)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, h.Gmail.AuthURL(state, verifier), http.StatusFound)
	}
}

func (h *Handler) HandleGmailCallback() http.HandlerFunc {
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
		tokens, err := h.Gmail.ExchangeCode(r.Context(), code, verifier)
		if err != nil {
			slog.Error("connect gmail code exchange failed", "err", err)
			http.Error(w, "authentication failed", http.StatusBadGateway)
			return
		}
		displayCode, err := h.CodeStore.Create(tokens)
		if err != nil {
			slog.Error("connect code create failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		switch meta["mode"] {
		case "http":
			port := meta["port"]
			redirectURL := fmt.Sprintf("http://127.0.0.1:%s/callback?code=%s", port, url.QueryEscape(displayCode))
			http.Redirect(w, r, redirectURL, http.StatusFound)
		case "code":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'")
			w.Header().Set("Referrer-Policy", "no-referrer")
			_, _ = fmt.Fprint(w, connectCodePage(displayCode))
		default:
			http.Error(w, "invalid mode in state", http.StatusBadRequest)
		}
	}
}

func (h *Handler) HandleCLIExchange() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxConnectBodyBytes)
		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
			http.Error(w, "code required", http.StatusBadRequest)
			return
		}
		tokens, err := h.CodeStore.Consume(req.Code)
		if err != nil {
			http.Error(w, "invalid or expired code", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokens)
	}
}

func (h *Handler) HandleGmailRefresh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxConnectBodyBytes)
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
			http.Error(w, "refresh_token required", http.StatusBadRequest)
			return
		}
		tokens, err := h.Gmail.Refresh(r.Context(), req.RefreshToken)
		if err != nil {
			slog.Error("connect gmail refresh failed", "err", err)
			http.Error(w, "refresh failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokens)
	}
}

func connectCodePage(code string) string {
	escaped := html.EscapeString(code)
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>KittyPaw Connect</title>
</head>
<body>
  <main>
    <p>KittyPaw Connect</p>
    <code data-code="%s">%s</code>
  </main>
</body>
</html>`, escaped, escaped)
}
