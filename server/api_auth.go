package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jinto/kittypaw/core"
)

const (
	webSessionCookieName = "kittypaw_session"
	webSessionTTL        = 12 * time.Hour
)

type webSession struct {
	AccountID string `json:"account_id"`
	Expires   int64  `json:"expires"`
}

func newWebSessionKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("server.New: generate web session key: " + err.Error())
	}
	return key
}

func newLocalAuthStore() *core.LocalAuthStore {
	path, err := core.LocalAuthPath()
	if err != nil {
		return nil
	}
	return core.NewLocalAuthStore(path)
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID string `json:"account_id"`
		Password  string `json:"password"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.AccountID == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "account_id and password are required")
		return
	}
	if !s.accountActive(body.AccountID) {
		writeError(w, http.StatusUnauthorized, "invalid account_id or password")
		return
	}
	if s.localAuth == nil {
		writeError(w, http.StatusInternalServerError, "local auth store unavailable")
		return
	}
	ok, err := s.localAuth.VerifyPassword(body.AccountID, body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "verify password")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid account_id or password")
		return
	}

	http.SetCookie(w, s.newWebSessionCookie(r, body.AccountID, time.Now().Add(webSessionTTL)))
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"account_id":    body.AccountID,
		"is_default":    body.AccountID == s.defaultAccountID(),
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, clearWebSessionCookie(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	required, err := s.localAuthRequired()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read local auth store")
		return
	}
	if !required {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false,
			"auth_required": false,
		})
		return
	}

	accountID, ok := s.webSessionAccountID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"auth_required": true,
		"account_id":    accountID,
		"is_default":    accountID == s.defaultAccountID(),
	})
}

func (s *Server) browserAPIToken(r *http.Request) (string, bool) {
	required, err := s.localAuthRequired()
	if err != nil || !required {
		return s.effectiveAPIKey(), err == nil
	}
	cookie, err := r.Cookie(webSessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	accountID, ok := s.webSessionTokenAccountID(cookie.Value)
	if !ok || accountID != s.defaultAccountID() {
		return "", false
	}
	return cookie.Value, true
}

func (s *Server) requireWebSessionIfAuthUsers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		required, err := s.localAuthRequired()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "read local auth store")
			return
		}
		if !required {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := s.webSessionAccountID(r); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireDefaultWebSessionIfAuthUsers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		required, err := s.localAuthRequired()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "read local auth store")
			return
		}
		if !required {
			next.ServeHTTP(w, r)
			return
		}
		accountID, ok := s.webSessionAccountID(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if accountID != s.defaultAccountID() {
			writeError(w, http.StatusForbidden, "default account session required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) localAuthRequired() (bool, error) {
	if s.localAuth == nil {
		return false, nil
	}
	return s.localAuth.HasUsers()
}

func (s *Server) accountActive(accountID string) bool {
	return s.accountDepsForID(accountID) != nil
}

func (s *Server) newWebSessionCookie(r *http.Request, accountID string, expires time.Time) *http.Cookie {
	payload := webSession{
		AccountID: accountID,
		Expires:   expires.UTC().Unix(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		panic("marshal web session: " + err.Error())
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.webSessionKey)
	mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return &http.Cookie{
		Name:     webSessionCookieName,
		Value:    body + "." + sig,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	}
}

func clearWebSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     webSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	}
}

func (s *Server) webSessionAccountID(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(webSessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return s.webSessionTokenAccountID(cookie.Value)
}

func (s *Server) webSessionTokenAccountID(token string) (string, bool) {
	session, err := s.parseWebSession(token)
	if err != nil {
		return "", false
	}
	if time.Now().UTC().Unix() > session.Expires {
		return "", false
	}
	if !s.accountActive(session.AccountID) {
		return "", false
	}
	if s.localAuth != nil {
		active, err := s.localAuth.IsActiveUser(session.AccountID)
		if err != nil || !active {
			return "", false
		}
	}
	return session.AccountID, true
}

func (s *Server) defaultWebSessionTokenOK(token string) bool {
	accountID, ok := s.webSessionTokenAccountID(token)
	return ok && accountID == s.defaultAccountID()
}

func (s *Server) parseWebSession(value string) (webSession, error) {
	var session webSession
	parts := strings.Split(value, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return session, errors.New("invalid session format")
	}
	if len(s.webSessionKey) == 0 {
		return session, errors.New("missing session key")
	}

	mac := hmac.New(sha256.New, s.webSessionKey)
	mac.Write([]byte(parts[0]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return session, err
	}
	if !hmac.Equal(got, want) {
		return session, errors.New("invalid session signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return session, err
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		return session, err
	}
	return session, nil
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
