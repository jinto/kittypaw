package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestBindOrDefault(t *testing.T) {
	tests := []struct {
		bind string
		want string
	}{
		{"", ":3000"},
		{":8080", ":8080"},
		{"0.0.0.0:9000", "0.0.0.0:9000"},
	}
	for _, tt := range tests {
		cfg := ServerConfig{Bind: tt.bind}
		got := cfg.BindOrDefault()
		if got != tt.want {
			t.Errorf("BindOrDefault(%q) = %q, want %q", tt.bind, got, tt.want)
		}
	}
}

func TestPermissionPolicyParsing(t *testing.T) {
	tomlContent := `
autonomy_level = "supervised"

[permissions]
require_approval = ["Shell.exec", "Git.push", "File.write"]
timeout_seconds = 60
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(path, []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(cfg.Permissions.RequireApproval) != 3 {
		t.Fatalf("expected 3 require_approval entries, got %d", len(cfg.Permissions.RequireApproval))
	}
	if cfg.Permissions.RequireApproval[0] != "Shell.exec" {
		t.Errorf("expected Shell.exec, got %s", cfg.Permissions.RequireApproval[0])
	}
	if cfg.Permissions.TimeoutSeconds != 60 {
		t.Errorf("expected timeout 60, got %d", cfg.Permissions.TimeoutSeconds)
	}
}

func TestPermissionPolicyDefaults(t *testing.T) {
	// When [permissions] is omitted, RequireApproval should be nil.
	tomlContent := `autonomy_level = "supervised"`

	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if cfg.Permissions.RequireApproval != nil {
		t.Errorf("expected nil RequireApproval, got %v", cfg.Permissions.RequireApproval)
	}
	if cfg.Permissions.TimeoutSeconds != 0 {
		t.Errorf("expected 0 timeout, got %d", cfg.Permissions.TimeoutSeconds)
	}

	// DefaultRequireApproval should have sensible entries.
	if len(DefaultRequireApproval) < 4 {
		t.Errorf("DefaultRequireApproval too short: %v", DefaultRequireApproval)
	}
}
