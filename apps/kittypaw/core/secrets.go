package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SecretsStore manages per-package secrets at a caller-provided path.
// Secrets are stored as plain JSON with 0600 file permissions to keep
// them out of package-level config.toml files that might be shared.
type SecretsStore struct {
	path string
	mu   sync.RWMutex
	data map[string]map[string]string // package_id → key → value
}

// DefaultAccountID is the legacy account ID retained for migration and
// compatibility. New multi-user flows should resolve an account explicitly
// and pass it to helpers such as LoadAccountSecrets.
const DefaultAccountID = "default"

// LoadSecrets reads the global secrets file. Retained for migration
// tooling; production code paths now use LoadAccountSecrets so that
// CLI writes and daemon reads target the same per-account store.
//
// Deprecated: use LoadAccountSecrets. The old "OAuth-once-per-host"
// global model was retired so each account on a shared host can have
// its own credentials.
func LoadSecrets() (*SecretsStore, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	return LoadSecretsFrom(filepath.Join(dir, "secrets.json"))
}

// LoadAccountSecrets returns the SecretsStore for the named account
// (~/.kittypaw/accounts/<accountID>/secrets.json). Creates the parent
// directory (mode 0o700) so the first Set() after a fresh wipe doesn't
// fail with ENOENT. Validates accountID to refuse path traversal; callers
// should pass an account resolved from --account, KITTYPAW_ACCOUNT, or an
// authenticated local Web UI session.
func LoadAccountSecrets(accountID string) (*SecretsStore, error) {
	if err := ValidateAccountID(accountID); err != nil {
		return nil, fmt.Errorf("account id: %w", err)
	}
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	accountDir := filepath.Join(dir, "accounts", accountID)
	if err := os.MkdirAll(accountDir, 0o700); err != nil {
		return nil, fmt.Errorf("create account secrets dir: %w", err)
	}
	return LoadSecretsFrom(filepath.Join(accountDir, "secrets.json"))
}

// LoadSecretsFrom reads secrets from a specific path.
func LoadSecretsFrom(path string) (*SecretsStore, error) {
	s := &SecretsStore{
		path: path,
		data: make(map[string]map[string]string),
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read secrets: %w", err)
	}

	// Handle all formats: pure nested, pure flat, or mixed (migration artifacts).
	var raw2 map[string]json.RawMessage
	if err := json.Unmarshal(raw, &raw2); err != nil {
		return nil, fmt.Errorf("parse secrets: %w", err)
	}
	for k, v := range raw2 {
		// Try as nested map: {"key": "val"}. Skip null (unmarshals to nil map).
		var nested map[string]string
		if json.Unmarshal(v, &nested) == nil && nested != nil {
			s.data[k] = nested
			continue
		}
		// Try as flat string: "val" with compound key "pkg/key".
		var str string
		if json.Unmarshal(v, &str) == nil && str != "" {
			if pkg, key, ok := strings.Cut(k, "/"); ok {
				if s.data[pkg] == nil {
					s.data[pkg] = make(map[string]string)
				}
				s.data[pkg][key] = str
			}
		}
		// null or other types → skip silently.
	}

	// Auto-migrate: if file was non-canonical (flat or mixed), rewrite as clean nested.
	clean, _ := json.MarshalIndent(s.data, "", "  ")
	if !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(clean)) {
		_ = s.persist()
	}

	return s, nil
}

// Get retrieves a secret for a package. Returns ("", false) if not found.
func (s *SecretsStore) Get(packageID, key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pkg, ok := s.data[packageID]
	if !ok {
		return "", false
	}
	val, ok := pkg[key]
	return val, ok
}

// Set stores a secret for a package and persists to disk.
func (s *SecretsStore) Set(packageID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data[packageID] == nil {
		s.data[packageID] = make(map[string]string)
	}
	s.data[packageID][key] = value
	return s.persist()
}

// Delete removes a secret for a package and persists to disk.
func (s *SecretsStore) Delete(packageID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pkg, ok := s.data[packageID]
	if !ok {
		return nil
	}
	delete(pkg, key)
	if len(pkg) == 0 {
		delete(s.data, packageID)
	}
	return s.persist()
}

// DeletePackage removes all secrets for a package and persists to disk.
func (s *SecretsStore) DeletePackage(packageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, packageID)
	return s.persist()
}

// persist writes the current state to disk with 0600 permissions.
// Uses atomic write (temp file + rename) to prevent data loss on crash.
func (s *SecretsStore) persist() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal secrets: %w", err)
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return fmt.Errorf("write temp secrets: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename secrets: %w", err)
	}
	return nil
}

// MaskSecrets replaces any known secret values in a string with "***".
// Use this before logging to prevent secret leakage.
func (s *SecretsStore) MaskSecrets(text string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, pkg := range s.data {
		for _, val := range pkg {
			if val != "" && len(val) >= 4 {
				text = strings.ReplaceAll(text, val, "***")
			}
		}
	}
	return text
}
