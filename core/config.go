package core

import (
	"fmt"
	"log/slog"
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

// TopLevelServerConfig holds server-wide settings loaded from server.toml.
// This is separate from per-tenant Config — it controls the daemon itself.
type TopLevelServerConfig struct {
	Bind          string `toml:"bind"`
	MasterAPIKey  string `toml:"master_api_key"`
	TenantsDir    string `toml:"tenants_dir"`
	DefaultTenant string `toml:"default_tenant"`
}

// LoadServerConfig reads server.toml from the given path.
func LoadServerConfig(path string) (*TopLevelServerConfig, error) {
	sc := &TopLevelServerConfig{
		DefaultTenant: "default",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sc, nil // defaults
		}
		return nil, fmt.Errorf("read server config: %w", err)
	}
	if err := toml.Unmarshal(data, sc); err != nil {
		return nil, fmt.Errorf("parse server config: %w", err)
	}
	return sc, nil
}

// ServerConfigPath returns the path to server.toml in the kittypaw dir.
func ServerConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "server.toml"), nil
}

// Config is the top-level application configuration, loaded from TOML.
type Config struct {
	LLM              LLMConfig           `toml:"llm"`
	Sandbox          SandboxConfig       `toml:"sandbox"`
	Agents           []AgentConfig       `toml:"agents"`
	Channels         []ChannelConfig     `toml:"channels"`
	AdminChatIDs     []string            `toml:"admin_chat_ids"`
	FreeformFallback bool                `toml:"freeform_fallback"`
	Models           []ModelConfig       `toml:"models"`
	STT              STTConfig           `toml:"stt"`
	Features         FeatureFlags        `toml:"features"`
	MCPServers       []MCPServerConfig   `toml:"mcp_servers"`
	AutonomyLevel    AutonomyLevel       `toml:"autonomy_level"`
	PairedChatIDs    []string            `toml:"paired_chat_ids"`
	Server           ServerConfig        `toml:"server"`
	Profiles         []ProfileConfig     `toml:"profiles"`
	DefaultProfile   string              `toml:"default_profile"`
	Reflection       ReflectionConfig    `toml:"reflection"`
	Evolution        EvolutionConfig     `toml:"evolution"`
	Orchestration    OrchestrationConfig `toml:"orchestration"`
	Registry         RegistryConfig      `toml:"registry"`
	SkillInstall     SkillInstallConfig  `toml:"skill_install"`
	Permissions      PermissionPolicy    `toml:"permissions"`
	Web              WebConfig           `toml:"web"`
	Workspace        WorkspaceConfig     `toml:"workspace"`
	User             UserConfig          `toml:"user"`

	// IsFamily marks a tenant as the shared/coordinator account. Family
	// tenants run scheduled skills on behalf of the household (e.g. weather
	// fetch) and fanout to individual tenants, but MUST NOT own chat
	// channels — a misconfigured [telegram] on family would silently
	// intercept updates meant for alice/bob. Enforced at startup by
	// ValidateFamilyTenants.
	IsFamily bool `toml:"is_family"`

	// Share declares which per-path reads this tenant grants to specific
	// peers, keyed by peer TenantID. Personal tenants list the family
	// tenant as a peer ("alice grants family read access to these paths"),
	// and family tenants likewise list personals. The check always runs
	// against the *owning* tenant's config — alice reading family/x checks
	// family.Share["alice"].Read. Empty map = no sharing (default).
	Share map[string]ShareConfig `toml:"share"`
}

// ShareConfig is the per-peer read allowlist for cross-tenant filesystem
// access. Read paths are tenant-relative (e.g. "memory/weather.json") and
// must match exactly after traversal/symlink validation in ValidateSharedReadPath.
// Write sharing is intentionally absent — the S3-lite scope forbids cross-tenant
// writes so a family skill bug can't corrupt alice's store.
type ShareConfig struct {
	Read []string `toml:"read"`
}

// UserConfig holds user-level settings surfaced to packages via __context__.user.
type UserConfig struct {
	Locale    string  `toml:"locale"`    // e.g. "ko", "en", "ja"
	Timezone  string  `toml:"timezone"`  // e.g. "Asia/Seoul"
	City      string  `toml:"city"`      // e.g. "Seoul"
	Latitude  float64 `toml:"latitude"`  // e.g. 37.57
	Longitude float64 `toml:"longitude"` // e.g. 126.98
}

// WorkspaceConfig controls workspace indexing behavior.
//
// LiveIndex toggles the fsnotify-backed live indexer. When true (default),
// workspace file changes are reflected in the FTS index within one debounce
// window without requiring explicit File.reindex calls. When false, the
// daemon falls back to v1 behavior: index at startup and on explicit
// Reindex only.
type WorkspaceConfig struct {
	LiveIndex bool `toml:"live_index"`
}

// WebConfig controls web tool behavior (search backend, etc.).
type WebConfig struct {
	SearchBackend string `toml:"search_backend"` // "firecrawl" | "tavily" | "duckduckgo"; empty = auto-detect
	FirecrawlKey  string `toml:"firecrawl_api_key"`
	FirecrawlURL  string `toml:"firecrawl_api_url"` // self-hosted; default https://api.firecrawl.dev
	TavilyAPIKey  string `toml:"tavily_api_key"`
}

// SkillInstallConfig controls skill installation behavior.
type SkillInstallConfig struct {
	MdExecutionMode string `toml:"md_execution_mode"` // "prompt" or "native", empty = ask user
}

// PermissionPolicy configures which operations require explicit user approval.
type PermissionPolicy struct {
	RequireApproval []string `toml:"require_approval"`
	TimeoutSeconds  int      `toml:"timeout_seconds"`
}

// DefaultRequireApproval is used when RequireApproval is nil (not configured).
// Skill.installFromRegistry is gated by default — installing a third-party
// skill on the fly is the most common supply-chain attack surface for an
// auto-discovering agent, so the user must always approve the first install.
var DefaultRequireApproval = []string{
	"Shell.exec", "Git.add", "Git.commit", "Git.push", "Git.pull", "File.delete",
	"Skill.installFromRegistry",
}

// LLMConfig holds the primary LLM provider settings.
type LLMConfig struct {
	Provider  string `toml:"provider"`
	APIKey    string `toml:"api_key"`
	Model     string `toml:"model"`
	MaxTokens uint32 `toml:"max_tokens"`
	BaseURL   string `toml:"base_url,omitempty"`
}

// ModelConfig defines an additional named model.
type ModelConfig struct {
	Name          string     `toml:"name"`
	Provider      string     `toml:"provider"`
	Model         string     `toml:"model"`
	APIKey        string     `toml:"api_key"`
	MaxTokens     uint32     `toml:"max_tokens"`
	Default       bool       `toml:"default"`
	BaseURL       string     `toml:"base_url"`
	ContextWindow uint32     `toml:"context_window"`
	Tier          *ModelTier `toml:"tier"`
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
	ProgressiveRetry  bool   `toml:"progressive_retry"`
	ContextCompaction bool   `toml:"context_compaction"`
	ModelRouting      bool   `toml:"model_routing"`
	BackgroundAgents  bool   `toml:"background_agents"`
	DailyTokenLimit   uint64 `toml:"daily_token_limit"`
	MaxObserveRounds  int    `toml:"max_observe_rounds"` // default 5
}

// ReflectionConfig controls daily pattern analysis.
type ReflectionConfig struct {
	Enabled         bool   `toml:"enabled"`
	Cron            string `toml:"cron"`
	MaxInputChars   uint32 `toml:"max_input_chars"`
	IntentThreshold uint32 `toml:"intent_threshold"`
	TTLDays         uint32 `toml:"ttl_days"`
	WeeklyReportDay uint32 `toml:"weekly_report_day"`
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

// RegistryConfig controls the remote package registry.
type RegistryConfig struct {
	URL string `toml:"url"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Bind           string   `toml:"bind"`
	APIKey         string   `toml:"api_key"`
	AllowedOrigins []string `toml:"allowed_origins"`
}

// BindOrDefault returns the configured bind address, defaulting to ":3000".
func (s ServerConfig) BindOrDefault() string {
	if s.Bind != "" {
		return s.Bind
	}
	return ":3000"
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
	ChannelType ChannelType `toml:"channel_type"`
	Token       string      `toml:"token"`
	BindAddr    string      `toml:"bind_addr"`
	KakaoWSURL  string      `toml:"-"` // runtime-injected from secrets (not in config.toml)
}

// InjectKakaoWSURL populates KakaoWSURL on kakao_talk channel configs from
// the named tenant's secrets. Called by ChannelSpawner.Reconcile so
// hot-reload and initial spawn share the same path. No-op if the tenant's
// secrets, api_url, or relay URL are missing.
//
// api_url is written by the setup paths under the bare "kittypaw-api"
// namespace of the tenant's per-tenant secrets store. When absent — e.g.
// the user only completed the KakaoTalk step and skipped API server
// login — fall back to DefaultAPIServerURL so the host-scoped secret
// saved by wizardKakao still resolves.
func InjectKakaoWSURL(tenantID string, channels []ChannelConfig) {
	secrets, err := LoadTenantSecrets(tenantID)
	if err != nil {
		return
	}
	mgr := NewAPITokenManager("", secrets)

	apiURL, ok := secrets.Get("kittypaw-api", "api_url")
	if !ok || apiURL == "" {
		apiURL = DefaultAPIServerURL
	}

	wsURL, ok := mgr.LoadKakaoRelayURL(apiURL)
	if !ok || wsURL == "" {
		return
	}

	for i := range channels {
		if channels[i].ChannelType == ChannelKakaoTalk {
			channels[i].KakaoWSURL = wsURL
		}
	}
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
		Registry: RegistryConfig{
			URL: DefaultRegistryURL,
		},
		Workspace: WorkspaceConfig{
			LiveIndex: true,
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

// ConfigDir returns the user's .kittypaw config directory, creating it if needed.
// On first run after rename, migrates ~/.gopaw → ~/.kittypaw automatically.
// The directory is owned-by-user only (mode 0700) so other OS users on the same
// host cannot read tenant data, skill sources, or secrets. KITTYPAW_CONFIG_DIR
// overrides the default location — set by init-system units (systemd/launchd).
func ConfigDir() (string, error) {
	if dir := os.Getenv("KITTYPAW_CONFIG_DIR"); dir != "" {
		if err := ensureConfigDirMode(dir); err != nil {
			return "", err
		}
		return dir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".kittypaw")

	// Migrate legacy ~/.gopaw if .kittypaw doesn't exist yet.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		oldDir := filepath.Join(home, ".gopaw")
		if _, err := os.Stat(oldDir); err == nil {
			if renameErr := os.Rename(oldDir, dir); renameErr != nil {
				slog.Warn("failed to migrate config dir, using legacy path",
					"from", oldDir, "to", dir, "error", renameErr)
				dir = oldDir
			} else {
				slog.Info("migrated config directory", "from", oldDir, "to", dir)
			}
		}
	}

	if err := ensureConfigDirMode(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// ensureConfigDirMode creates dir if missing and enforces mode 0700 even on
// pre-existing directories left over from earlier versions that used 0755.
func ensureConfigDirMode(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// ResolveBaseDir returns baseDir if non-empty, otherwise falls back to ConfigDir.
func ResolveBaseDir(baseDir string) (string, error) {
	if baseDir != "" {
		return baseDir, nil
	}
	return ConfigDir()
}

// ConfigPath returns the default tenant's config file path
// (~/.kittypaw/tenants/<DefaultTenantID>/config.toml). All CLI commands
// that need to read or write the active config (config check, registry
// resolution, etc.) target this location so daemon-side DiscoverTenants
// and the CLI see the same file. The legacy global path
// (~/.kittypaw/config.toml) is only consulted by MigrateLegacyLayout for
// upgrade paths and is otherwise unused.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tenants", DefaultTenantID, "config.toml"), nil
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
