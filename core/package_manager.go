package core

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// PackageManager handles installation, configuration, and loading of skill packages.
type PackageManager struct {
	baseDir string // if empty, falls back to global ConfigDir
	secrets *SecretsStore
}

// NewPackageManager creates a PackageManager backed by the given secrets store.
// Uses the global ConfigDir for package storage.
func NewPackageManager(secrets *SecretsStore) *PackageManager {
	return &PackageManager{secrets: secrets}
}

// NewPackageManagerFrom creates a PackageManager with an explicit base directory
// for multi-tenant isolation.
func NewPackageManagerFrom(baseDir string, secrets *SecretsStore) *PackageManager {
	return &PackageManager{baseDir: baseDir, secrets: secrets}
}

// packagesDir returns the packages directory, using baseDir if set.
func (pm *PackageManager) packagesDir() (string, error) {
	if pm.baseDir != "" {
		return PackagesDirFrom(pm.baseDir)
	}
	return PackagesDir()
}

// Install validates and copies a package from sourcePath to ~/.kittypaw/packages/<id>/.
// sourcePath must contain a valid package.toml and main.js.
// Rejects symlinks in source files to prevent arbitrary file reads.
func (pm *PackageManager) Install(sourcePath string) (*SkillPackage, error) {
	tomlPath := filepath.Join(sourcePath, "package.toml")

	// Reject symlinks in source files.
	for _, name := range []string{"package.toml", "main.js"} {
		fi, err := os.Lstat(filepath.Join(sourcePath, name))
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("install: %s must not be a symlink", name)
		}
	}

	pkg, err := LoadPackageToml(tomlPath)
	if err != nil {
		return nil, fmt.Errorf("install: %w", err)
	}

	// main.js is required.
	mainJS := filepath.Join(sourcePath, "main.js")
	if _, err := os.Stat(mainJS); err != nil {
		return nil, fmt.Errorf("install: main.js is required but missing in %s", sourcePath)
	}

	pkgDir, err := pm.packagesDir()
	if err != nil {
		return nil, err
	}

	destDir := filepath.Join(pkgDir, pkg.Meta.ID)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("install: create dir: %w", err)
	}

	// Copy package.toml and main.js.
	if err := copyFile(tomlPath, filepath.Join(destDir, "package.toml")); err != nil {
		return nil, fmt.Errorf("install: copy package.toml: %w", err)
	}
	if err := copyFile(mainJS, filepath.Join(destDir, "main.js")); err != nil {
		return nil, fmt.Errorf("install: copy main.js: %w", err)
	}

	// Copy config.toml if it exists.
	configSrc := filepath.Join(sourcePath, "config.toml")
	if _, err := os.Stat(configSrc); err == nil {
		_ = copyFile(configSrc, filepath.Join(destDir, "config.toml"))
	}

	return pkg, nil
}

// InstallFromRegistry downloads a package from a registry and installs it.
// Verifies that the package.toml ID matches the registry entry ID *before*
// installation to prevent a malicious registry from overwriting existing packages.
func (pm *PackageManager) InstallFromRegistry(client *RegistryClient, entry RegistryEntry) (*SkillPackage, error) {
	tmpDir, err := client.DownloadPackage(entry)
	if err != nil {
		return nil, fmt.Errorf("install from registry: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Verify ID match before Install to prevent overwriting unrelated packages.
	tomlPkg, err := LoadPackageToml(filepath.Join(tmpDir, "package.toml"))
	if err != nil {
		return nil, fmt.Errorf("install from registry: %w", err)
	}
	if tomlPkg.Meta.ID != entry.ID {
		return nil, fmt.Errorf("install from registry: package ID mismatch (registry %q, toml %q)", entry.ID, tomlPkg.Meta.ID)
	}

	return pm.Install(tmpDir)
}

// Uninstall removes a package directory and its secrets.
func (pm *PackageManager) Uninstall(id string) error {
	if err := ValidatePackageID(id); err != nil {
		return err
	}

	pkgDir, err := pm.packagesDir()
	if err != nil {
		return err
	}

	dir := filepath.Join(pkgDir, id)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("package %q not installed", id)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("uninstall %q: %w", id, err)
	}

	// Clean up secrets.
	if pm.secrets != nil {
		_ = pm.secrets.DeletePackage(id)
	}

	return nil
}

// ListInstalled returns all installed packages.
func (pm *PackageManager) ListInstalled() ([]SkillPackage, error) {
	pkgDir, err := pm.packagesDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var packages []SkillPackage
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		tomlPath := filepath.Join(pkgDir, entry.Name(), "package.toml")
		pkg, err := LoadPackageToml(tomlPath)
		if err != nil {
			continue
		}
		packages = append(packages, *pkg)
	}
	return packages, nil
}

// LoadPackage loads a single installed package by ID.
func (pm *PackageManager) LoadPackage(id string) (*SkillPackage, string, error) {
	if err := ValidatePackageID(id); err != nil {
		return nil, "", err
	}

	pkgDir, err := pm.packagesDir()
	if err != nil {
		return nil, "", err
	}

	tomlPath := filepath.Join(pkgDir, id, "package.toml")
	pkg, err := LoadPackageToml(tomlPath)
	if err != nil {
		return nil, "", err
	}

	jsPath := filepath.Join(pkgDir, id, "main.js")
	js, err := os.ReadFile(jsPath)
	if err != nil {
		return nil, "", fmt.Errorf("load package %q: read main.js: %w", id, err)
	}

	return pkg, string(js), nil
}

// GetConfig resolves all config values for a package, substituting secrets
// for fields marked as secret.
func (pm *PackageManager) GetConfig(id string) (map[string]string, error) {
	pkg, _, err := pm.LoadPackage(id)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)

	// Load package-local config.toml if it exists.
	pkgDir, _ := pm.packagesDir()
	localConfig := make(map[string]string)
	configPath := filepath.Join(pkgDir, id, "config.toml")
	if data, err := os.ReadFile(configPath); err == nil {
		_ = toml.Unmarshal(data, &localConfig)
	}

	for _, field := range pkg.Config {
		if field.Secret && pm.secrets != nil {
			if val, ok := pm.secrets.Get(id, field.Key); ok {
				result[field.Key] = val
				continue
			}
		}
		if val, ok := localConfig[field.Key]; ok {
			result[field.Key] = val
			continue
		}
		result[field.Key] = field.Default
	}

	return result, nil
}

// SetConfig sets a config value for a package. Secret fields are routed to
// secrets.json; non-secret fields are stored in the package's config.toml.
func (pm *PackageManager) SetConfig(id, key, value string) error {
	pkg, _, err := pm.LoadPackage(id)
	if err != nil {
		return err
	}

	// Find the config field definition.
	var field *ConfigField
	for i := range pkg.Config {
		if pkg.Config[i].Key == key {
			field = &pkg.Config[i]
			break
		}
	}
	if field == nil {
		return fmt.Errorf("package %q has no config field %q", id, key)
	}

	if field.Secret {
		if pm.secrets == nil {
			return fmt.Errorf("secrets store not available")
		}
		return pm.secrets.Set(id, key, value)
	}

	// Non-secret: write to config.toml.
	pkgDir, err := pm.packagesDir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(pkgDir, id, "config.toml")
	existing := make(map[string]string)
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		_ = toml.Unmarshal(data, &existing)
	}
	existing[key] = value

	f, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(existing)
}

// LoadChain loads the chain of packages for a given package.
// Returns the packages in execution order. An error is returned if any
// chain step references a package that is not installed.
func (pm *PackageManager) LoadChain(pkg *SkillPackage) ([]ChainPackage, error) {
	if len(pkg.Chain) == 0 {
		return nil, nil
	}

	var chain []ChainPackage
	for _, step := range pkg.Chain {
		stepPkg, code, err := pm.LoadPackage(step.PackageID)
		if err != nil {
			return nil, fmt.Errorf("chain step %q: %w", step.PackageID, err)
		}
		chain = append(chain, ChainPackage{
			Package: *stepPkg,
			Code:    code,
			Model:   step.Model,
		})
	}
	return chain, nil
}

// ChainPackage bundles a loaded chain step with its code and optional model override.
type ChainPackage struct {
	Package SkillPackage
	Code    string
	Model   string // override from chain step definition
}
