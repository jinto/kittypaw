package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

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

// Get returns the tenant with the given ID, or nil if not found.
func (r *TenantRegistry) Get(id string) *Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tenants[id]
}

// GetOrDefault returns the tenant with the given ID, falling back to the
// default tenant. Returns nil only if the default tenant is also missing.
func (r *TenantRegistry) GetOrDefault(id string) *Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaultID
	}
	if t, ok := r.tenants[id]; ok {
		return t
	}
	return r.tenants[r.defaultID]
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
