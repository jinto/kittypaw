package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// SkillPackage represents an installable skill package with metadata, config,
// chain steps, and permissions.
type SkillPackage struct {
	Meta        PackageMeta        `toml:"meta"`
	Config      []ConfigField      `toml:"config"`
	Chain       []ChainStep        `toml:"chain"`
	Permissions PackagePermissions `toml:"permissions"`
}

// PackageMeta holds the core identity of a package.
type PackageMeta struct {
	ID          string `toml:"id"          json:"id"`
	Name        string `toml:"name"        json:"name"`
	Version     string `toml:"version"     json:"version"`
	Description string `toml:"description" json:"description"`
	Author      string `toml:"author"      json:"author"`
	Model       string `toml:"model"       json:"model,omitempty"`
	Cron        string `toml:"cron"        json:"cron,omitempty"`
}

// ConfigField defines a user-configurable parameter for a package.
// Fields marked Secret are stored in secrets.json instead of config.toml.
type ConfigField struct {
	Key         string `toml:"key"         json:"key"`
	Label       string `toml:"label"       json:"label"`
	Default     string `toml:"default"     json:"default,omitempty"`
	Required    bool   `toml:"required"    json:"required"`
	Secret      bool   `toml:"secret"      json:"secret"`
}

// ChainStep defines one step in a multi-package execution chain.
// The first step receives the initial input; subsequent steps receive
// the previous step's output as prev_output.
type ChainStep struct {
	PackageID string `toml:"package_id" json:"package_id"`
	Model     string `toml:"model"      json:"model,omitempty"`
}

// PackagePermissions declares what a package is allowed to do.
type PackagePermissions struct {
	Primitives   []string `toml:"primitives"    json:"primitives"`
	AllowedHosts []string `toml:"allowed_hosts" json:"allowed_hosts"`
	CanDisable   *bool    `toml:"can_disable"   json:"can_disable,omitempty"`
}

// PackagesDir returns the directory where packages are stored, creating it if needed.
func PackagesDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	pkgDir := filepath.Join(dir, "packages")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return "", err
	}
	return pkgDir, nil
}

// ValidatePackageID checks that a package ID contains only safe characters.
// Allowed: lowercase alphanumeric, hyphens, underscores. No path traversal.
func ValidatePackageID(id string) error {
	if id == "" {
		return fmt.Errorf("package ID is empty")
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("package ID contains path traversal characters: %q", id)
	}
	if !validPackageID.MatchString(id) {
		return fmt.Errorf("package ID contains invalid characters: %q (allowed: a-z, 0-9, _, -)", id)
	}
	return nil
}

// validPackageID allows lowercase alphanumeric, hyphens, and underscores.
var validPackageID = regexp.MustCompile(`^[a-z0-9_-]+$`)

// LoadPackageToml reads and parses a package.toml file.
func LoadPackageToml(path string) (*SkillPackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read package.toml: %w", err)
	}

	var pkg SkillPackage
	if err := toml.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse package.toml: %w", err)
	}

	if err := ValidatePackageID(pkg.Meta.ID); err != nil {
		return nil, fmt.Errorf("invalid package: %w", err)
	}
	if pkg.Meta.Name == "" {
		return nil, fmt.Errorf("package meta.name is required")
	}
	if pkg.Meta.Version == "" {
		return nil, fmt.Errorf("package meta.version is required")
	}

	return &pkg, nil
}

// CanDisable returns whether a package can be disabled by the auto-fix system.
// Defaults to false (package-owned skills cannot be auto-disabled).
func (p *SkillPackage) CanDisable() bool {
	if p.Permissions.CanDisable != nil {
		return *p.Permissions.CanDisable
	}
	return false
}
