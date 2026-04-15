package core

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Provider defaults — used by both web and CLI wizards.
const (
	ClaudeDefaultModel     = "claude-sonnet-4-20250514"
	OpenRouterBaseURL      = "https://openrouter.ai/api/v1/chat/completions"
	OpenRouterDefaultModel = "qwen/qwen3-235b-a22b:free"
	OllamaDefaultBaseURL   = "http://localhost:11434/v1"
)

// WizardResult holds all values collected by a setup wizard (CLI or web).
// Zero-value fields mean "not configured / keep existing".
type WizardResult struct {
	// LLM — these are internal (resolved) values, not user-facing provider names.
	// Use ResolveLLMConfig to convert user-facing choices before populating.
	LLMProvider string
	LLMAPIKey   string
	LLMModel    string
	LLMBaseURL  string

	// Telegram
	TelegramBotToken string
	TelegramChatID   string

	// KakaoTalk
	KakaoRelayURL  string
	KakaoUserToken string

	// Web search
	FirecrawlKey string

	// Workspace & permissions
	WorkspacePath string
	HTTPAccess    bool
}

// ResolveLLMConfig converts a user-facing provider choice into internal
// config values (provider, model, baseURL). The caller should populate
// WizardResult with the returned values.
func ResolveLLMConfig(provider, localURL, localModel string) (internalProvider, model, baseURL string) {
	switch strings.ToLower(provider) {
	case "claude", "anthropic":
		return "anthropic", ClaudeDefaultModel, ""
	case "openrouter":
		return "openai", OpenRouterDefaultModel, OpenRouterBaseURL
	case "local", "ollama":
		u := strings.TrimRight(localURL, "/")
		if u == "" {
			u = OllamaDefaultBaseURL
		}
		u = strings.TrimSuffix(u, "/chat/completions")
		return "openai", localModel, u + "/chat/completions"
	default:
		return provider, "", ""
	}
}

// MergeWizardSettings applies wizard results onto an existing config.
// Fields with zero values in WizardResult are left unchanged.
func MergeWizardSettings(existing *Config, w WizardResult) *Config {
	cfg := *existing
	cfg.FreeformFallback = true

	// LLM — when provider is set, apply all LLM fields unconditionally
	// (including empty values) to avoid stale keys when switching providers.
	if w.LLMProvider != "" {
		cfg.LLM.Provider = w.LLMProvider
		cfg.LLM.APIKey = w.LLMAPIKey
		cfg.LLM.BaseURL = w.LLMBaseURL
	}
	if w.LLMModel != "" {
		cfg.LLM.Model = w.LLMModel
	}
	if cfg.LLM.MaxTokens == 0 {
		cfg.LLM.MaxTokens = 4096
	}

	// Channels — only replace wizard-managed types when setup values exist.
	hasTelegram := w.TelegramBotToken != ""
	hasKakao := w.KakaoRelayURL != ""

	var kept []ChannelConfig
	for _, ch := range cfg.Channels {
		if ch.ChannelType == ChannelTelegram && hasTelegram {
			continue
		}
		if ch.ChannelType == ChannelKakaoTalk && hasKakao {
			continue
		}
		kept = append(kept, ch)
	}

	if hasTelegram {
		kept = append(kept, ChannelConfig{
			ChannelType: ChannelTelegram,
			Token:       w.TelegramBotToken,
		})
		if w.TelegramChatID != "" {
			cfg.AdminChatIDs = []string{w.TelegramChatID}
		}
	}

	if hasKakao {
		kept = append(kept, ChannelConfig{
			ChannelType: ChannelKakaoTalk,
			Kakao: &KakaoChannelConfig{
				RelayURL:  w.KakaoRelayURL,
				UserToken: w.KakaoUserToken,
			},
		})
	}

	cfg.Channels = kept

	// Web search backend
	if w.FirecrawlKey != "" {
		cfg.Web.FirecrawlKey = w.FirecrawlKey
		if cfg.Web.SearchBackend == "" || cfg.Web.SearchBackend == "duckduckgo" {
			cfg.Web.SearchBackend = "firecrawl"
		}
	}

	// Sandbox defaults
	if cfg.Sandbox.AllowedHosts == nil {
		cfg.Sandbox.AllowedHosts = []string{}
	}

	// Workspace → sandbox allowed paths
	if w.WorkspacePath != "" {
		cfg.Sandbox.AllowedPaths = []string{w.WorkspacePath}
	}

	return &cfg
}

// WriteConfigAtomic encodes cfg as TOML and writes it to cfgPath
// via a temporary file and atomic rename.
func WriteConfigAtomic(cfg *Config, cfgPath string) error {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	tmpPath := cfgPath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write tmp config: %w", err)
	}
	if err := os.Rename(tmpPath, cfgPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}
