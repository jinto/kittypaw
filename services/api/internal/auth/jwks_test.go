package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittyapi/internal/auth"
)

// TestHandleJWKS_ResponseShape pins the wire contract: status 200,
// Content-Type application/json, body that decodes into a JWK Set with
// exactly one key whose kid matches the provider.
func TestHandleJWKS_ResponseShape(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := auth.NewSingleKeyProvider(&key.PublicKey, "kid-abc")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()
	auth.HandleJWKS(provider).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var got auth.JWKSet
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(got.Keys) != 1 {
		t.Fatalf("keys len = %d, want 1", len(got.Keys))
	}
	if got.Keys[0].Kid != "kid-abc" {
		t.Fatalf("kid = %q, want kid-abc", got.Keys[0].Kid)
	}
	if got.Keys[0].Alg != "RS256" {
		t.Fatalf("alg = %q, want RS256", got.Keys[0].Alg)
	}
}

// TestHandleJWKS_CacheControl pins the cache header. kittychat-side
// cache TTL (10min) is aligned to this — drift here breaks the
// rotation contract (old key 30min overlap = access TTL 15 + cache 10
// + safety 5).
func TestHandleJWKS_CacheControl(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := auth.NewSingleKeyProvider(&key.PublicKey, "kid-abc")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	w := httptest.NewRecorder()
	auth.HandleJWKS(provider).ServeHTTP(w, req)

	const want = "public, max-age=600"
	if got := w.Header().Get("Cache-Control"); got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
}
