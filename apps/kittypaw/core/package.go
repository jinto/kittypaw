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
	Meta         PackageMeta         `toml:"meta"`
	Config       []ConfigField       `toml:"config"`
	Chain        []ChainStep         `toml:"chain"`
	Discovery    PackageDiscovery    `toml:"discovery"`
	Capabilities PackageCapabilities `toml:"capabilities"`
	Invocation   PackageInvocation   `toml:"invocation"`
	Attribution  PackageAttribution  `toml:"attribution"`
	Permissions  PackagePermissions  `toml:"permissions"`
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
	"string": true, "number": true, "boolean": true, "secret": true, "select": true, "cron": true,
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

// RequiresAPILogin reports whether any config field sources from the
// kittypaw-api namespace. Such packages need a valid API session to run;
// installers warn on this and the executor refuses to run them without login.
func (p *SkillPackage) RequiresAPILogin() bool {
	for _, f := range p.Config {
		if strings.HasPrefix(f.Source, "kittypaw-api/") {
			return true
		}
	}
	return false
}

// ChainStep defines one step in a multi-package execution chain.
// The first step receives the initial input; subsequent steps receive
// the previous step's output as prev_output.
type ChainStep struct {
	PackageID string `toml:"package_id" json:"package_id"`
	Model     string `toml:"model"      json:"model,omitempty"`
}

// PackageDiscovery describes when a caller should select this package.
// These fields are intended for engine/LLM discovery, not for package JS.
type PackageDiscovery struct {
	TimeScope       string            `toml:"time_scope"       json:"time_scope,omitempty"`
	TriggerExamples []string          `toml:"trigger_examples" json:"trigger_examples,omitempty"`
	AntiExamples    []string          `toml:"anti_examples"    json:"anti_examples,omitempty"`
	DelegatesTo     map[string]string `toml:"delegates_to"     json:"delegates_to,omitempty"`
}

// PackageCapabilities declares structured slots a package can consume and
// where those slots should be resolved.
type PackageCapabilities struct {
	Location LocationCapability `toml:"location" json:"location,omitempty"`
}

// LocationCapability declares how location input is accepted.
type LocationCapability struct {
	Accepts    []string `toml:"accepts"    json:"accepts,omitempty"`
	Resolution string   `toml:"resolution" json:"resolution,omitempty"`
}

// PackageInvocation describes the contract the engine/LLM caller must follow
// before and after executing a deterministic package.
type PackageInvocation struct {
	ExecutionModel         string            `toml:"execution_model"          json:"execution_model,omitempty"`
	CallerResponsibilities []string          `toml:"caller_responsibilities"  json:"caller_responsibilities,omitempty"`
	MissingSlotPolicy      string            `toml:"missing_slot_policy"      json:"missing_slot_policy,omitempty"`
	Postprocess            string            `toml:"postprocess"              json:"postprocess,omitempty"`
	Inputs                 []InvocationInput `toml:"inputs"                   json:"inputs,omitempty"`
}

// InvocationInput is one structured input a package can receive.
type InvocationInput struct {
	Key         string   `toml:"key"         json:"key"`
	Path        string   `toml:"path"        json:"path"`
	Type        string   `toml:"type"        json:"type,omitempty"`
	Required    bool     `toml:"required"    json:"required"`
	Resolver    string   `toml:"resolver"    json:"resolver,omitempty"`
	Fields      []string `toml:"fields"      json:"fields,omitempty"`
	Description string   `toml:"description" json:"description,omitempty"`
}

// PackageAttribution declares when a package or its upstream providers should
// show user-facing credit. Official packages default to quiet output and render
// attribution only when this metadata or a runtime API payload requires it.
type PackageAttribution struct {
	Policy    string                `toml:"policy"    json:"policy,omitempty"`
	Providers []ProviderAttribution `toml:"providers" json:"providers,omitempty"`
}

// ProviderAttribution describes one upstream data provider's display contract.
type ProviderAttribution struct {
	ID       string `toml:"id"       json:"id"`
	Name     string `toml:"name"     json:"name,omitempty"`
	Label    string `toml:"label"    json:"label,omitempty"`
	URL      string `toml:"url"      json:"url,omitempty"`
	Required bool   `toml:"required" json:"required"`
}

// PackagePermissions declares what a package is allowed to do and know.
type PackagePermissions struct {
	Primitives   []string `toml:"primitives"    json:"primitives"`
	AllowedHosts []string `toml:"allowed_hosts" json:"allowed_hosts"`
	// Context lists the user-context fields this package needs (e.g. "locale",
	// "location"). Only declared fields are injected into __context__.user.
	Context []string `toml:"context" json:"context,omitempty"`
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
	Discovery    PackageDiscovery    `toml:"discovery"`
	Capabilities PackageCapabilities `toml:"capabilities"`
	Invocation   PackageInvocation   `toml:"invocation"`
	Attribution  PackageAttribution  `toml:"attribution"`
	Permissions  PackagePermissions  `toml:"permissions"`
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
	parseErr := toml.Unmarshal(data, &pkg)
	if parseErr != nil || pkg.Meta.ID == "" {
		var reg registryPackageFormat
		if err2 := toml.Unmarshal(data, &reg); err2 != nil || reg.Package.ID == "" {
			if parseErr != nil {
				return nil, fmt.Errorf("parse package.toml: %w", parseErr)
			}
		} else {
			// Fall back to registry format.
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
				Config:       reg.Config.Fields,
				Discovery:    reg.Discovery,
				Capabilities: reg.Capabilities,
				Invocation:   reg.Invocation,
				Attribution:  reg.Attribution,
				Permissions:  reg.Permissions,
			}
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
