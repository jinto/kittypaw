package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitTenant_HappyPath_Personal(t *testing.T) {
	tenantsDir := t.TempDir()

	tt, err := InitTenant(tenantsDir, "alice", TenantOpts{
		TelegramToken: "12345:alice-token",
		AdminChatID:   "111",
	})
	if err != nil {
		t.Fatalf("InitTenant: %v", err)
	}
	if tt == nil {
		t.Fatal("InitTenant returned nil tenant")
	}
	if tt.ID != "alice" {
		t.Errorf("ID = %q, want alice", tt.ID)
	}

	dir := filepath.Join(tenantsDir, "alice")
	for _, sub := range []string{"data", "skills", "profiles", "packages"} {
		if info, err := os.Stat(filepath.Join(dir, sub)); err != nil || !info.IsDir() {
			t.Errorf("expected subdir %q, err=%v", sub, err)
		}
	}

	cfgPath := filepath.Join(dir, "config.toml")
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.IsFamily {
		t.Error("personal tenant should not have IsFamily=true")
	}
	if len(cfg.Channels) != 1 || cfg.Channels[0].ChannelType != ChannelTelegram {
		t.Errorf("expected one telegram channel, got %+v", cfg.Channels)
	}
	if cfg.Channels[0].Token != "12345:alice-token" {
		t.Errorf("token = %q, want 12345:alice-token", cfg.Channels[0].Token)
	}
	if len(cfg.AdminChatIDs) != 1 || cfg.AdminChatIDs[0] != "111" {
		t.Errorf("AdminChatIDs = %v, want [111]", cfg.AdminChatIDs)
	}

	// Config holds a bot token; enforce 0600. Windows CI fakes perms.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(cfgPath)
		if err != nil {
			t.Fatalf("stat config: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("config.toml perm = %o, want 0600", info.Mode().Perm())
		}
	}

	if _, err := os.Stat(filepath.Join(tenantsDir, ".alice.staging")); !os.IsNotExist(err) {
		t.Errorf("staging dir should be gone after commit, stat err=%v", err)
	}
}

func TestInitTenant_HappyPath_Family(t *testing.T) {
	tenantsDir := t.TempDir()

	tt, err := InitTenant(tenantsDir, "family", TenantOpts{IsFamily: true})
	if err != nil {
		t.Fatalf("InitTenant family: %v", err)
	}

	cfg, err := LoadConfig(filepath.Join(tt.BaseDir, "config.toml"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.IsFamily {
		t.Error("expected IsFamily=true")
	}
	if len(cfg.Channels) != 0 {
		t.Errorf("family tenant must declare no channels, got %+v", cfg.Channels)
	}
}

// Re-adding must not clobber existing DB/secrets/skills.
func TestInitTenant_DuplicateID(t *testing.T) {
	tenantsDir := t.TempDir()
	if _, err := InitTenant(tenantsDir, "alice", TenantOpts{TelegramToken: "12345:a"}); err != nil {
		t.Fatalf("first InitTenant: %v", err)
	}

	marker := filepath.Join(tenantsDir, "alice", "data", "do-not-delete")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	_, err := InitTenant(tenantsDir, "alice", TenantOpts{TelegramToken: "99999:b"})
	if !errors.Is(err, ErrTenantExists) {
		t.Fatalf("expected ErrTenantExists, got %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker file missing — existing tenant was clobbered: %v", err)
	}
}

// Collision must surface before any filesystem write — otherwise the error only appears at daemon startup.
func TestInitTenant_DuplicateTelegramToken(t *testing.T) {
	tenantsDir := t.TempDir()
	if _, err := InitTenant(tenantsDir, "alice", TenantOpts{TelegramToken: "shared"}); err != nil {
		t.Fatalf("alice: %v", err)
	}

	_, err := InitTenant(tenantsDir, "bob", TenantOpts{TelegramToken: "shared"})
	if err == nil {
		t.Fatal("expected duplicate-token error, got nil")
	}
	if !strings.Contains(err.Error(), "telegram bot_token") {
		t.Errorf("error should cite telegram bot_token: %q", err.Error())
	}

	if _, err := os.Stat(filepath.Join(tenantsDir, "bob")); !os.IsNotExist(err) {
		t.Errorf("bob dir should not exist after collision, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(tenantsDir, ".bob.staging")); !os.IsNotExist(err) {
		t.Errorf("bob staging should be cleaned up, err=%v", err)
	}
}

// Family-no-channels invariant must reject before any file is written.
func TestInitTenant_FamilyWithToken(t *testing.T) {
	tenantsDir := t.TempDir()

	_, err := InitTenant(tenantsDir, "family", TenantOpts{
		IsFamily:      true,
		TelegramToken: "12345:family",
	})
	if err == nil {
		t.Fatal("expected error for family + telegram token, got nil")
	}
	if !strings.Contains(err.Error(), "family") {
		t.Errorf("error should cite family: %q", err.Error())
	}
	if _, err := os.Stat(filepath.Join(tenantsDir, "family")); !os.IsNotExist(err) {
		t.Errorf("family dir should not exist after rejection")
	}
}

// Accepting "../escape" would be a traversal vulnerability.
func TestInitTenant_InvalidID(t *testing.T) {
	tenantsDir := t.TempDir()

	_, err := InitTenant(tenantsDir, "../escape", TenantOpts{TelegramToken: "x"})
	if err == nil {
		t.Fatal("expected error for invalid tenant id")
	}
}

// Without this, SIGKILL/disk-full during provisioning would one-shot break the feature.
func TestInitTenant_StagingRecovery(t *testing.T) {
	tenantsDir := t.TempDir()

	stale := filepath.Join(tenantsDir, ".alice.staging")
	if err := os.MkdirAll(filepath.Join(stale, "data"), 0o755); err != nil {
		t.Fatalf("seed stale staging: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, "config.toml"), []byte("garbage"), 0o600); err != nil {
		t.Fatalf("seed stale config: %v", err)
	}

	if _, err := InitTenant(tenantsDir, "alice", TenantOpts{TelegramToken: "12345:a"}); err != nil {
		t.Fatalf("InitTenant after staging crash: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale staging should be cleaned up, stat err=%v", err)
	}
	cfg, err := LoadConfig(filepath.Join(tenantsDir, "alice", "config.toml"))
	if err != nil {
		t.Fatalf("LoadConfig after recovery: %v", err)
	}
	if len(cfg.Channels) != 1 {
		t.Errorf("expected 1 channel after recovery, got %d", len(cfg.Channels))
	}
}
