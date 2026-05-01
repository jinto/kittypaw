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

// Namespace invariant: service URLs are stored under the portal host
// (NamespaceForURL(apiURL)). The stored api_base_url may point to a different
// host — that's intentional. The namespace tracks auth identity, not service
// topology, so token refresh keeps working unchanged across relay migrations.

// saveOrDelete writes value under (ns, key), or deletes the key when value is
// empty. Used by Save*URL helpers so that a /discovery response with an empty
// field erases a stale value instead of persisting "".
func (m *APITokenManager) saveOrDelete(ns, key, value string) error {
	if value == "" {
		return m.secrets.Delete(ns, key)
	}
	return m.secrets.Set(ns, key, value)
}

const (
	kakaoRelayURLKey   = "kakao_relay_url"
	kakaoRelayWSURLKey = "kakao_relay_ws_url"
)

// SaveKakaoRelayBaseURL stores the KakaoTalk relay server base URL from GET /discovery.
// Empty value deletes the key so stale URLs don't survive relay migrations.
func (m *APITokenManager) SaveKakaoRelayBaseURL(apiURL, relayURL string) error {
	return m.saveOrDelete(NamespaceForURL(apiURL), kakaoRelayURLKey, relayURL)
}

// LoadKakaoRelayBaseURL returns the stored KakaoTalk relay server base URL.
func (m *APITokenManager) LoadKakaoRelayBaseURL(apiURL string) (string, bool) {
	return m.secrets.Get(NamespaceForURL(apiURL), kakaoRelayURLKey)
}

// SaveAPIBaseURL stores the API base URL from GET /discovery.
// Save-only for now (see plan D5/D6); reserved for future exchange/refresh routing.
// Empty value deletes the key.
func (m *APITokenManager) SaveAPIBaseURL(apiURL, apiBaseURL string) error {
	return m.saveOrDelete(NamespaceForURL(apiURL), "api_base_url", apiBaseURL)
}

// LoadAPIBaseURL returns the stored API base URL.
func (m *APITokenManager) LoadAPIBaseURL(apiURL string) (string, bool) {
	return m.secrets.Get(NamespaceForURL(apiURL), "api_base_url")
}

// SaveSkillsRegistryURL stores the skills registry URL from GET /discovery.
// Save-only for now (see plan D6); not yet routed into registryClient.
// Empty value deletes the key.
func (m *APITokenManager) SaveSkillsRegistryURL(apiURL, skillsRegistryURL string) error {
	return m.saveOrDelete(NamespaceForURL(apiURL), "skills_registry_url", skillsRegistryURL)
}

// LoadSkillsRegistryURL returns the stored skills registry URL.
func (m *APITokenManager) LoadSkillsRegistryURL(apiURL string) (string, bool) {
	return m.secrets.Get(NamespaceForURL(apiURL), "skills_registry_url")
}

// SaveKakaoRelayWSURL stores the full Kakao relay WebSocket URL built from
// a client-side relay registration (baseURL + /ws/{token}).
func (m *APITokenManager) SaveKakaoRelayWSURL(apiURL, wsURL string) error {
	ns := NamespaceForURL(apiURL)
	return m.secrets.Set(ns, kakaoRelayWSURLKey, wsURL)
}

// LoadKakaoRelayWSURL returns the stored Kakao relay WebSocket URL.
func (m *APITokenManager) LoadKakaoRelayWSURL(apiURL string) (string, bool) {
	ns := NamespaceForURL(apiURL)
	return m.secrets.Get(ns, kakaoRelayWSURLKey)
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
