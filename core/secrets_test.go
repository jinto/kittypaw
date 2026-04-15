package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretsStore_CRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")

	s, err := LoadSecretsFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	// Initially empty.
	if _, ok := s.Get("pkg1", "key1"); ok {
		t.Error("expected not found in empty store")
	}

	// Set.
	if err := s.Set("pkg1", "key1", "secret-value"); err != nil {
		t.Fatal(err)
	}
	val, ok := s.Get("pkg1", "key1")
	if !ok || val != "secret-value" {
		t.Errorf("Get = %q, %v", val, ok)
	}

	// Persist and reload.
	s2, err := LoadSecretsFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	val, ok = s2.Get("pkg1", "key1")
	if !ok || val != "secret-value" {
		t.Error("secret not persisted")
	}

	// Delete.
	if err := s.Delete("pkg1", "key1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("pkg1", "key1"); ok {
		t.Error("expected deleted")
	}
}

func TestSecretsStore_DeletePackage(t *testing.T) {
	dir := t.TempDir()
	s, _ := LoadSecretsFrom(filepath.Join(dir, "secrets.json"))

	s.Set("pkg1", "a", "1")
	s.Set("pkg1", "b", "2")
	s.Set("pkg2", "c", "3")

	if err := s.DeletePackage("pkg1"); err != nil {
		t.Fatal(err)
	}

	if _, ok := s.Get("pkg1", "a"); ok {
		t.Error("pkg1 secrets should be deleted")
	}
	if _, ok := s.Get("pkg2", "c"); !ok {
		t.Error("pkg2 should be unaffected")
	}
}

func TestSecretsStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")

	s, _ := LoadSecretsFrom(path)
	s.Set("pkg1", "key1", "val")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestSecretsStore_MaskSecrets(t *testing.T) {
	dir := t.TempDir()
	s, _ := LoadSecretsFrom(filepath.Join(dir, "secrets.json"))

	s.Set("pkg1", "token", "sk-abc123def456")

	text := "Error connecting with token sk-abc123def456 to server"
	masked := s.MaskSecrets(text)
	expected := "Error connecting with token *** to server"
	if masked != expected {
		t.Errorf("masked = %q, want %q", masked, expected)
	}
}

func TestSecretsStore_MaskSecrets_ShortValueIgnored(t *testing.T) {
	dir := t.TempDir()
	s, _ := LoadSecretsFrom(filepath.Join(dir, "secrets.json"))

	s.Set("pkg1", "pin", "12") // too short to mask (< 4 chars)

	text := "PIN is 12 for testing"
	masked := s.MaskSecrets(text)
	if masked != text {
		t.Errorf("short secrets should not be masked: %q", masked)
	}
}

func TestSecretsStore_LoadNonexistent(t *testing.T) {
	s, err := LoadSecretsFrom("/nonexistent/secrets.json")
	if err != nil {
		t.Fatal("should return empty store, not error")
	}
	if _, ok := s.Get("any", "key"); ok {
		t.Error("empty store should return not found")
	}
}

func TestSecretsStore_MixedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")

	// Write mixed format: nested + flat null (migration artifact).
	mixed := []byte(`{
  "telegram": {"bot_token": "tk-123", "chat_id": "999"},
  "telegram/bot_token": null,
  "telegram/chat_id": null,
  "search/api_key": "sk-flat"
}`)
	os.WriteFile(path, mixed, 0o600)

	s, err := LoadSecretsFrom(path)
	if err != nil {
		t.Fatalf("failed to load mixed format: %v", err)
	}

	// Nested values resolve.
	if v, ok := s.Get("telegram", "bot_token"); !ok || v != "tk-123" {
		t.Errorf("telegram/bot_token = %q, want %q", v, "tk-123")
	}
	// Flat string values resolve.
	if v, ok := s.Get("search", "api_key"); !ok || v != "sk-flat" {
		t.Errorf("search/api_key = %q, want %q", v, "sk-flat")
	}

	// File should be auto-migrated to clean canonical format.
	rewritten, _ := os.ReadFile(path)
	if string(rewritten) == string(mixed) {
		t.Error("file should have been rewritten to canonical format")
	}
	// Null keys should be gone.
	if bytes.Contains(rewritten, []byte("null")) {
		t.Error("canonical file should not contain null values")
	}

	// Reload should still work.
	s2, err := LoadSecretsFrom(path)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if v, _ := s2.Get("telegram", "bot_token"); v != "tk-123" {
		t.Errorf("after reload: telegram/bot_token = %q", v)
	}
}
