package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jinto/kittypaw/client"
	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/server"
)

func TestIsTransportDropErr_StringMatches(t *testing.T) {
	cases := []string{
		"EOF",
		"unexpected EOF",
		"write tcp 127.0.0.1:57428->127.0.0.1:3000: write: broken pipe",
		"failed to flush: write: broken pipe",
		"use of closed network connection",
		"read: connection reset by peer",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if !isTransportDropErr(errors.New(msg)) {
				t.Errorf("expected transport-drop classification for %q", msg)
			}
		})
	}
}

func TestIsTransportDropErr_TypedSentinels(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"io.EOF", io.EOF},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF},
		{"syscall.EPIPE", syscall.EPIPE},
		{"syscall.ECONNRESET", syscall.ECONNRESET},
		{"net.ErrClosed", net.ErrClosed},
		{"wrapped io.EOF", fmt.Errorf("read ws msg: %w", io.EOF)},
		{"wrapped EPIPE", fmt.Errorf("write chat msg: %w", syscall.EPIPE)},
		{"deeply wrapped ECONNRESET", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", syscall.ECONNRESET))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !isTransportDropErr(tc.err) {
				t.Errorf("expected transport-drop for %v", tc.err)
			}
		})
	}
}

func TestIsTransportDropErr_RejectsServerSide(t *testing.T) {
	// Server errors carry ErrServerSide via client/ws.go. Even when the
	// embedded message contains "EOF" / "broken pipe" / etc. the silent-
	// reconnect path must not retry — replaying would double-charge the
	// user without healing the underlying server failure.
	cases := []error{
		fmt.Errorf("%w: decode response: unexpected EOF", client.ErrServerSide),
		fmt.Errorf("%w: upstream returned broken pipe", client.ErrServerSide),
		fmt.Errorf("%w: connection reset by peer in tool result", client.ErrServerSide),
		fmt.Errorf("%w: use of closed network connection from skill", client.ErrServerSide),
		client.ErrServerSide,
	}
	for _, err := range cases {
		t.Run(err.Error(), func(t *testing.T) {
			if isTransportDropErr(err) {
				t.Errorf("server-side error %q must NOT classify as transport drop", err)
			}
		})
	}
}

func TestShouldPrintChatSendErrorSuppressesServerSideCallbackErrors(t *testing.T) {
	err := fmt.Errorf("%w: 지금 답변을 만들지 못했어요", client.ErrServerSide)
	if shouldPrintChatSendError(false, err) {
		t.Fatal("server-side errors already shown by OnError should not be printed again")
	}
}

func TestShouldPrintChatSendErrorPrintsTransportErrorsWithoutResult(t *testing.T) {
	err := fmt.Errorf("read ws msg: EOF")
	if !shouldPrintChatSendError(false, err) {
		t.Fatal("transport errors without a result should be printed")
	}
}

func TestIsTransportDropErr_NegativeCases(t *testing.T) {
	cases := []string{
		"daemon failed to start",
		"chat protocol invalid",
		"unauthorized: 401",
		"timeout exceeded",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if isTransportDropErr(errors.New(msg)) {
				t.Errorf("non-transport error %q must not classify as drop", msg)
			}
		})
	}
}

func TestIsTransportDropErr_NilSafe(t *testing.T) {
	if isTransportDropErr(nil) {
		t.Fatal("nil error must not classify as transport drop")
	}
}

func TestResolveCLIAccountExplicit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "alice", "config.toml"))

	got, err := resolveCLIAccount("alice")
	if err != nil || got != "alice" {
		t.Fatalf("resolveCLIAccount explicit = %q, %v; want alice nil", got, err)
	}
	if _, err := resolveCLIAccount("../bad"); err == nil {
		t.Fatal("expected invalid explicit account error")
	}
}

func TestResolveCLIAccountEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "alice", "config.toml"))

	t.Setenv("KITTYPAW_ACCOUNT", "alice")
	got, err := resolveCLIAccount("")
	if err != nil || got != "alice" {
		t.Fatalf("resolveCLIAccount env = %q, %v; want alice nil", got, err)
	}
	t.Setenv("KITTYPAW_ACCOUNT", "../bad")
	if _, err := resolveCLIAccount(""); err == nil {
		t.Fatal("expected invalid env account error")
	}
}

func TestResolveCLIAccountExplicitMissingAccount(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("KITTYPAW_ACCOUNT", "")
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "alice", "config.toml"))

	_, err := resolveCLIAccount("missing")
	if err == nil {
		t.Fatal("expected missing explicit account error")
	}
	for _, want := range []string{"missing", "alice"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestResolveCLIAccountSingleAccount(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("KITTYPAW_ACCOUNT", "")
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "alice", "config.toml"))

	got, err := resolveCLIAccount("")
	if err != nil || got != "alice" {
		t.Fatalf("resolveCLIAccount = %q, %v; want alice nil", got, err)
	}
}

func TestResolveCLIAccountMultipleRequiresExplicit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("KITTYPAW_ACCOUNT", "")
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "alice", "config.toml"))
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "bob", "config.toml"))

	_, err := resolveCLIAccount("")
	if err == nil {
		t.Fatal("expected multiple account error")
	}
	for _, want := range []string{"alice", "bob", "--account", "KITTYPAW_ACCOUNT"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestResolveCLIAccountUsesServerDefaultAccount(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("KITTYPAW_ACCOUNT", "")
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "alice", "config.toml"))
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "bob", "config.toml"))
	if err := os.WriteFile(filepath.Join(root, "server.toml"), []byte("default_account = \"bob\"\n"), 0o600); err != nil {
		t.Fatalf("write server.toml: %v", err)
	}

	got, err := resolveCLIAccount("")
	if err != nil || got != "bob" {
		t.Fatalf("resolveCLIAccount = %q, %v; want bob nil", got, err)
	}
}

func TestResolveCLIAccountNoAccounts(t *testing.T) {
	t.Setenv("KITTYPAW_CONFIG_DIR", t.TempDir())
	t.Setenv("KITTYPAW_ACCOUNT", "")
	if _, err := resolveCLIAccount(""); err == nil {
		t.Fatal("expected no accounts error")
	}
}

func TestPrintAccountContext(t *testing.T) {
	var b strings.Builder
	printAccountContext(&b, "jinto", "kittypaw chat")
	if got, want := b.String(), "Account: jinto\n"; got != want {
		t.Fatalf("account context = %q, want %q", got, want)
	}
}

func TestFormatChatHeaderUsesCompactAccountFirstShape(t *testing.T) {
	got := formatChatHeader("dev-cli", "dev-server", "claude-test", "jinto", []string{"telegram"})
	want := "KittyPaw chat · jinto · claude-test · telegram"
	if got != want {
		t.Fatalf("formatChatHeader = %q, want %q", got, want)
	}
}

func TestSkillResetHintMessagePointsToRightCommands(t *testing.T) {
	got := skillResetHintMessage()
	for _, want := range []string{"kittypaw reset", "kittypaw skill uninstall <name>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("skill reset hint %q missing %q", got, want)
		}
	}
}

func TestDefaultAccountBaseUsesResolvedAccount(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("KITTYPAW_ACCOUNT", "")
	flagAccount = ""
	if err := os.MkdirAll(filepath.Join(root, "accounts", "default"), 0o700); err != nil {
		t.Fatalf("mkdir incomplete default account: %v", err)
	}
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "jinto", "config.toml"))

	got, err := defaultAccountBase()
	if err != nil {
		t.Fatalf("defaultAccountBase: %v", err)
	}
	want := filepath.Join(root, "accounts", "jinto")
	if got != want {
		t.Fatalf("defaultAccountBase = %q, want %q", got, want)
	}
}

func TestOpenStoreUsesResolvedAccountDB(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("KITTYPAW_ACCOUNT", "")
	flagAccount = ""
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "jinto", "config.toml"))

	st, err := openStore()
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	_ = st.Close()

	if _, err := os.Stat(filepath.Join(root, "accounts", "jinto", "data", "kittypaw.db")); err != nil {
		t.Fatalf("expected account db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "kittypaw.db")); !os.IsNotExist(err) {
		t.Fatalf("legacy db should not be created; stat err = %v", err)
	}
}

func TestBootstrapRejectsMissingConfiguredDefaultAccount(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("KITTYPAW_ACCOUNT", "")
	if err := os.WriteFile(filepath.Join(root, "server.toml"), []byte("default_account = \"charlie\"\n"), 0o600); err != nil {
		t.Fatalf("write server.toml: %v", err)
	}
	mustWriteTestConfig(t, filepath.Join(root, "accounts", "alice", "config.toml"))

	_, _, err := bootstrap()
	if err == nil {
		t.Fatal("expected missing configured default_account error")
	}
	for _, want := range []string{"default_account", "charlie", "accounts"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestResolveServeBindUsesServerTomlUnlessFlagChanged(t *testing.T) {
	flagBind = ":3000"
	cmd := newServeCmd()
	got := resolveServeBind(cmd, core.TopLevelServerConfig{Bind: "127.0.0.1:4567"}, nil)
	if got != "127.0.0.1:4567" {
		t.Fatalf("resolveServeBind = %q, want server.toml bind", got)
	}

	if err := cmd.Flags().Set("bind", "127.0.0.1:9999"); err != nil {
		t.Fatalf("set bind: %v", err)
	}
	got = resolveServeBind(cmd, core.TopLevelServerConfig{Bind: "127.0.0.1:4567"}, nil)
	if got != "127.0.0.1:9999" {
		t.Fatalf("resolveServeBind explicit flag = %q, want flag bind", got)
	}
}

func TestResolveServeBindFallsBackToSelectedAccount(t *testing.T) {
	flagBind = ":3000"
	cmd := newServeCmd()
	cfg := core.DefaultConfig()
	cfg.Server.Bind = "127.0.0.1:4567"
	deps := []*server.AccountDeps{
		{Account: &core.Account{ID: "alice", Config: &cfg}},
	}

	got := resolveServeBind(cmd, core.TopLevelServerConfig{MasterAPIKey: "server-key"}, deps)
	if got != "127.0.0.1:4567" {
		t.Fatalf("resolveServeBind = %q, want account bind", got)
	}
}

func TestBootstrapBackfillsMissingAccountAPIKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	t.Setenv("HOME", t.TempDir())

	cfg := core.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "claude-test"
	cfg.Server.APIKey = ""
	cfgPath := filepath.Join(root, "accounts", "alice", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatalf("mkdir account dir: %v", err)
	}
	if err := core.WriteConfigAtomic(&cfg, cfgPath); err != nil {
		t.Fatalf("write account config: %v", err)
	}

	deps, _, err := bootstrap()
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, dep := range deps {
		_ = dep.Close()
	}

	loaded, err := core.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("reload account config: %v", err)
	}
	if loaded.Server.APIKey != "" {
		t.Fatalf("bootstrap wrote server.api_key to config, want secret-only storage")
	}
	secrets, err := core.LoadSecretsFrom(filepath.Join(root, "accounts", "alice", "secrets.json"))
	if err != nil {
		t.Fatalf("load account secrets: %v", err)
	}
	if key, ok := secrets.Get("local-server", "api_key"); !ok || key == "" {
		t.Fatal("bootstrap must backfill local-server api key secret for existing accounts")
	}
}

func TestWaitForProcessExitPollsUntilProcessStops(t *testing.T) {
	oldProcessRunning := processRunning
	oldPollInterval := stopWaitPollInterval
	defer func() {
		processRunning = oldProcessRunning
		stopWaitPollInterval = oldPollInterval
	}()

	calls := 0
	processRunning = func(pid int) bool {
		if pid != 123 {
			t.Fatalf("pid = %d, want 123", pid)
		}
		calls++
		return calls < 3
	}
	stopWaitPollInterval = time.Nanosecond

	if !waitForProcessExit(123, time.Second) {
		t.Fatal("waitForProcessExit returned false before process stopped")
	}
	if calls < 3 {
		t.Fatalf("processRunning called %d times, want at least 3", calls)
	}
}

func mustWriteTestConfig(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	cfg := core.DefaultConfig()
	if err := core.WriteConfigAtomic(&cfg, path); err != nil {
		t.Fatalf("write config %s: %v", path, err)
	}
}
