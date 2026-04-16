package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// APITokenManager handles storage and auto-refresh of kittypaw-api tokens.
type APITokenManager struct {
	secrets *SecretsStore
	mu      sync.Mutex
	client  *http.Client
}

// NewAPITokenManager creates a manager backed by the given secrets store.
func NewAPITokenManager(_ string, secrets *SecretsStore) *APITokenManager {
	return &APITokenManager{
		secrets: secrets,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// NamespaceForURL converts an API URL to a secrets namespace.
// "http://localhost:8080" → "kittypaw-api/localhost:8080"
func NamespaceForURL(apiURL string) string {
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return "kittypaw-api/unknown"
	}
	return "kittypaw-api/" + parsed.Host
}

// SaveTokens stores the access token, refresh token, and API URL.
func (m *APITokenManager) SaveTokens(apiURL, accessToken, refreshToken string) error {
	ns := NamespaceForURL(apiURL)
	if err := m.secrets.Set(ns, "access_token", accessToken); err != nil {
		return fmt.Errorf("save access_token: %w", err)
	}
	if err := m.secrets.Set(ns, "refresh_token", refreshToken); err != nil {
		return fmt.Errorf("save refresh_token: %w", err)
	}
	if err := m.secrets.Set(ns, "api_url", apiURL); err != nil {
		return fmt.Errorf("save api_url: %w", err)
	}
	return nil
}

// LoadAccessToken returns a valid access token, refreshing if expired.
// Returns ("", nil) if not logged in.
func (m *APITokenManager) LoadAccessToken(apiURL string) (string, error) {
	ns := NamespaceForURL(apiURL)

	accessToken, ok := m.secrets.Get(ns, "access_token")
	if !ok || accessToken == "" {
		return "", nil // not logged in
	}

	if !isJWTExpired(accessToken) {
		return accessToken, nil
	}

	// Token expired — refresh under mutex (single-flight).
	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-check after acquiring lock (another goroutine may have refreshed).
	accessToken, _ = m.secrets.Get(ns, "access_token")
	if accessToken != "" && !isJWTExpired(accessToken) {
		return accessToken, nil
	}

	refreshToken, ok := m.secrets.Get(ns, "refresh_token")
	if !ok || refreshToken == "" {
		return "", fmt.Errorf("no refresh token available, please run: kittypaw login")
	}

	newAccess, err := m.refreshTokens(apiURL, refreshToken)
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w (please run: kittypaw login)", err)
	}
	return newAccess, nil
}

// ClearTokens removes stored tokens for an API.
func (m *APITokenManager) ClearTokens(apiURL string) error {
	ns := NamespaceForURL(apiURL)
	return m.secrets.DeletePackage(ns)
}

// refreshTokens calls POST /auth/token/refresh and saves new tokens.
func (m *APITokenManager) refreshTokens(apiURL, refreshToken string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	resp, err := m.client.Post(
		apiURL+"/auth/token/refresh",
		"application/json",
		strings.NewReader(string(payload)),
	)
	if err != nil {
		return "", fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, string(b))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode refresh response: %w", err)
	}

	ns := NamespaceForURL(apiURL)
	if err := m.secrets.Set(ns, "access_token", result.AccessToken); err != nil {
		return "", fmt.Errorf("save refreshed access_token: %w", err)
	}
	if result.RefreshToken != "" {
		if err := m.secrets.Set(ns, "refresh_token", result.RefreshToken); err != nil {
			return "", fmt.Errorf("save refreshed refresh_token: %w", err)
		}
	}
	return result.AccessToken, nil
}

// isJWTExpired checks the exp claim without verifying the signature.
// Returns true if the token is expired or within 30 seconds of expiry.
func isJWTExpired(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return true
	}

	// Decode the payload (middle segment).
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return true
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return true
	}

	// 30-second grace window.
	return time.Now().Unix()+30 >= claims.Exp
}
