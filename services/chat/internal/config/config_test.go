package config

import (
	"testing"
)

func TestLoadRequiresStaticMVPSecrets(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "")
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
}

func TestLoadPrefersExplicitBindAddrOverPort(t *testing.T) {
	t.Setenv("KITTYCHAT_API_TOKEN", "api_secret")
	t.Setenv("KITTYCHAT_DEVICE_TOKEN", "dev_secret")
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
