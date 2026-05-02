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

func TestLoadAccountSecrets_CreatesParentDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)

	s, err := LoadAccountSecrets("default")
	if err != nil {
		t.Fatalf("LoadAccountSecrets: %v", err)
	}

	// Parent dir must exist after Load even before any Set.
	accountDir := filepath.Join(root, "accounts", "default")
	info, err := os.Stat(accountDir)
	if err != nil {
		t.Fatalf("account dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("account path is not a directory")
	}

	// First Set after a fresh wipe must succeed (no ENOENT).
	if err := s.Set("kittypaw-api/test", "access_token", "AT"); err != nil {
		t.Fatalf("Set on fresh account store: %v", err)
	}

	// File landed at the per-account path (not global).
	accountSecretsPath := filepath.Join(accountDir, "secrets.json")
	if _, err := os.Stat(accountSecretsPath); err != nil {
		t.Fatalf("per-account secrets file missing: %v", err)
	}
	globalSecretsPath := filepath.Join(root, "secrets.json")
	if _, err := os.Stat(globalSecretsPath); err == nil {
		t.Fatal("global secrets.json must not exist — write should be per-account")
	}
}

// TestSecretsStore_FreshLoadAfterPeerWrite pins the realistic timeline a
// long-lived in-memory cache would otherwise break: process A writes a
// key on disk; process B (which had already loaded the file before A
// wrote) writes a different key. If B holds a stale in-memory map and
// persists it, A's key disappears.
//
// The fix is "open fresh on every Set in long-lived contexts" — see the
// commentary on server/api_setup.go's setup-complete path. This test
// guards that the per-account store, when re-loaded between writes,
// composes correctly even though the underlying *SecretsStore objects
// are different instances.
func TestSecretsStore_FreshLoadAfterPeerWrite(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)

	// Step 1 — peer (e.g. /kakao/register) opens fresh and writes one key.
	peerA, err := LoadAccountSecrets("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := peerA.Set("kittypaw-api/localhost", "kakao_relay_ws_url", "wss://r/abc"); err != nil {
		t.Fatal(err)
	}

	// Step 2 — second caller opens fresh and writes a different key.
	// (This mirrors the post-fix server/api_setup.go pattern: every
	// server-side Set goes through a freshly loaded store.)
	peerB, err := LoadAccountSecrets("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := peerB.Set("kittypaw-api", "api_url", "http://localhost:8080"); err != nil {
		t.Fatal(err)
	}

	// Both keys must survive on disk. A long-lived stale-cache writer
	// (the bug fixed by removing server.Server.secrets) would have
	// erased peerA's write here.
	final, err := LoadAccountSecrets("default")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := final.Get("kittypaw-api/localhost", "kakao_relay_ws_url"); !ok || v != "wss://r/abc" {
		t.Errorf("kakao_relay_ws_url lost after peer write: got %q (ok=%v)", v, ok)
	}
	if v, ok := final.Get("kittypaw-api", "api_url"); !ok || v != "http://localhost:8080" {
		t.Errorf("api_url missing: got %q (ok=%v)", v, ok)
	}
}

func TestLoadAccountSecrets_RejectsInvalidAccountID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)

	bad := []string{"../escape", "../../etc", "/abs", "with spaces", ""}
	for _, id := range bad {
		if _, err := LoadAccountSecrets(id); err == nil {
			t.Errorf("LoadAccountSecrets(%q) accepted hostile id", id)
		}
	}
}

func TestSecretsStore_MultiNamespace_Coexist(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)

	s, err := LoadAccountSecrets("default")
	if err != nil {
		t.Fatal(err)
	}

	// Pre-existing per-account key (e.g. telegram bot token written by account setup).
	if err := s.Set("telegram", "bot_token", "tg-123"); err != nil {
		t.Fatal(err)
	}

	// Login flow writes API token + Kakao URL to the same account store.
	if err := s.Set("kittypaw-api/localhost:8080", "access_token", "AT"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("kittypaw-api", "api_url", "http://localhost:8080"); err != nil {
		t.Fatal(err)
	}

	// All three keys must coexist after the writes.
	s2, err := LoadAccountSecrets("default")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := s2.Get("telegram", "bot_token"); !ok || v != "tg-123" {
		t.Errorf("telegram bot_token clobbered: got %q (ok=%v)", v, ok)
	}
	if v, ok := s2.Get("kittypaw-api/localhost:8080", "access_token"); !ok || v != "AT" {
		t.Errorf("api access_token missing: got %q (ok=%v)", v, ok)
	}
	if v, ok := s2.Get("kittypaw-api", "api_url"); !ok || v != "http://localhost:8080" {
		t.Errorf("bare api_url missing: got %q (ok=%v)", v, ok)
	}
}
