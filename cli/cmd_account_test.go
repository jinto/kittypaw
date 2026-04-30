package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
)

// Security invariant: stdin > env > flag. A regression would let a hostile env hijack stdin-typed tokens.
func TestResolveAccountToken_StdinPreferred(t *testing.T) {
	t.Setenv(accountEnvBotToken, "env-token")
	f := &accountAddFlags{
		telegramToken:      "flag-token",
		telegramTokenStdin: true,
	}
	var stderr bytes.Buffer

	tok, err := resolveAccountToken(f, strings.NewReader("stdin-token\n"), &stderr)
	if err != nil {
		t.Fatalf("resolveAccountToken: %v", err)
	}
	if tok != "stdin-token" {
		t.Errorf("token = %q, want stdin-token", tok)
	}
}

func TestResolveAccountToken_EnvBeatsFlag(t *testing.T) {
	t.Setenv(accountEnvBotToken, "env-token")
	f := &accountAddFlags{telegramToken: "flag-token"}
	var stderr bytes.Buffer

	tok, err := resolveAccountToken(f, strings.NewReader(""), &stderr)
	if err != nil {
		t.Fatalf("resolveAccountToken: %v", err)
	}
	if tok != "env-token" {
		t.Errorf("token = %q, want env-token", tok)
	}
	if !strings.Contains(stderr.String(), "ignored") {
		t.Errorf("expected shadowing warning, stderr = %q", stderr.String())
	}
}

// Silent flag path would train users into the ps-exposed habit.
func TestResolveAccountToken_FlagWarnsVisible(t *testing.T) {
	t.Setenv(accountEnvBotToken, "")
	f := &accountAddFlags{telegramToken: "flag-token"}
	var stderr bytes.Buffer

	tok, err := resolveAccountToken(f, strings.NewReader(""), &stderr)
	if err != nil {
		t.Fatalf("resolveAccountToken: %v", err)
	}
	if tok != "flag-token" {
		t.Errorf("token = %q, want flag-token", tok)
	}
	if !strings.Contains(stderr.String(), "process list") {
		t.Errorf("expected process-list warning, stderr = %q", stderr.String())
	}
}

// Silent accept would provision an account with an empty token — passes validation, fails at runtime.
func TestResolveAccountToken_StdinEmpty(t *testing.T) {
	f := &accountAddFlags{telegramTokenStdin: true}
	var stderr bytes.Buffer

	_, err := resolveAccountToken(f, strings.NewReader("   \n"), &stderr)
	if err == nil {
		t.Fatal("expected error for empty stdin, got nil")
	}
}

func TestNewAccountAddCmd_RegistersPasswordStdinFlag(t *testing.T) {
	cmd := newAccountAddCmd()
	if f := cmd.Flags().Lookup("password-stdin"); f == nil {
		t.Fatal("--password-stdin flag not registered on `kittypaw account add`")
	}
}

func TestAccountAddCreatesAuthUser(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	f := &accountAddFlags{
		isFamily:      true,
		passwordStdin: true,
		noActivate:    true,
	}
	if err := runAccountAdd("alice", f, strings.NewReader("pw123\n"), &stdout, &stderr); err != nil {
		t.Fatalf("runAccountAdd: %v", err)
	}
	if ok, err := core.NewLocalAuthStore(filepath.Join(root, "auth.json")).VerifyPassword("alice", "pw123"); err != nil || !ok {
		t.Fatalf("VerifyPassword = %v, %v; want true nil", ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, "accounts", "alice", "config.toml")); err != nil {
		t.Fatalf("account config missing: %v", err)
	}
}

func TestAccountAddTokenAndPasswordStdin(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	f := &accountAddFlags{
		telegramTokenStdin: true,
		adminChatID:        "111",
		passwordStdin:      true,
		noActivate:         true,
	}
	stdin := strings.NewReader("12345:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij\npw123\n")
	if err := runAccountAdd("alice", f, stdin, &stdout, &stderr); err != nil {
		t.Fatalf("runAccountAdd: %v", err)
	}
	auth := core.NewLocalAuthStore(filepath.Join(root, "auth.json"))
	if ok, err := auth.VerifyPassword("alice", "pw123"); err != nil || !ok {
		t.Fatalf("VerifyPassword = %v, %v; want true nil", ok, err)
	}
}

func TestAccountAddAuthRollback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("HOME", t.TempDir())
	auth := core.NewLocalAuthStore(filepath.Join(root, "auth.json"))
	if err := auth.CreateUser("alice", "existing"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	var stdout, stderr bytes.Buffer
	f := &accountAddFlags{
		isFamily:      true,
		passwordStdin: true,
		noActivate:    true,
	}
	err := runAccountAdd("alice", f, strings.NewReader("new-password\n"), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected duplicate auth user error")
	}
	if _, statErr := os.Stat(filepath.Join(root, "accounts", "alice")); !os.IsNotExist(statErr) {
		t.Fatalf("account dir should not be committed on auth duplicate, stat err=%v", statErr)
	}
	if ok, err := auth.VerifyPassword("alice", "existing"); err != nil || !ok {
		t.Fatalf("existing password should remain valid, got %v, %v", ok, err)
	}
}

// admin-chat-id is supplied so FetchTelegramChatID is skipped; tests must not hit the network.
func TestRunAccountAdd_HappyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	f := &accountAddFlags{
		telegramToken:      "12345:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
		telegramTokenStdin: false,
		adminChatID:        "111",
		passwordStdin:      true,
	}
	if err := runAccountAdd("alice", f, strings.NewReader("pw123\n"), &stdout, &stderr); err != nil {
		t.Fatalf("runAccountAdd: %v", err)
	}

	accountDir := filepath.Join(home, ".kittypaw", "accounts", "alice")
	if info, err := os.Stat(accountDir); err != nil || !info.IsDir() {
		t.Errorf("account dir missing: err=%v", err)
	}
	if !strings.Contains(stdout.String(), "alice") {
		t.Errorf("stdout should confirm account name, got %q", stdout.String())
	}
	// No daemon running → fallback hint should surface; exact phrasing
	// may shift, but the operator must see a recovery path.
	if !strings.Contains(stdout.String(), "kittypaw serve") {
		t.Errorf("stdout should mention how to activate, got %q", stdout.String())
	}
}

func TestRunAccountAdd_Family(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	f := &accountAddFlags{isFamily: true, passwordStdin: true}
	if err := runAccountAdd("family", f, strings.NewReader("pw123\n"), &stdout, &stderr); err != nil {
		t.Fatalf("runAccountAdd family: %v", err)
	}

	accountDir := filepath.Join(home, ".kittypaw", "accounts", "family")
	if _, err := os.Stat(filepath.Join(accountDir, "config.toml")); err != nil {
		t.Errorf("family config.toml missing: %v", err)
	}
}

// Most common mistake: omitting both --is-family and any token source.
func TestRunAccountAdd_NoTokenRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	err := runAccountAdd("charlie", &accountAddFlags{}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing token, got nil")
	}
	if !strings.Contains(err.Error(), "Telegram bot token is required") {
		t.Errorf("error should explain missing token: %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(home, ".kittypaw", "accounts", "charlie")); !os.IsNotExist(statErr) {
		t.Errorf("no account dir should be created on validation failure")
	}
}

// Accepting malformed tokens would defer the failure to the first getUpdates — worse error surface.
func TestRunAccountAdd_InvalidTokenFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	f := &accountAddFlags{
		telegramToken: "not-a-real-token",
		adminChatID:   "111",
	}
	err := runAccountAdd("alice", f, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected invalid-format error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid telegram bot token") {
		t.Errorf("error should name the field: %q", err.Error())
	}
}

// --- account remove ---

// Shared daemon-down fixture: HOME override means client.NewDaemonConn
// reads an empty secrets tree → IsRunning() returns false → CLI takes the
// offline path (no RPC attempt, filesystem mutations still happen).
func setupRemoveFixture(t *testing.T, members map[string]bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	accountsDir := filepath.Join(home, ".kittypaw", "accounts")
	for name, isFamily := range members {
		dir := filepath.Join(accountsDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		cfg := "[llm]\nprovider = \"anthropic\"\n"
		if isFamily {
			cfg = "is_family = true\n" + cfg + "\n[share.alice]\nread = [\"pub/index.txt\"]\n[share.bob]\nread = [\"pub/index.txt\"]\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o640); err != nil {
			t.Fatalf("write config for %s: %v", name, err)
		}
	}
	return home
}

func TestRunAccountRemove_AccountNotFound(t *testing.T) {
	setupRemoveFixture(t, map[string]bool{"alice": false})
	var stdout, stderr bytes.Buffer
	err := runAccountRemove("zzz", &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want account-not-found error, got %v", err)
	}
}

// AC-RM2: no daemon → no RPC, filesystem mutations still run.
func TestRunAccountRemove_DaemonDown_OfflinePath(t *testing.T) {
	home := setupRemoveFixture(t, map[string]bool{"alice": false})
	accountDir := filepath.Join(home, ".kittypaw", "accounts", "alice")
	trashRoot := filepath.Join(home, ".kittypaw", ".trash")

	var stdout, stderr bytes.Buffer
	if err := runAccountRemove("alice", &stdout, &stderr); err != nil {
		t.Fatalf("runAccountRemove: %v", err)
	}

	if _, err := os.Stat(accountDir); !os.IsNotExist(err) {
		t.Errorf("account dir should be moved out of accounts/: stat err = %v", err)
	}
	entries, err := os.ReadDir(trashRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly 1 trash entry, got %d (err=%v)", len(entries), err)
	}
	if !strings.HasPrefix(entries[0].Name(), "alice-") {
		t.Errorf("trash entry should be alice-<ts>, got %q", entries[0].Name())
	}
	if !strings.Contains(stdout.String(), "skipping hot-deactivation") {
		t.Errorf("stdout should note daemon offline path, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "BotFather") {
		t.Errorf("stderr should warn about BotFather /revoke, got %q", stderr.String())
	}
}

// AC-RM1(d): family/config.toml loses [share.alice], [share.bob] untouched.
func TestRunAccountRemove_FamilyConfigScrub(t *testing.T) {
	home := setupRemoveFixture(t, map[string]bool{
		"alice":  false,
		"bob":    false,
		"family": true,
	})

	var stdout, stderr bytes.Buffer
	if err := runAccountRemove("alice", &stdout, &stderr); err != nil {
		t.Fatalf("runAccountRemove: %v", err)
	}

	famCfg, err := core.LoadConfig(filepath.Join(home, ".kittypaw", "accounts", "family", "config.toml"))
	if err != nil {
		t.Fatalf("reload family config: %v", err)
	}
	if _, ok := famCfg.Share["alice"]; ok {
		t.Error("[share.alice] still in family config after removal")
	}
	if _, ok := famCfg.Share["bob"]; !ok {
		t.Error("[share.bob] should be preserved in family config")
	}
}

// AC-RM4: removing a personal account when no family exists is a no-op
// (no family to scrub), not an error.
func TestRunAccountRemove_NoFamily_NoOp(t *testing.T) {
	home := setupRemoveFixture(t, map[string]bool{"alice": false})
	var stdout, stderr bytes.Buffer
	if err := runAccountRemove("alice", &stdout, &stderr); err != nil {
		t.Fatalf("runAccountRemove: %v", err)
	}
	// Assertion: no panic, no family config magically appears.
	if _, err := os.Stat(filepath.Join(home, ".kittypaw", "accounts", "family")); !os.IsNotExist(err) {
		t.Errorf("family account should not exist, stat err = %v", err)
	}
}

// AC-RM8: two removes inside the same clock second get distinct trash paths.
func TestRunAccountRemove_TrashCollision(t *testing.T) {
	home := setupRemoveFixture(t, map[string]bool{"alice": false, "bob": false})
	trashRoot := filepath.Join(home, ".kittypaw", ".trash")

	// Pre-populate .trash/alice-<now> so the real remove sees a collision.
	if err := os.MkdirAll(trashRoot, 0o700); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}
	stamp := time.Now().UTC().Format("20060102150405")
	preExisting := filepath.Join(trashRoot, "alice-"+stamp)
	if err := os.Mkdir(preExisting, 0o700); err != nil {
		t.Fatalf("seed collision: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := runAccountRemove("alice", &stdout, &stderr); err != nil {
		t.Fatalf("runAccountRemove: %v", err)
	}

	entries, err := os.ReadDir(trashRoot)
	if err != nil {
		t.Fatalf("read trash: %v", err)
	}
	var hasSuffix, hasPre bool
	for _, e := range entries {
		if e.Name() == "alice-"+stamp {
			hasPre = true
		}
		if strings.HasPrefix(e.Name(), "alice-"+stamp+"-") {
			hasSuffix = true
		}
	}
	if !hasPre {
		t.Error("pre-existing trash dir was overwritten")
	}
	if !hasSuffix {
		t.Error("collision suffix (-2, -3, ...) not applied")
	}
}

// AC-RM7: removing the family account itself surfaces an extra warning AND
// skips the scrub step (no "upstream" family to clean).
func TestRunAccountRemove_FamilySelf_ExtraWarning(t *testing.T) {
	home := setupRemoveFixture(t, map[string]bool{"family": true, "alice": false})
	var stdout, stderr bytes.Buffer
	if err := runAccountRemove("family", &stdout, &stderr); err != nil {
		t.Fatalf("runAccountRemove(family): %v", err)
	}

	if !strings.Contains(stderr.String(), "family account removed") {
		t.Errorf("expected extra family-removal warning, stderr = %q", stderr.String())
	}
	// alice's config is untouched (no cascade).
	cfg, err := core.LoadConfig(filepath.Join(home, ".kittypaw", "accounts", "alice", "config.toml"))
	if err != nil {
		t.Fatalf("reload alice config: %v", err)
	}
	_ = cfg
}

// CLI-layer rejection gives a flag-oriented message, not a config-file one.
func TestRunAccountAdd_FamilyWithTokenRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var stdout, stderr bytes.Buffer
	f := &accountAddFlags{
		isFamily:      true,
		telegramToken: "12345:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
		adminChatID:   "111",
	}
	err := runAccountAdd("family", f, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("expected rejection of family+token, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should say mutually exclusive: %q", err.Error())
	}
}
