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

// TestFamilyShareParsing enforces the TOML wire format for family accounts.
// The shape ([share.<peer>] read=[...]) is the user-facing contract the spec
// promises — this regression pins it so a future config refactor can't
// silently reshape it into something that breaks existing family installs.
func TestFamilyShareParsing(t *testing.T) {
	tomlContent := `
is_family = true

[share.family]
read = ["memory/weather.json", "memory/household.json"]

[share.alice]
read = ["summary.md"]
`
	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !cfg.IsFamily {
		t.Errorf("IsFamily=true expected")
	}
	if len(cfg.Share) != 2 {
		t.Fatalf("expected 2 share peers, got %d: %#v", len(cfg.Share), cfg.Share)
	}
	family := cfg.Share["family"]
	if len(family.Read) != 2 || family.Read[0] != "memory/weather.json" {
		t.Errorf("share.family.read wrong: %v", family.Read)
	}
	alice := cfg.Share["alice"]
	if len(alice.Read) != 1 || alice.Read[0] != "summary.md" {
		t.Errorf("share.alice.read wrong: %v", alice.Read)
	}
}

// TestFamilyShareDefaults locks in the zero-state contract — a personal
// account config with no [share] blocks must decode to IsFamily=false and
// a nil Share map. If this drifts (e.g. share becomes a required field),
// every existing account breaks at daemon start.
func TestFamilyShareDefaults(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`autonomy_level = "full"`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.IsFamily {
		t.Error("IsFamily should default to false")
	}
	if cfg.Share != nil {
		t.Errorf("Share should default to nil, got %#v", cfg.Share)
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
