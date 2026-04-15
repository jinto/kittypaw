package core

import (
	"fmt"
	"log/slog"
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

// Allowed ConfigField type values. Unknown types fall back to "string" with a warning.
var validConfigTypes = map[string]bool{
	"string": true, "number": true, "boolean": true, "secret": true, "select": true,
}

// ConfigField defines a user-configurable parameter for a package.
// Fields marked Secret (or Type=="secret") are stored in secrets.json instead of config.toml.
type ConfigField struct {
	Key      string   `toml:"key"      json:"key"`
	Label    string   `toml:"label"    json:"label"`
	Default  string   `toml:"default"  json:"default,omitempty"`
	Required bool     `toml:"required" json:"required"`
	Secret   bool     `toml:"secret"   json:"secret"`
	Type     string   `toml:"type"     json:"type,omitempty"`
	Options  []string `toml:"options"  json:"options,omitempty"`
	Source   string   `toml:"source"   json:"source,omitempty"` // "namespace/key" ref into shared secrets
}

// ResolvedType returns the effective field type with backward-compatible defaults.
// Empty Type is inferred from the Secret bool. Unknown types fall back to "string".
func (f ConfigField) ResolvedType() string {
	if f.Type == "" {
		if f.Secret {
			return "secret"
		}
		return "string"
	}
	if !validConfigTypes[f.Type] {
		return "string"
	}
	if f.Type == "select" && len(f.Options) == 0 {
		return "string"
	}
	return f.Type
}

// IsSecret returns true if the field value should be stored in the secrets store.
func (f ConfigField) IsSecret() bool {
	return f.Secret || f.Type == "secret"
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
	return PackagesDirFrom(dir)
}

// PackagesDirFrom returns the packages directory under baseDir, creating it if needed.
func PackagesDirFrom(baseDir string) (string, error) {
	pkgDir := filepath.Join(baseDir, "packages")
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

// registryPackageFormat matches the registry's package.toml schema
// ([package] + [[config.fields]]), distinct from the internal format
// ([meta] + [[config]]).
type registryPackageFormat struct {
	Package struct {
		ID          string `toml:"id"`
		Name        string `toml:"name"`
		Version     string `toml:"version"`
		Description string `toml:"description"`
		Author      string `toml:"author"`
		Model       string `toml:"model"`
		Cron        string `toml:"cron"`
	} `toml:"package"`
	Config struct {
		Fields []ConfigField `toml:"fields"`
	} `toml:"config"`
	Trigger struct {
		Type string `toml:"type"`
		Cron string `toml:"cron"`
	} `toml:"trigger"`
	Permissions PackagePermissions `toml:"permissions"`
}

// LoadPackageToml reads and parses a package.toml file.
// Supports both the internal format ([meta] + [[config]]) and the
// registry format ([package] + [[config.fields]]).
func LoadPackageToml(path string) (*SkillPackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read package.toml: %w", err)
	}

	var pkg SkillPackage
	if err := toml.Unmarshal(data, &pkg); err != nil {
		// Fall back to registry format.
		var reg registryPackageFormat
		if err2 := toml.Unmarshal(data, &reg); err2 != nil {
			return nil, fmt.Errorf("parse package.toml: %w", err)
		}
		pkg = SkillPackage{
			Meta: PackageMeta{
				ID:          reg.Package.ID,
				Name:        reg.Package.Name,
				Version:     reg.Package.Version,
				Description: reg.Package.Description,
				Author:      reg.Package.Author,
				Model:       reg.Package.Model,
				Cron:        reg.Trigger.Cron,
			},
			Config:      reg.Config.Fields,
			Permissions: reg.Permissions,
		}
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

	// Validate and warn about config field types.
	for i := range pkg.Config {
		f := &pkg.Config[i]
		if f.Type != "" && !validConfigTypes[f.Type] {
			slog.Warn("unknown config field type, falling back to string",
				"package", pkg.Meta.ID, "field", f.Key, "type", f.Type)
		}
		if f.Type == "select" && len(f.Options) == 0 {
			slog.Warn("select config field has no options, falling back to string",
				"package", pkg.Meta.ID, "field", f.Key)
		}
		// Normalize: Type=="secret" implies Secret routing.
		if f.Type == "secret" {
			f.Secret = true
		}
		// Validate source binding format (must be "namespace/key").
		if f.Source != "" {
			if _, _, ok := strings.Cut(f.Source, "/"); !ok {
				slog.Warn("config field source must be namespace/key format, ignoring",
					"package", pkg.Meta.ID, "field", f.Key, "source", f.Source)
				f.Source = ""
			}
		}
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
