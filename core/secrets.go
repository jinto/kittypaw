package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SecretsStore manages per-package secrets in ~/.kittypaw/secrets.json.
// Secrets are stored as plain JSON with 0600 file permissions to keep
// them out of package-level config.toml files that might be shared.
type SecretsStore struct {
	path string
	mu   sync.RWMutex
	data map[string]map[string]string // package_id → key → value
}

// LoadSecrets reads the secrets file from the default path.
// Returns an empty store if the file does not exist.
func LoadSecrets() (*SecretsStore, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	return LoadSecretsFrom(filepath.Join(dir, "secrets.json"))
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

	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("parse secrets: %w", err)
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

