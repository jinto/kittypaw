package config

import (
	"testing"
)

func TestLoadRequiresStaticMVPSecrets(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "")
	t.Setenv("KITTYCHAT_JWT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_USER_ID", "")
	t.Setenv("KITTYCHAT_DEVICE_ID", "")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want missing env error")
	}
}

func TestLoadUsesEnvAndDefaults(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "api_secret")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "dev_secret")
	t.Setenv("KITTYCHAT_JWT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_USER_ID", "user_1")
	t.Setenv("KITTYCHAT_DEVICE_ID", "dev_1")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "alice")
	t.Setenv("PORT", "")
	t.Setenv("KITTYCHAT_BIND_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BindAddr != ":8080" {
		t.Fatalf("BindAddr = %q, want :8080", cfg.BindAddr)
	}
	if cfg.APIToken != "api_secret" || cfg.DeviceToken != "dev_secret" {
		t.Fatalf("tokens not loaded: %+v", cfg)
	}
	if cfg.UserID != "user_1" || cfg.DeviceID != "dev_1" || cfg.LocalAccountID != "alice" {
		t.Fatalf("principal not loaded: %+v", cfg)
	}
	if cfg.PublicBaseURL != "https://chat.kittypaw.app" {
		t.Fatalf("PublicBaseURL = %q, want prod chat URL", cfg.PublicBaseURL)
	}
	if cfg.APIAuthBaseURL != "https://portal.kittypaw.app/auth" {
		t.Fatalf("APIAuthBaseURL = %q, want prod portal auth URL", cfg.APIAuthBaseURL)
	}
}

func TestLoadUsesHostedWebAuthEnv(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "")
	t.Setenv("KITTYCHAT_JWT_SECRET", "test-jwt-secret-with-at-least-32-bytes")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_PUBLIC_BASE_URL", "http://localhost:8080")
	t.Setenv("KITTYCHAT_API_AUTH_BASE_URL", "http://localhost:9714/auth")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PublicBaseURL != "http://localhost:8080" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
	if cfg.APIAuthBaseURL != "http://localhost:9714/auth" {
		t.Fatalf("APIAuthBaseURL = %q", cfg.APIAuthBaseURL)
	}
}

func TestLoadPrefersExplicitBindAddrOverPort(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "api_secret")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "dev_secret")
	t.Setenv("KITTYCHAT_JWT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_USER_ID", "user_1")
	t.Setenv("KITTYCHAT_DEVICE_ID", "dev_1")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "alice")
	t.Setenv("PORT", "9090")
	t.Setenv("KITTYCHAT_BIND_ADDR", "127.0.0.1:7777")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:7777" {
		t.Fatalf("BindAddr = %q, want explicit bind addr", cfg.BindAddr)
	}
}

func TestLoadAcceptsUnixSocketBindAddr(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "api_secret")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "dev_secret")
	t.Setenv("KITTYCHAT_JWT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_USER_ID", "user_1")
	t.Setenv("KITTYCHAT_DEVICE_ID", "dev_1")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "alice")
	t.Setenv("PORT", "9090")
	t.Setenv("KITTYCHAT_BIND_ADDR", "/tmp/kittychat.sock")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BindAddr != "/tmp/kittychat.sock" {
		t.Fatalf("BindAddr = %q, want unix socket path", cfg.BindAddr)
	}
}

func TestLoadUsesJWTSecretInsteadOfStaticAPIToken(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "dev_secret")
	t.Setenv("KITTYCHAT_JWT_SECRET", "test-jwt-secret-with-at-least-32-bytes")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_USER_ID", "user_1")
	t.Setenv("KITTYCHAT_DEVICE_ID", "dev_1")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "alice")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JWTSecret != "test-jwt-secret-with-at-least-32-bytes" {
		t.Fatalf("JWTSecret = %q", cfg.JWTSecret)
	}
}

func TestLoadAllowsJWTOnlyCredentials(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "")
	t.Setenv("KITTYCHAT_JWT_SECRET", "test-jwt-secret-with-at-least-32-bytes")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_USER_ID", "")
	t.Setenv("KITTYCHAT_DEVICE_ID", "")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JWTSecret != "test-jwt-secret-with-at-least-32-bytes" {
		t.Fatalf("JWTSecret = %q", cfg.JWTSecret)
	}
}

func TestLoadAllowsJWKSOnlyCredentials(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "")
	t.Setenv("KITTYCHAT_JWT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "https://portal.kittypaw.app/.well-known/jwks.json")
	t.Setenv("KITTYCHAT_USER_ID", "")
	t.Setenv("KITTYCHAT_DEVICE_ID", "")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.JWKSURL != "https://portal.kittypaw.app/.well-known/jwks.json" {
		t.Fatalf("JWKSURL = %q", cfg.JWKSURL)
	}
}

func TestLoadRequiresStaticPrincipalWhenStaticTokenConfigured(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "dev_secret")
	t.Setenv("KITTYCHAT_JWT_SECRET", "test-jwt-secret-with-at-least-32-bytes")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_USER_ID", "")
	t.Setenv("KITTYCHAT_DEVICE_ID", "dev_1")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "alice")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing static principal error")
	}
}

func TestLoadRejectsShortJWTSecret(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "dev_secret")
	t.Setenv("KITTYCHAT_JWT_SECRET", "short")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("KITTYCHAT_JWKS_URL", "")
	t.Setenv("KITTYCHAT_USER_ID", "user_1")
	t.Setenv("KITTYCHAT_DEVICE_ID", "dev_1")
	t.Setenv("KITTYCHAT_LOCAL_ACCOUNT_ID", "alice")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want short JWT secret error")
	}
}
