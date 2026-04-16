package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNamespaceForURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"http://localhost:8080", "kittypaw-api/localhost:8080"},
		{"https://api.kittypaw.com", "kittypaw-api/api.kittypaw.com"},
		{"http://10.0.0.1:3000", "kittypaw-api/10.0.0.1:3000"},
		{"https://api.kittypaw.com:443", "kittypaw-api/api.kittypaw.com:443"},
	}
	for _, tt := range tests {
		got := NamespaceForURL(tt.url)
		if got != tt.want {
			t.Errorf("NamespaceForURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func makeJWT(expUnix int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	claims, _ := json.Marshal(map[string]any{"uid": "user-1", "exp": expUnix})
	payload := base64.RawURLEncoding.EncodeToString(claims)
	sig := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payload + "." + sig
}

func TestIsJWTExpired(t *testing.T) {
	tests := []struct {
		name string
		exp  int64
		want bool
	}{
		{"future 10min", time.Now().Add(10 * time.Minute).Unix(), false},
		{"past 1min", time.Now().Add(-1 * time.Minute).Unix(), true},
		{"within grace 15s", time.Now().Add(15 * time.Second).Unix(), true},
		{"exactly at grace 30s", time.Now().Add(30 * time.Second).Unix(), true},
		{"just outside grace 31s", time.Now().Add(31 * time.Second).Unix(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := makeJWT(tt.exp)
			got := isJWTExpired(token)
			if got != tt.want {
				t.Errorf("isJWTExpired(exp=%d) = %v, want %v", tt.exp, got, tt.want)
			}
		})
	}
}

func TestIsJWTExpired_InvalidTokens(t *testing.T) {
	tests := []string{
		"",
		"not-a-jwt",
		"a.b", // only 2 parts
		"a." + base64.RawURLEncoding.EncodeToString([]byte("{}")) + ".c", // no exp
	}
	for _, token := range tests {
		if !isJWTExpired(token) {
			t.Errorf("isJWTExpired(%q) should be true for invalid token", token)
		}
	}
}

func TestAPITokenManager_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	secrets := &SecretsStore{
		path: dir + "/secrets.json",
		data: make(map[string]map[string]string),
	}

	mgr := NewAPITokenManager("", secrets)
	apiURL := "http://localhost:8080"

	validToken := makeJWT(time.Now().Add(10 * time.Minute).Unix())
	if err := mgr.SaveTokens(apiURL, validToken, "refresh-abc"); err != nil {
		t.Fatal(err)
	}

	got, err := mgr.LoadAccessToken(apiURL)
	if err != nil {
		t.Fatal(err)
	}
	if got != validToken {
		t.Errorf("LoadAccessToken = %q, want %q", got, validToken)
	}
}

func TestAPITokenManager_NotLoggedIn(t *testing.T) {
	secrets := &SecretsStore{
		path: "/dev/null",
		data: make(map[string]map[string]string),
	}
	mgr := NewAPITokenManager("", secrets)

	got, err := mgr.LoadAccessToken("http://localhost:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty token for not-logged-in, got %q", got)
	}
}

func TestAPITokenManager_AutoRefresh(t *testing.T) {
	newToken := makeJWT(time.Now().Add(15 * time.Minute).Unix())

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"new-refresh","token_type":"Bearer","expires_in":900}`, newToken)
	}))
	defer ts.Close()

	secrets := &SecretsStore{
		path: t.TempDir() + "/secrets.json",
		data: make(map[string]map[string]string),
	}
	mgr := NewAPITokenManager("", secrets)
	mgr.client = ts.Client()

	// Save an expired token.
	expiredToken := makeJWT(time.Now().Add(-1 * time.Minute).Unix())
	mgr.SaveTokens(ts.URL, expiredToken, "old-refresh")

	got, err := mgr.LoadAccessToken(ts.URL)
	if err != nil {
		t.Fatalf("LoadAccessToken: %v", err)
	}
	if got != newToken {
		t.Errorf("expected refreshed token, got %q", got)
	}

	// Verify the new refresh token was saved.
	ns := NamespaceForURL(ts.URL)
	if rt, _ := secrets.Get(ns, "refresh_token"); rt != "new-refresh" {
		t.Errorf("expected new refresh token saved, got %q", rt)
	}
}

func TestAPITokenManager_ClearTokens(t *testing.T) {
	secrets := &SecretsStore{
		path: t.TempDir() + "/secrets.json",
		data: make(map[string]map[string]string),
	}
	mgr := NewAPITokenManager("", secrets)

	validToken := makeJWT(time.Now().Add(10 * time.Minute).Unix())
	mgr.SaveTokens("http://localhost:8080", validToken, "refresh")
	mgr.ClearTokens("http://localhost:8080")

	got, err := mgr.LoadAccessToken("http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("expected empty after clear, got %q", got)
	}
}
