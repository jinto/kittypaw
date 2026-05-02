package main

import (
	"bufio"
	"fmt"
	"io"

	"github.com/jinto/kittypaw/client"
)

// User-facing strings for the setup → chat auto-entry flow. Pinned by
// TestSetupStrings_Golden (AC-STRINGS) — the wording is referenced by the
// spec/plan and downstream docs, so an incidental rewording must be a
// deliberate test update, not a silent drift.
const (
	// Base prompt (no hint) — promptYesNo appends " (Y/n): " itself.
	setupPromptAutoChat       = "> 지금 바로 대화를 시작할까요?"
	setupPromptInstallService = "> 서버가 자동으로 실행되게 할까요?"
	setupMsgReloaded          = "✓ 서버 설정 재적용"
	setupMsgServerOff         = "다음 단계: 'kittypaw server start' 로 서버를 시작하거나 'kittypaw chat' 이 자동으로 기동합니다."
	setupMsgReloadFailedFmt   = "경고: 서버 reload 실패: %v — 'kittypaw server stop && kittypaw server start' 로 재시작하세요."
	setupMsgAutoChatBlocked   = "자동 채팅 진입을 건너뜁니다 — 현재 서버가 이전 설정을 그대로 쓰고 있습니다. 재시작 후 'kittypaw chat' 으로 다시 시도하세요."
	setupMsgServiceInstalled  = "✓ 서비스 등록 완료 — 'kittypaw server status' 로 상태 확인"
	setupMsgServiceSkipped    = "서비스 등록을 건너뜁니다. 나중에 'kittypaw server install' 로 등록할 수 있습니다."
	setupMsgServiceFailedFmt  = "서비스 등록 실패: %v"
)

// serviceInstallEligible decides whether runSetup should offer the inline
// service-install prompt. Mirrors autoChatEligible's truth table: requires
// interactive TTY on both sides, --provider not set (CI path), and the
// user not explicitly opting out with --no-service.
func serviceInstallEligible(f setupFlags, stdinIsTTY, stdoutIsTTY bool) bool {
	if f.provider != "" || f.noService {
		return false
	}
	if !serverServiceSupported() {
		return false
	}
	return stdinIsTTY && stdoutIsTTY
}

// maybeInstallService asks the user whether to register the server with the
// platform init system. Returns true on successful install so callers can
// adjust downstream messaging. A failure — including a port conflict — is
// surfaced on stderr but is non-fatal: the chat auto-entry should still
// proceed because setup itself succeeded.
func maybeInstallService(scanner *bufio.Scanner, stdout, stderr io.Writer) bool {
	if !serverServiceSupported() {
		return false
	}
	if !promptYesNo(scanner, setupPromptInstallService, true) {
		_, _ = fmt.Fprintln(stdout, setupMsgServiceSkipped)
		return false
	}
	if err := installServerServiceFromSetup(stdout, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, setupMsgServiceFailedFmt+"\n", err)
		return false
	}
	_, _ = fmt.Fprintln(stdout, setupMsgServiceInstalled)
	return true
}

// autoChatEligible decides whether `kittypaw setup` should offer the inline
// prompt that enters `kittypaw chat` directly. All four conditions must hold
// (AC-1 truth table):
//
//   - stdin is a TTY (we need to read the y/n answer),
//   - stdout is a TTY (readline needs a real terminal to draw),
//   - --provider flag was not passed (that path is non-interactive / CI),
//   - --no-chat flag was not passed (user opt-out).
func autoChatEligible(f setupFlags, stdinIsTTY, stdoutIsTTY bool) bool {
	if f.provider != "" || f.noChat {
		return false
	}
	return stdinIsTTY && stdoutIsTTY
}

// serverSession is the slice of client.DaemonConn+Client that maybeReloadServer
// needs. The narrow interface keeps unit tests free of real network/PID state.
type serverSession interface {
	IsRunning() bool
	Reload() error
}

// serverDialer resolves a serverSession lazily so callers can defer the
// expensive setup (config load, PID probe) until maybeReloadServer actually
// runs. A dial error is treated as "server off" — we want a hint, not a
// crash, when the config isn't ready.
type serverDialer func() (serverSession, error)

// reloadOutcome tells runSetup whether it's safe to auto-enter `kittypaw chat`
// after setup finishes. Three outcomes, three auto-entry rules:
//
//   - reloadOutcomeServerOff: no live server — the next `kittypaw chat` will
//     spawn one that reads the fresh config, so auto-entry is safe.
//   - reloadOutcomeReloaded:  the running server accepted Reload — its
//     in-memory channel set now matches config.toml, auto-entry is safe.
//   - reloadOutcomeFailed:    the server is live but rejected Reload — chat
//     would attach to a server that still holds the PREVIOUS config (stale
//     LLM key, stale channel tokens). Auto-entry MUST be blocked and the
//     user pointed at a manual restart. Closes the adversarial-review
//     finding where stale-state chat was sent silently.
type reloadOutcome int

const (
	reloadOutcomeServerOff reloadOutcome = iota
	reloadOutcomeReloaded
	reloadOutcomeFailed
)

// maybeReloadServer asks a running server to re-read config and reconcile
// channels so the subsequent `kittypaw chat` REPL connects to a server that
// already sees the new setup. The happy path prints a single success line on
// stdout; the failure paths print recovery hints on stderr. The returned
// outcome decides whether runSetup may auto-enter chat (see reloadOutcome).
// See AC-4 / AC-5 / AC-6.
func maybeReloadServer(dial serverDialer, stdout, stderr io.Writer) reloadOutcome {
	s, err := dial()
	if err != nil || s == nil || !s.IsRunning() {
		_, _ = fmt.Fprintln(stderr, setupMsgServerOff)
		return reloadOutcomeServerOff
	}
	if err := s.Reload(); err != nil {
		_, _ = fmt.Fprintf(stderr, setupMsgReloadFailedFmt+"\n", err)
		return reloadOutcomeFailed
	}
	_, _ = fmt.Fprintln(stdout, setupMsgReloaded)
	return reloadOutcomeReloaded
}

// defaultServerDial wires maybeReloadServer to the production DaemonConn +
// Client pair. Kept as a var so tests can swap in a stub without reaching
// through a whole injection framework.
var defaultServerDial serverDialer = func() (serverSession, error) {
	conn, err := client.NewDaemonConn(flagRemote)
	if err != nil {
		return nil, err
	}
	return &serverSessionAdapter{conn: conn}, nil
}

// serverSessionAdapter bridges a *DaemonConn (IsRunning) + *Client (Reload)
// onto the single serverSession interface. The adapter lazily constructs the
// Client on first Reload call — IsRunning alone doesn't need one.
type serverSessionAdapter struct {
	conn *client.DaemonConn
}

func (a *serverSessionAdapter) IsRunning() bool { return a.conn.IsRunning() }

func (a *serverSessionAdapter) Reload() error {
	cl := client.New(a.conn.BaseURL, a.conn.APIKey)
	_, err := cl.Reload()
	return err
}
