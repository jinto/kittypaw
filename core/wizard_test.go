package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestResolveLLMConfig_Anthropic(t *testing.T) {
	p, m, u := ResolveLLMConfig("anthropic", "", "")
	if p != "anthropic" {
		t.Errorf("provider = %q, want anthropic", p)
	}
	if m != ClaudeDefaultModel {
		t.Errorf("model = %q, want %q", m, ClaudeDefaultModel)
	}
	if u != "" {
		t.Errorf("baseURL = %q, want empty", u)
	}
}

func TestResolveLLMConfig_OpenRouter(t *testing.T) {
	p, m, u := ResolveLLMConfig("openrouter", "", "")
	if p != "openai" {
		t.Errorf("provider = %q, want openai", p)
	}
	if m != OpenRouterDefaultModel {
		t.Errorf("model = %q, want %q", m, OpenRouterDefaultModel)
	}
	if u != OpenRouterBaseURL {
		t.Errorf("baseURL = %q, want %q", u, OpenRouterBaseURL)
	}
}

func TestResolveLLMConfig_Local(t *testing.T) {
	p, m, u := ResolveLLMConfig("local", "http://myhost:1234/v1", "llama3")
	if p != "openai" {
		t.Errorf("provider = %q, want openai", p)
	}
	if m != "llama3" {
		t.Errorf("model = %q, want llama3", m)
	}
	want := "http://myhost:1234/v1/chat/completions"
	if u != want {
		t.Errorf("baseURL = %q, want %q", u, want)
	}
}

func TestResolveLLMConfig_LocalDefaultURL(t *testing.T) {
	_, _, u := ResolveLLMConfig("local", "", "phi3")
	want := OllamaDefaultBaseURL + "/chat/completions"
	if u != want {
		t.Errorf("baseURL = %q, want %q", u, want)
	}
}

func TestResolveLLMConfig_LocalFullURL(t *testing.T) {
	// User pastes full URL with /chat/completions — must not double-append.
	_, _, u := ResolveLLMConfig("local", "http://myhost:1234/v1/chat/completions", "llama3")
	want := "http://myhost:1234/v1/chat/completions"
	if u != want {
		t.Errorf("baseURL = %q, want %q", u, want)
	}
}

func TestResolveLLMConfig_Claude(t *testing.T) {
	p, _, _ := ResolveLLMConfig("claude", "", "")
	if p != "anthropic" {
		t.Errorf("provider = %q, want anthropic", p)
	}
}

func TestMergeWizardSettings_LLMAnthropic(t *testing.T) {
	base := DefaultConfig()
	w := WizardResult{
		LLMProvider: "anthropic",
		LLMAPIKey:   "sk-test",
		LLMModel:    "claude-sonnet-4-20250514",
	}
	cfg := MergeWizardSettings(&base, w)
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("LLM.Provider = %q", cfg.LLM.Provider)
	}
	if cfg.LLM.APIKey != "sk-test" {
		t.Errorf("LLM.APIKey = %q", cfg.LLM.APIKey)
	}
	if cfg.LLM.BaseURL != "" {
		t.Errorf("LLM.BaseURL = %q, want empty", cfg.LLM.BaseURL)
	}
	if !cfg.FreeformFallback {
		t.Error("FreeformFallback should be true")
	}
}

func TestMergeWizardSettings_ProviderSwitchClearsAPIKey(t *testing.T) {
	// Regression: switching from Claude to local must clear the API key.
	base := DefaultConfig()
	base.LLM.Provider = "anthropic"
	base.LLM.APIKey = "old-claude-key"
	base.LLM.BaseURL = ""

	w := WizardResult{
		LLMProvider: "openai",
		LLMAPIKey:   "", // local provider → empty key
		LLMModel:    "llama3",
		LLMBaseURL:  "http://localhost:11434/v1/chat/completions",
	}
	cfg := MergeWizardSettings(&base, w)
	if cfg.LLM.APIKey != "" {
		t.Errorf("LLM.APIKey = %q, want empty (stale key should be cleared)", cfg.LLM.APIKey)
	}
}

func TestMergeWizardSettings_LLMOpenRouter(t *testing.T) {
	base := DefaultConfig()
	w := WizardResult{
		LLMProvider: "openai",
		LLMAPIKey:   "or-key",
		LLMModel:    OpenRouterDefaultModel,
		LLMBaseURL:  OpenRouterBaseURL,
	}
	cfg := MergeWizardSettings(&base, w)
	if cfg.LLM.Provider != "openai" {
		t.Errorf("LLM.Provider = %q", cfg.LLM.Provider)
	}
	if cfg.LLM.BaseURL != OpenRouterBaseURL {
		t.Errorf("LLM.BaseURL = %q", cfg.LLM.BaseURL)
	}
}

func TestMergeWizardSettings_Telegram(t *testing.T) {
	base := DefaultConfig()
	base.Channels = []ChannelConfig{
		{ChannelType: ChannelWeb},
	}
	w := WizardResult{
		TelegramBotToken: "123:abc",
		TelegramChatID:   "99999",
	}
	cfg := MergeWizardSettings(&base, w)

	if len(cfg.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(cfg.Channels))
	}
	// Web channel preserved.
	if cfg.Channels[0].ChannelType != ChannelWeb {
		t.Errorf("channels[0] = %q, want web", cfg.Channels[0].ChannelType)
	}
	// Telegram added.
	if cfg.Channels[1].ChannelType != ChannelTelegram {
		t.Errorf("channels[1] = %q, want telegram", cfg.Channels[1].ChannelType)
	}
	if cfg.Channels[1].Token != "123:abc" {
		t.Errorf("telegram token = %q", cfg.Channels[1].Token)
	}
	if len(cfg.AdminChatIDs) != 1 || cfg.AdminChatIDs[0] != "99999" {
		t.Errorf("AdminChatIDs = %v", cfg.AdminChatIDs)
	}
}

func TestMergeWizardSettings_TelegramReplaces(t *testing.T) {
	base := DefaultConfig()
	base.Channels = []ChannelConfig{
		{ChannelType: ChannelTelegram, Token: "old-token"},
	}
	w := WizardResult{TelegramBotToken: "new-token"}
	cfg := MergeWizardSettings(&base, w)

	if len(cfg.Channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(cfg.Channels))
	}
	if cfg.Channels[0].Token != "new-token" {
		t.Errorf("token = %q, want new-token", cfg.Channels[0].Token)
	}
}

func TestMergeWizardSettings_EmptyPreservesExisting(t *testing.T) {
	base := DefaultConfig()
	base.LLM.Provider = "anthropic"
	base.LLM.APIKey = "existing-key"
	base.LLM.Model = "existing-model"
	base.LLM.BaseURL = "existing-url"
	base.Sandbox.AllowedPaths = []string{"/old/path"}

	w := WizardResult{} // all zeros
	cfg := MergeWizardSettings(&base, w)

	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("LLM.Provider = %q, want anthropic", cfg.LLM.Provider)
	}
	if cfg.LLM.APIKey != "existing-key" {
		t.Errorf("LLM.APIKey = %q", cfg.LLM.APIKey)
	}
	if cfg.LLM.Model != "existing-model" {
		t.Errorf("LLM.Model = %q", cfg.LLM.Model)
	}
	// BaseURL preserved when provider not set in wizard.
	if cfg.LLM.BaseURL != "existing-url" {
		t.Errorf("LLM.BaseURL = %q, want existing-url", cfg.LLM.BaseURL)
	}
	if len(cfg.Sandbox.AllowedPaths) != 1 || cfg.Sandbox.AllowedPaths[0] != "/old/path" {
		t.Errorf("AllowedPaths = %v", cfg.Sandbox.AllowedPaths)
	}
}

func TestMergeWizardSettings_Workspace(t *testing.T) {
	base := DefaultConfig()
	w := WizardResult{WorkspacePath: "/home/user/projects"}
	cfg := MergeWizardSettings(&base, w)

	if len(cfg.Sandbox.AllowedPaths) != 1 || cfg.Sandbox.AllowedPaths[0] != "/home/user/projects" {
		t.Errorf("AllowedPaths = %v", cfg.Sandbox.AllowedPaths)
	}
}

func TestMergeWizardSettings_AllowedHostsNonNil(t *testing.T) {
	base := DefaultConfig()
	base.Sandbox.AllowedHosts = nil
	w := WizardResult{}
	cfg := MergeWizardSettings(&base, w)
	if cfg.Sandbox.AllowedHosts == nil {
		t.Error("AllowedHosts should not be nil")
	}
}

func TestWriteConfigAtomic(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	cfg := DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "test-key"

	if err := WriteConfigAtomic(&cfg, cfgPath); err != nil {
		t.Fatalf("WriteConfigAtomic: %v", err)
	}

	// Verify file exists and is readable TOML.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var loaded Config
	if err := toml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if loaded.LLM.Provider != "anthropic" {
		t.Errorf("loaded provider = %q", loaded.LLM.Provider)
	}
	if loaded.LLM.APIKey != "test-key" {
		t.Errorf("loaded api_key = %q", loaded.LLM.APIKey)
	}

	// Verify permissions.
	info, _ := os.Stat(cfgPath)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}

	// Verify no tmp file left behind.
	tmpPath := cfgPath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful write")
	}
}

func TestWriteConfigAtomic_NoPartialOnFailure(t *testing.T) {
	// Write to a non-existent directory should fail.
	cfgPath := filepath.Join(t.TempDir(), "nonexistent", "config.toml")
	cfg := DefaultConfig()
	err := WriteConfigAtomic(&cfg, cfgPath)
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
	if !strings.Contains(err.Error(), "write tmp config") {
		t.Errorf("unexpected error: %v", err)
	}
}
