package core

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// validTenantID restricts tenant names to a safe ASCII subset that can never
// traverse the filesystem ("../"), collide under case-insensitive FS, or
// surprise a logging/audit pipeline with unicode. 1-32 chars, lowercase.
// Leading underscore is allowed to accommodate reserved-form IDs like
// `_default_` / `_shared_` for future multi-user support.
var validTenantID = regexp.MustCompile(`^[a-z0-9_][a-z0-9_-]{0,31}$`)

// ValidateTenantID returns nil if id is safe to use as a filesystem
// directory name and as a TenantRouter map key. A TenantID is trusted as
// a privacy boundary, so any CLI/HTTP flow that accepts user-supplied
// IDs must call this before persisting.
func ValidateTenantID(id string) error {
	if !validTenantID.MatchString(id) {
		return fmt.Errorf("invalid tenant id %q: must match %s", id, validTenantID.String())
	}
	return nil
}

// Tenant represents a single user/workspace with isolated data.
type Tenant struct {
	ID      string
	BaseDir string // e.g. ~/.kittypaw/tenants/<id>/
	Config  *Config
}

// DataDir returns the tenant's database directory.
func (t *Tenant) DataDir() string {
	return filepath.Join(t.BaseDir, "data")
}

// SkillsDir returns the tenant's skills directory.
func (t *Tenant) SkillsDir() string {
	return filepath.Join(t.BaseDir, "skills")
}

// ProfilesDir returns the tenant's profiles directory.
func (t *Tenant) ProfilesDir() string {
	return filepath.Join(t.BaseDir, "profiles")
}

// SecretsPath returns the path to the tenant's secrets file.
func (t *Tenant) SecretsPath() string {
	return filepath.Join(t.BaseDir, "secrets.json")
}

// DBPath returns the path to the tenant's SQLite database.
// Migrates legacy gopaw.db → kittypaw.db on first access.
func (t *Tenant) DBPath() string {
	dbDir := filepath.Join(t.BaseDir, "data")
	newPath := filepath.Join(dbDir, "kittypaw.db")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		oldPath := filepath.Join(dbDir, "gopaw.db")
		if _, err := os.Stat(oldPath); err == nil {
			_ = os.Rename(oldPath, newPath)
			// Also migrate WAL and SHM files if they exist.
			_ = os.Rename(oldPath+"-wal", newPath+"-wal")
			_ = os.Rename(oldPath+"-shm", newPath+"-shm")
		}
	}
	return newPath
}

// PackagesDir returns the tenant's npm packages directory.
func (t *Tenant) PackagesDir() string {
	return filepath.Join(t.BaseDir, "packages")
}

// EnsureDirs creates all required directories for the tenant.
func (t *Tenant) EnsureDirs() error {
	dirs := []string{
		t.BaseDir,
		t.DataDir(),
		t.SkillsDir(),
		t.ProfilesDir(),
		t.PackagesDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

// TenantRegistry manages all loaded tenants. Safe for concurrent access.
type TenantRegistry struct {
	mu        sync.RWMutex
	tenants   map[string]*Tenant
	baseDir   string // e.g. ~/.kittypaw/tenants/
	defaultID string
}

// NewTenantRegistry creates a registry rooted at baseDir.
func NewTenantRegistry(baseDir, defaultID string) *TenantRegistry {
	if defaultID == "" {
		defaultID = "default"
	}
	return &TenantRegistry{
		tenants:   make(map[string]*Tenant),
		baseDir:   baseDir,
		defaultID: defaultID,
	}
}

// Register adds a tenant to the registry.
func (r *TenantRegistry) Register(t *Tenant) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tenants[t.ID] = t
}

// Unregister removes a tenant from the registry. Returns true if one was
// present. Used by hot-add rollback when a downstream step fails after
// the registry has already accepted the tenant — we must retract the
// Share.read / Fanout surface so peers cannot resolve a half-bound tenant.
func (r *TenantRegistry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tenants[id]; !ok {
		return false
	}
	delete(r.tenants, id)
	return true
}

// Get returns the tenant with the given ID, or nil if not found.
func (r *TenantRegistry) Get(id string) *Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tenants[id]
}

// List returns all registered tenant IDs.
func (r *TenantRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.tenants))
	for id := range r.tenants {
		ids = append(ids, id)
	}
	return ids
}

// DefaultID returns the configured default tenant ID.
func (r *TenantRegistry) DefaultID() string {
	return r.defaultID
}

// BaseDir returns the tenants root directory.
func (r *TenantRegistry) BaseDir() string {
	return r.baseDir
}

// ValidateTenantChannels fails fast if two tenants declare the same
// Telegram bot token or Kakao relay WebSocket URL. Without this check the
// Telegram long-poll would silently race (one tenant's bot would steal
// updates from another) and the Kakao relay would dual-bind a single
// user account — both scenarios cause hard-to-diagnose message loss.
//
// tenantChannels maps tenantID → channel configs. Returns an aggregated
// error listing every duplicate.
func ValidateTenantChannels(tenantChannels map[string][]ChannelConfig) error {
	telegramSeen := make(map[string]string) // token → owning tenant
	kakaoSeen := make(map[string]string)    // wsURL → owning tenant
	var dupes []string

	// Deterministic iteration order for stable error messages.
	ids := make([]string, 0, len(tenantChannels))
	for id := range tenantChannels {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, tid := range ids {
		for _, cfg := range tenantChannels[tid] {
			switch cfg.ChannelType {
			case ChannelTelegram:
				token := strings.TrimSpace(cfg.Token)
				if token == "" {
					continue
				}
				if prev, ok := telegramSeen[token]; ok {
					dupes = append(dupes, fmt.Sprintf(
						"telegram bot_token collides between tenants %q and %q",
						prev, tid))
					continue
				}
				telegramSeen[token] = tid
			case ChannelKakaoTalk:
				url := strings.TrimSpace(cfg.KakaoWSURL)
				if url == "" {
					continue
				}
				if prev, ok := kakaoSeen[url]; ok {
					dupes = append(dupes, fmt.Sprintf(
						"kakao relay URL collides between tenants %q and %q",
						prev, tid))
					continue
				}
				kakaoSeen[url] = tid
			}
		}
	}

	if len(dupes) == 0 {
		return nil
	}
	return fmt.Errorf("duplicate channel credentials across tenants: %v", dupes)
}

// ChatBelongsToTenant reports whether chatID belongs to the tenant whose
// Config is cfg. A tenant with no AdminChatIDs configured returns true —
// the check is permissive for legacy single-tenant installs and for channels
// like web_chat whose ownership is tracked by SessionID, not chat_id.
//
// When AdminChatIDs is non-empty the check is strict: only IDs in that list
// pass. This is the last line of defense against a compromised bot token or
// a crafted inbound payload that claims TenantID=alice while carrying bob's
// chat_id — a mismatch must never reach the agent loop or it would mix
// tenants' conversation histories in the DB (AC-T7).
func ChatBelongsToTenant(cfg *Config, chatID string) bool {
	if cfg == nil || len(cfg.AdminChatIDs) == 0 {
		return true
	}
	for _, owned := range cfg.AdminChatIDs {
		if owned == chatID {
			return true
		}
	}
	return false
}

// ValidateFamilyTenants fails fast when a tenant marked `is_family=true`
// declares channel configs. Family tenants are coordinators (scheduled
// skills + fanout push); they never own a Telegram/Kakao account of their
// own. A misconfigured `[telegram]` on family would race the real
// personal bot for updates — that race must never boot in the first place.
//
// Pair with ValidateTenantChannels at server startup; this covers the
// family-specific rule that the token/URL collision check cannot see.
func ValidateFamilyTenants(tenants []*Tenant) error {
	var offenders []string
	for _, t := range tenants {
		if t == nil || t.Config == nil || !t.Config.IsFamily {
			continue
		}
		if len(t.Config.Channels) == 0 {
			continue
		}
		types := make([]string, 0, len(t.Config.Channels))
		for _, ch := range t.Config.Channels {
			types = append(types, string(ch.ChannelType))
		}
		offenders = append(offenders, fmt.Sprintf("%s:%v", t.ID, types))
	}
	if len(offenders) == 0 {
		return nil
	}
	return fmt.Errorf("family tenant must not declare channels: %v", offenders)
}

// MigrateLegacyLayout moves a pre-multi-tenant ~/.kittypaw layout into
// tenants/default/ so existing v0.x installs upgrade without manual file
// surgery. It is a one-way, idempotent operation invoked at daemon
// bootstrap.
//
// Detection: legacy layout has config.toml at baseDir AND no tenants/
// subdirectory yet. If tenants/ already exists (even empty) we step
// aside — the user may have scaffolded a tenant manually and we must
// not drop legacy files on top of it.
//
// Moved (tenant-scoped): config.toml, secrets.json, data/, skills/,
// profiles/, packages/. Left in place (server-wide): server.toml,
// daemon.pid, daemon.log, anything else under baseDir.
//
// Atomicity: files are first relocated into a staging directory
// (tenants/.default.staging/), and only once every move succeeds is the
// staging dir renamed to tenants/default/. If any intermediate step
// fails, the caller sees an error, nothing has moved from the user's
// perspective, and the next boot can retry cleanly — the legacy-guard
// (config.toml-at-baseDir) still holds.
func MigrateLegacyLayout(baseDir string) error {
	legacyCfg := filepath.Join(baseDir, "config.toml")
	if _, err := os.Stat(legacyCfg); os.IsNotExist(err) {
		return nil // Fresh install or already migrated.
	} else if err != nil {
		return fmt.Errorf("stat legacy config: %w", err)
	}

	// Clean up any abandoned staging dir from a previous crashed run
	// BEFORE the "tenants/ exists" guard, otherwise we'd wedge
	// permanently — the guard would see tenants/ and bail, but tenants/
	// holds only the half-done staging dir.
	tenantsDir := filepath.Join(baseDir, "tenants")
	stagingDir := filepath.Join(tenantsDir, ".default.staging")
	_ = os.RemoveAll(stagingDir)

	if _, err := os.Stat(tenantsDir); err == nil {
		// tenants/ is non-empty (has real tenant dirs) — step aside.
		// Only the staging path is ours to clean up.
		if isEmptyDir(tenantsDir) {
			_ = os.Remove(tenantsDir)
		} else {
			slog.Warn("legacy config detected but tenants/ is non-empty — skipping migration; move config manually into tenants/default/",
				"legacy_config", legacyCfg, "tenants_dir", tenantsDir)
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat tenants dir: %w", err)
	}

	// Stage into tenants/.default.staging/ so an error mid-flight leaves
	// the user's legacy tree intact.
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}

	for _, name := range []string{"config.toml", "secrets.json", "data", "skills", "profiles", "packages"} {
		src := filepath.Join(baseDir, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			_ = os.RemoveAll(stagingDir)
			return fmt.Errorf("stat %s: %w", src, err)
		}
		dst := filepath.Join(stagingDir, name)
		if err := os.Rename(src, dst); err != nil {
			_ = os.RemoveAll(stagingDir)
			return fmt.Errorf("stage %s → %s: %w", src, dst, err)
		}
	}

	finalDir := filepath.Join(tenantsDir, "default")
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return fmt.Errorf("commit staging → %s: %w", finalDir, err)
	}
	return nil
}

// isEmptyDir returns true if dir exists and contains no entries.
// Used to detect a tenants/ that only ever held our own staging dir.
func isEmptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

// DiscoverTenants scans baseDir for tenant directories, loads their
// configs, and returns Tenant values. It does NOT register them — the
// caller is responsible for bootstrapping (Store, Session, etc.) and
// calling Register.
func DiscoverTenants(baseDir string) ([]*Tenant, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read tenants dir: %w", err)
	}

	var tenants []*Tenant
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		// Reject anything that could traverse the filesystem or collide
		// with a case-insensitive FS — TenantID is a privacy boundary.
		if err := ValidateTenantID(id); err != nil {
			slog.Warn("discover: rejecting unsafe tenant dir name",
				"name", id, "error", err)
			continue
		}
		tenantDir := filepath.Join(baseDir, id)
		cfgPath := filepath.Join(tenantDir, "config.toml")

		cfg, err := LoadConfig(cfgPath)
		if err != nil {
			// Skip tenants with invalid or missing configs.
			continue
		}

		tenants = append(tenants, &Tenant{
			ID:      id,
			BaseDir: tenantDir,
			Config:  cfg,
		})
	}
	return tenants, nil
}
