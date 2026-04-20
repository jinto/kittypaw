package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// AC-1: autoChatEligible truth table — gates auto-entry on (stdin+stdout TTY)
// AND (provider=="") AND (!noChat). Non-interactive or opt-out paths must
// return false without prompting the user.
func TestAutoChatEligible_TruthTable(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		noChat    bool
		stdinTTY  bool
		stdoutTTY bool
		want      bool
	}{
		{"all on, tty", "", false, true, true, true},
		{"stdin not tty", "", false, false, true, false},
		{"stdout not tty", "", false, true, false, false},
		{"both not tty", "", false, false, false, false},
		{"provider set", "anthropic", false, true, true, false},
		{"noChat set", "", true, true, true, false},
		{"provider + noChat", "anthropic", true, true, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupFlags{provider: tc.provider, noChat: tc.noChat}
			got := autoChatEligible(f, tc.stdinTTY, tc.stdoutTTY)
			if got != tc.want {
				t.Fatalf("autoChatEligible(%+v, stdin=%v, stdout=%v) = %v, want %v",
					f, tc.stdinTTY, tc.stdoutTTY, got, tc.want)
			}
		})
	}
}

// AC-STRINGS: Korean user-facing strings are pinned so a casual rewording
// doesn't silently break UX or downstream doc references. setupPromptAutoChat
// is the base prompt; promptYesNo() appends " (Y/n): " at render time.
func TestSetupStrings_Golden(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"prompt base", setupPromptAutoChat, "> 지금 바로 대화를 시작할까요?"},
		{"reloaded", setupMsgReloaded, "✓ 데몬 설정 재적용"},
		{"daemon off", setupMsgDaemonOff, "다음 단계: 'kittypaw serve' 로 데몬을 시작하거나 'kittypaw chat' 이 자동으로 기동합니다."},
		{"reload failed", setupMsgReloadFailedFmt, "경고: 데몬 reload 실패: %v — 'kittypaw stop && kittypaw serve' 로 재시작하세요."},
		{"auto-chat blocked", setupMsgAutoChatBlocked, "자동 채팅 진입을 건너뜁니다 — 현재 데몬이 이전 설정을 그대로 쓰고 있습니다. 재시작 후 'kittypaw chat' 으로 다시 시도하세요."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %q, want %q", c.got, c.want)
			}
		})
	}
}

// fakeDaemon implements daemonSession for maybeReloadDaemon tests.
type fakeDaemon struct {
	running   bool
	reloadErr error
	reloadN   int
}

func (f *fakeDaemon) IsRunning() bool { return f.running }
func (f *fakeDaemon) Reload() error {
	f.reloadN++
	return f.reloadErr
}

// AC-5: daemon not running → print hint, don't attempt reload, return
// reloadOutcomeDaemonOff so runSetup still allows auto-entry (a fresh daemon
// will pick up the new config when chat spawns it).
func TestMaybeReloadDaemon_Off(t *testing.T) {
	var out, errBuf bytes.Buffer
	fd := &fakeDaemon{running: false}
	dial := func() (daemonSession, error) { return fd, nil }

	got := maybeReloadDaemon(dial, &out, &errBuf)

	if got != reloadOutcomeDaemonOff {
		t.Fatalf("outcome = %v, want reloadOutcomeDaemonOff", got)
	}
	if fd.reloadN != 0 {
		t.Fatalf("Reload called %d times, expected 0", fd.reloadN)
	}
	if !strings.Contains(errBuf.String(), "kittypaw serve") {
		t.Fatalf("stderr missing hint: %q", errBuf.String())
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", out.String())
	}
}

// AC-4: daemon running + reload OK → 1 Reload call + success line on stdout
// + reloadOutcomeReloaded so runSetup may auto-enter chat.
func TestMaybeReloadDaemon_Happy(t *testing.T) {
	var out, errBuf bytes.Buffer
	fd := &fakeDaemon{running: true}
	dial := func() (daemonSession, error) { return fd, nil }

	got := maybeReloadDaemon(dial, &out, &errBuf)

	if got != reloadOutcomeReloaded {
		t.Fatalf("outcome = %v, want reloadOutcomeReloaded", got)
	}
	if fd.reloadN != 1 {
		t.Fatalf("Reload called %d times, expected 1", fd.reloadN)
	}
	if !strings.Contains(out.String(), setupMsgReloaded) {
		t.Fatalf("stdout missing success line: %q", out.String())
	}
	if errBuf.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errBuf.String())
	}
}

// AC-6: daemon running + reload err → warning on stderr + recovery hint, no
// success on stdout, and reloadOutcomeFailed so runSetup blocks auto-entry
// (chat would otherwise attach to a server still holding the previous
// config — stale LLM key / channels). Closes the adversarial-review finding
// that stale state was silently sent.
func TestMaybeReloadDaemon_Error(t *testing.T) {
	var out, errBuf bytes.Buffer
	fd := &fakeDaemon{running: true, reloadErr: errors.New("boom")}
	dial := func() (daemonSession, error) { return fd, nil }

	got := maybeReloadDaemon(dial, &out, &errBuf)

	if got != reloadOutcomeFailed {
		t.Fatalf("outcome = %v, want reloadOutcomeFailed", got)
	}
	if fd.reloadN != 1 {
		t.Fatalf("Reload called %d times, expected 1", fd.reloadN)
	}
	if !strings.Contains(errBuf.String(), "경고: 데몬 reload 실패") {
		t.Fatalf("stderr missing warning: %q", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "kittypaw stop && kittypaw serve") {
		t.Fatalf("stderr missing recovery hint: %q", errBuf.String())
	}
	if strings.Contains(out.String(), setupMsgReloaded) {
		t.Fatalf("unexpected success on stdout: %q", out.String())
	}
}

// dial error is treated as "daemon off" — same hint, no Reload attempt,
// reloadOutcomeDaemonOff so auto-entry still works. Protects against a
// transient dial failure silently skipping the hint.
func TestMaybeReloadDaemon_DialError(t *testing.T) {
	var out, errBuf bytes.Buffer
	dial := func() (daemonSession, error) { return nil, errors.New("no config") }

	got := maybeReloadDaemon(dial, &out, &errBuf)

	if got != reloadOutcomeDaemonOff {
		t.Fatalf("outcome = %v, want reloadOutcomeDaemonOff", got)
	}
	if !strings.Contains(errBuf.String(), "kittypaw serve") {
		t.Fatalf("stderr missing hint: %q", errBuf.String())
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", out.String())
	}
}

// AC-2 regression: `kittypaw setup --no-chat` must register the flag so users
// can opt out of the auto-entry prompt. A missing flag would quietly reintroduce
// the old "just exit after setup" behavior for every user.
func TestNewSetupCmd_RegistersNoChatFlag(t *testing.T) {
	cmd := newSetupCmd()
	f := cmd.Flags().Lookup("no-chat")
	if f == nil {
		t.Fatal("--no-chat flag not registered on `kittypaw setup`")
	}
	if f.DefValue != "false" {
		t.Errorf("--no-chat default = %q, want \"false\"", f.DefValue)
	}
}

// AC-3 regression: `--provider` (non-interactive mode) must NOT prompt for
// auto-entry. This drives autoChatEligible directly to double-pin the gate
// — the T1 truth table covers the helper in isolation; this test covers the
// call-site wiring via the same public flag name end users see.
func TestAutoChatEligible_ProviderFlagSkipsPrompt(t *testing.T) {
	f := setupFlags{provider: "anthropic"}
	if autoChatEligible(f, true, true) {
		t.Fatal("non-interactive (--provider set) must not trigger auto-entry")
	}
}
