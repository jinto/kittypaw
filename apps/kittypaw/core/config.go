package core

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// AutonomyLevel controls how much freedom the agent has.
type AutonomyLevel string

const (
	AutonomyReadonly   AutonomyLevel = "readonly"
	AutonomySupervised AutonomyLevel = "supervised"
	AutonomyFull       AutonomyLevel = "full"
)

// ChannelType identifies a messaging channel backend.
type ChannelType string

const (
	ChannelTelegram  ChannelType = "telegram"
	ChannelSlack     ChannelType = "slack"
	ChannelDiscord   ChannelType = "discord"
	ChannelWeb       ChannelType = "web"
	ChannelDesktop   ChannelType = "desktop"
	ChannelKakaoTalk ChannelType = "kakao_talk"
)

// Config is the top-level application configuration, loaded from TOML.
type Config struct {
	LLM             LLMConfig           `toml:"llm"`
	Sandbox         SandboxConfig       `toml:"sandbox"`
	Agents          []AgentConfig       `toml:"agents"`
	Channels        []ChannelConfig     `toml:"channels"`
	AdminChatIDs    []string            `toml:"admin_chat_ids"`
	FreeformFallback bool              `toml:"freeform_fallback"`
	Models          []ModelConfig       `toml:"models"`
	STT             STTConfig           `toml:"stt"`
	Features        FeatureFlags        `toml:"features"`
	MCPServers      []MCPServerConfig   `toml:"mcp_servers"`
	AutonomyLevel   AutonomyLevel       `toml:"autonomy_level"`
	PairedChatIDs   []string            `toml:"paired_chat_ids"`
	Server          ServerConfig        `toml:"server"`
	Profiles        []ProfileConfig     `toml:"profiles"`
	DefaultProfile  string              `toml:"default_profile"`
	Reflection      ReflectionConfig    `toml:"reflection"`
	Evolution       EvolutionConfig     `toml:"evolution"`
	Orchestration   OrchestrationConfig `toml:"orchestration"`
}

// LLMConfig holds the primary LLM provider settings.
type LLMConfig struct {
	Provider  string `toml:"provider"`
	APIKey    string `toml:"api_key"`
	Model     string `toml:"model"`
	MaxTokens uint32 `toml:"max_tokens"`
}

// ModelConfig defines an additional named model.
type ModelConfig struct {
	Name          string            `toml:"name"`
	Provider      string            `toml:"provider"`
	Model         string            `toml:"model"`
	APIKey        string            `toml:"api_key"`
	MaxTokens     uint32            `toml:"max_tokens"`
	Default       bool              `toml:"default"`
	BaseURL       string            `toml:"base_url"`
	ContextWindow uint32            `toml:"context_window"`
	Tier          *ModelTier        `toml:"tier"`
}

// SandboxConfig controls the JavaScript execution sandbox.
type SandboxConfig struct {
	TimeoutSecs   uint64   `toml:"timeout_secs"`
	MemoryLimitMB uint64   `toml:"memory_limit_mb"`
	AllowedPaths  []string `toml:"allowed_paths"`
	AllowedHosts  []string `toml:"allowed_hosts"`
}

// STTConfig holds speech-to-text settings.
type STTConfig struct {
	Provider string `toml:"provider"`
	APIKey   string `toml:"api_key"`
	Language string `toml:"language"`
}

// FeatureFlags toggles experimental features.
type FeatureFlags struct {
	ProgressiveRetry   bool   `toml:"progressive_retry"`
	ContextCompaction  bool   `toml:"context_compaction"`
	ModelRouting       bool   `toml:"model_routing"`
	BackgroundAgents   bool   `toml:"background_agents"`
	DailyTokenLimit    uint64 `toml:"daily_token_limit"`
}

// ReflectionConfig controls daily pattern analysis.
type ReflectionConfig struct {
	Enabled          bool   `toml:"enabled"`
	Cron             string `toml:"cron"`
	MaxInputChars    uint32 `toml:"max_input_chars"`
	IntentThreshold  uint32 `toml:"intent_threshold"`
	TTLDays          uint32 `toml:"ttl_days"`
	WeeklyReportDay  uint32 `toml:"weekly_report_day"`
}

// EvolutionConfig controls autonomous skill suggestion.
type EvolutionConfig struct {
	Enabled              bool   `toml:"enabled"`
	ObservationThreshold uint32 `toml:"observation_threshold"`
}

// OrchestrationConfig controls multi-agent PM pattern.
type OrchestrationConfig struct {
	Enabled      bool   `toml:"enabled"`
	MaxDepth     uint32 `toml:"max_depth"`
	MaxDelegates uint32 `toml:"max_delegates"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	APIKey         string   `toml:"api_key"`
	AllowedOrigins []string `toml:"allowed_origins"`
}

// MCPServerConfig defines an external MCP tool server.
type MCPServerConfig struct {
	Name    string            `toml:"name"`
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
}

// ChannelConfig defines a messaging channel.
type ChannelConfig struct {
	ChannelType ChannelType        `toml:"channel_type"`
	Token       string             `toml:"token"`
	BindAddr    string             `toml:"bind_addr"`
	Kakao       *KakaoChannelConfig `toml:"kakao"`
}

// KakaoChannelConfig holds Kakao-specific relay settings.
type KakaoChannelConfig struct {
	RelayURL  string `toml:"relay_url"`
	UserToken string `toml:"user_token"`
}

// AgentConfig defines a single agent's behavior.
type AgentConfig struct {
	ID            string            `toml:"id"`
	Name          string            `toml:"name"`
	SystemPrompt  string            `toml:"system_prompt"`
	Channels      []string          `toml:"channels"`
	AllowedSkills []SkillPermission `toml:"allowed_skills"`
}

// SkillPermission controls per-skill access for an agent.
type SkillPermission struct {
	Skill              string   `toml:"skill"`
	Methods            []string `toml:"methods"`
	RateLimitPerMinute uint32   `toml:"rate_limit_per_minute"`
}

// ProfileConfig defines a switchable persona.
type ProfileConfig struct {
	ID       string   `toml:"id"`
	Nick     string   `toml:"nick"`
	Channels []string `toml:"channels"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		LLM: LLMConfig{
			Model:     "claude-sonnet-4-20250514",
			MaxTokens: 4096,
		},
		Sandbox: SandboxConfig{
			TimeoutSecs:   30,
			MemoryLimitMB: 64,
		},
		AutonomyLevel:  AutonomyFull,
		DefaultProfile: "default",
		STT: STTConfig{
			Language: "ko",
		},
		Features: FeatureFlags{
			ProgressiveRetry:  true,
			ContextCompaction: true,
		},
		Reflection: ReflectionConfig{
			Enabled:         true,
			Cron:            "0 0 3 * * *",
			MaxInputChars:   4000,
			IntentThreshold: 3,
			TTLDays:         7,
		},
		Evolution: EvolutionConfig{
			ObservationThreshold: 20,
		},
		Orchestration: OrchestrationConfig{
			MaxDepth:     3,
			MaxDelegates: 5,
		},
	}
}

// LoadConfig reads and parses a TOML config file.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// ConfigDir returns the user's .gopaw config directory, creating it if needed.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".gopaw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ConfigPath returns the default config file path.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// FindAgent returns the agent config matching the given ID, or nil.
func (c *Config) FindAgent(id string) *AgentConfig {
	for i := range c.Agents {
		if c.Agents[i].ID == id {
			return &c.Agents[i]
		}
	}
	return nil
}

// DefaultAgent returns the first agent, or nil if none configured.
func (c *Config) DefaultAgent() *AgentConfig {
	if len(c.Agents) == 0 {
		return nil
	}
	return &c.Agents[0]
}

// FindModel returns the model config matching the given name, or nil.
func (c *Config) FindModel(name string) *ModelConfig {
	for i := range c.Models {
		if c.Models[i].Name == name {
			return &c.Models[i]
		}
	}
	return nil
}

// DefaultModel returns the model marked as default, or the first model.
func (c *Config) DefaultModel() *ModelConfig {
	for i := range c.Models {
		if c.Models[i].Default {
			return &c.Models[i]
		}
	}
	if len(c.Models) > 0 {
		return &c.Models[0]
	}
	return nil
}
