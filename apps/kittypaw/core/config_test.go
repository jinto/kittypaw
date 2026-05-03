package core

import (
	"os"
	"path/filepath"
	"strings"
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

func TestConfigPathForAccount(t *testing.T) {
	t.Setenv("KITTYPAW_CONFIG_DIR", t.TempDir())
	got, err := ConfigPathForAccount("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, filepath.Join("accounts", "alice", "config.toml")) {
		t.Fatalf("ConfigPathForAccount = %q", got)
	}
}

func TestConfigPathForAccountRejectsInvalidID(t *testing.T) {
	t.Setenv("KITTYPAW_CONFIG_DIR", t.TempDir())
	if _, err := ConfigPathForAccount("../bad"); err == nil {
		t.Fatal("expected invalid account id error")
	}
}

func TestConfigV2ShapeParsing(t *testing.T) {
	tomlContent := `
version = 2
is_shared = true
freeform_fallback = true
autonomy_level = "full"
default_profile = "default"

[llm]
default = "main"
fallback = "backup"

[[llm.models]]
id = "main"
provider = "openai"
model = "gpt-5.5"
credential = "openai"
max_tokens = 4096

[[llm.models]]
id = "backup"
provider = "anthropic"
model = "claude-sonnet-4-6"
credential = "anthropic"
max_tokens = 4096

[[channels]]
id = "telegram"
type = "telegram"
allowed_chat_ids = ["54076829"]

[[channels]]
id = "kakao"
type = "kakao_talk"

[workspace]
default = "home"
live_index = true

[[workspace.roots]]
alias = "home"
path = "/Users/jinto/Documents/kittypaw/jinto"
access = "read_write"
`

	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if cfg.Version != 2 {
		t.Fatalf("Version = %d, want 2", cfg.Version)
	}
	if !cfg.IsSharedAccount() {
		t.Fatal("is_shared=true must mark account as shared")
	}
	if cfg.LLM.Default != "main" || cfg.LLM.Fallback != "backup" {
		t.Fatalf("LLM default/fallback = %q/%q", cfg.LLM.Default, cfg.LLM.Fallback)
	}
	if got := cfg.DefaultModel(); got == nil || got.ID != "main" || got.Credential != "openai" {
		t.Fatalf("DefaultModel = %#v, want main/openai", got)
	}
	if got := cfg.FallbackModel(); got == nil || got.ID != "backup" || got.Credential != "anthropic" {
		t.Fatalf("FallbackModel = %#v, want backup/anthropic", got)
	}
	if len(cfg.Channels) != 2 {
		t.Fatalf("Channels len = %d", len(cfg.Channels))
	}
	if cfg.Channels[0].ID != "telegram" || cfg.Channels[0].ChannelType != ChannelTelegram {
		t.Fatalf("telegram channel = %#v", cfg.Channels[0])
	}
	if got := cfg.Channels[0].AllowedChatIDs; len(got) != 1 || got[0] != "54076829" {
		t.Fatalf("AllowedChatIDs = %v", got)
	}
	if cfg.Channels[1].ID != "kakao" || cfg.Channels[1].ChannelType != ChannelKakaoTalk {
		t.Fatalf("kakao channel = %#v", cfg.Channels[1])
	}
	if cfg.Workspace.Default != "home" {
		t.Fatalf("workspace default = %q", cfg.Workspace.Default)
	}
	if got := cfg.WorkspaceRoots(); len(got) != 1 || got[0].Alias != "home" || got[0].Path == "" {
		t.Fatalf("WorkspaceRoots = %#v", got)
	}
}

func TestTeamSpaceConfigParsing(t *testing.T) {
	tomlContent := `
is_shared = true

[team_space]
members = ["alice", "bob"]
`
	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !cfg.IsTeamSpaceAccount() {
		t.Fatal("is_shared=true must mark account as team space")
	}
	if got := cfg.TeamSpace.Members; len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Fatalf("TeamSpace.Members = %#v, want alice,bob", got)
	}
	if !cfg.TeamSpaceHasMember("alice") {
		t.Fatal("alice must be recognized as a team-space member")
	}
}

func TestTeamSpaceConfigDefaultsDenyAll(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`is_shared = true`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !cfg.IsTeamSpaceAccount() {
		t.Fatal("is_shared=true must mark account as team space")
	}
	if len(cfg.TeamSpace.Members) != 0 {
		t.Fatalf("missing [team_space].members must default empty, got %#v", cfg.TeamSpace.Members)
	}
	if cfg.TeamSpaceHasMember("alice") {
		t.Fatal("empty team-space members must deny all accounts")
	}
}

func TestLegacyShareParsingStillLoads(t *testing.T) {
	tomlContent := `
is_shared = true

[share.alice]
read = ["memory/weather.json"]
`
	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cfg.Share) != 1 || len(cfg.Share["alice"].Read) != 1 {
		t.Fatalf("legacy Share did not parse: %#v", cfg.Share)
	}
	if cfg.TeamSpaceHasMember("alice") {
		t.Fatal("[share.alice] must not imply team-space membership")
	}
}

func TestConfigV2SecretsHydration(t *testing.T) {
	t.Setenv("KITTYPAW_CONFIG_DIR", t.TempDir())
	secrets, err := LoadAccountSecrets("jinto")
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.Set("llm/openai", "api_key", "sk-openai"); err != nil {
		t.Fatal(err)
	}
	if err := secrets.Set("channel/telegram", "bot_token", "tg-token"); err != nil {
		t.Fatal(err)
	}
	if err := secrets.Set("channel/kakao", "ws_url", "wss://kakao.kittypaw.app/ws/token"); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		LLM: LLMConfig{
			Default: "main",
			Models: []ModelConfig{{
				ID:         "main",
				Provider:   "openai",
				Model:      "gpt-5.5",
				Credential: "openai",
			}},
		},
		Channels: []ChannelConfig{
			{ID: "telegram", ChannelType: ChannelTelegram},
			{ID: "kakao", ChannelType: ChannelKakaoTalk},
		},
	}

	model, ok := cfg.RuntimeDefaultModel(secrets)
	if !ok {
		t.Fatal("RuntimeDefaultModel not found")
	}
	if model.APIKey != "sk-openai" {
		t.Fatalf("hydrated model APIKey = %q", model.APIKey)
	}

	InjectChannelSecrets("jinto", cfg.Channels)
	if cfg.Channels[0].Token != "tg-token" {
		t.Fatalf("telegram token = %q", cfg.Channels[0].Token)
	}
	if cfg.Channels[1].KakaoWSURL != "wss://kakao.kittypaw.app/ws/token" {
		t.Fatalf("kakao ws url = %q", cfg.Channels[1].KakaoWSURL)
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
is_shared = true

[share.family]
read = ["memory/weather.json", "memory/household.json"]

[share.alice]
read = ["summary.md"]
`
	var cfg Config
	if _, err := toml.Decode(tomlContent, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !cfg.IsSharedAccount() {
		t.Errorf("IsSharedAccount=true expected")
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
// every existing account breaks at server start.
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
