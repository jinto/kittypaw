package config_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/kittypaw-app/kittyapi/internal/config"
)

// generatePEM returns a fresh PEM-encoded RSA private key of the given
// bit size. Inline so tests don't depend on the static testdata/jwks/
// fixture (built in T5) — keeps T3 self-contained.
func generatePEM(t *testing.T, bits int) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// loadWithEnv injects required env vars then calls Load(). Bare-minimum
// fields are always set so test failures isolate to the JWT key path.
func loadWithEnv(t *testing.T, envs map[string]string) (*config.Config, error) {
	t.Helper()
	base := map[string]string{
		"DATABASE_URL": "postgres://localhost/x",
		"JWT_SECRET":   "test-only-dummy-not-a-real-secret", //gitleaks:allow
	}
	for k, v := range envs {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
	return config.Load()
}

func TestConfig_LoadJWTKey_Valid(t *testing.T) {
	pemStr := generatePEM(t, 2048)
	b64 := base64.StdEncoding.EncodeToString([]byte(pemStr))

	cfg, err := loadWithEnv(t, map[string]string{"JWT_PRIVATE_KEY_PEM_B64": b64})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWTPrivateKey == nil {
		t.Fatal("JWTPrivateKey is nil")
	}
	if cfg.JWTKID == "" {
		t.Fatal("JWTKID is empty")
	}
	if cfg.JWTPrivateKey.N.BitLen() < 2048 {
		t.Fatalf("loaded key bits = %d, want ≥2048", cfg.JWTPrivateKey.N.BitLen())
	}
}

// TestConfig_LoadJWTKey_BadBase64 ensures we fail-fast at startup
// rather than letting an undecodable env survive into request-time.
func TestConfig_LoadJWTKey_BadBase64(t *testing.T) {
	_, err := loadWithEnv(t, map[string]string{"JWT_PRIVATE_KEY_PEM_B64": "!!!not-base64"})
	if err == nil {
		t.Fatal("expected error for malformed base64")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "base64") &&
		!strings.Contains(strings.ToLower(err.Error()), "decode") {
		t.Fatalf("error doesn't mention base64/decode: %v", err)
	}
}

// TestConfig_LoadJWTKey_BadPEM covers the case where base64 decodes
// successfully but the bytes aren't valid PEM (typo, truncated key, etc).
func TestConfig_LoadJWTKey_BadPEM(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("not a pem"))
	_, err := loadWithEnv(t, map[string]string{"JWT_PRIVATE_KEY_PEM_B64": b64})
	if err == nil {
		t.Fatal("expected error for non-PEM bytes")
	}
}

// TestConfig_LoadJWTKey_TooSmall enforces the 2048-bit floor — RSA
// keys below this are not considered safe for production signing today.
func TestConfig_LoadJWTKey_TooSmall(t *testing.T) {
	pemStr := generatePEM(t, 1024)
	b64 := base64.StdEncoding.EncodeToString([]byte(pemStr))

	_, err := loadWithEnv(t, map[string]string{"JWT_PRIVATE_KEY_PEM_B64": b64})
	if err == nil {
		t.Fatal("expected error for 1024-bit key")
	}
	if !strings.Contains(err.Error(), "2048") {
		t.Fatalf("error doesn't mention 2048-bit floor: %v", err)
	}
}
