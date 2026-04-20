package main

import (
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
	setupPromptAutoChat     = "> 지금 바로 대화를 시작할까요?"
	setupMsgReloaded        = "✓ 데몬 설정 재적용"
	setupMsgDaemonOff       = "다음 단계: 'kittypaw serve' 로 데몬을 시작하거나 'kittypaw chat' 이 자동으로 기동합니다."
	setupMsgReloadFailedFmt = "경고: 데몬 reload 실패: %v — 'kittypaw stop && kittypaw serve' 로 재시작하세요."
	setupMsgAutoChatBlocked = "자동 채팅 진입을 건너뜁니다 — 현재 데몬이 이전 설정을 그대로 쓰고 있습니다. 재시작 후 'kittypaw chat' 으로 다시 시도하세요."
)

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

// daemonSession is the slice of client.DaemonConn+Client that maybeReloadDaemon
// needs. The narrow interface keeps unit tests free of real network/PID state.
type daemonSession interface {
	IsRunning() bool
	Reload() error
}

// daemonDialer resolves a daemonSession lazily so callers can defer the
// expensive setup (config load, PID probe) until maybeReloadDaemon actually
// runs. A dial error is treated as "daemon off" — we want a hint, not a
// crash, when the config isn't ready.
type daemonDialer func() (daemonSession, error)

// reloadOutcome tells runSetup whether it's safe to auto-enter `kittypaw chat`
// after setup finishes. Three outcomes, three auto-entry rules:
//
//   - reloadOutcomeDaemonOff: no live daemon — the next `kittypaw chat` will
//     spawn one that reads the fresh config, so auto-entry is safe.
//   - reloadOutcomeReloaded:  the running daemon accepted Reload — its
//     in-memory channel set now matches config.toml, auto-entry is safe.
//   - reloadOutcomeFailed:    the daemon is live but rejected Reload — chat
//     would attach to a server that still holds the PREVIOUS config (stale
//     LLM key, stale channel tokens). Auto-entry MUST be blocked and the
//     user pointed at a manual restart. Closes the adversarial-review
//     finding where stale-state chat was sent silently.
type reloadOutcome int

const (
	reloadOutcomeDaemonOff reloadOutcome = iota
	reloadOutcomeReloaded
	reloadOutcomeFailed
)

// maybeReloadDaemon asks a running daemon to re-read config and reconcile
// channels so the subsequent `kittypaw chat` REPL connects to a server that
// already sees the new setup. The happy path prints a single success line on
// stdout; the failure paths print recovery hints on stderr. The returned
// outcome decides whether runSetup may auto-enter chat (see reloadOutcome).
// See AC-4 / AC-5 / AC-6.
func maybeReloadDaemon(dial daemonDialer, stdout, stderr io.Writer) reloadOutcome {
	s, err := dial()
	if err != nil || s == nil || !s.IsRunning() {
		_, _ = fmt.Fprintln(stderr, setupMsgDaemonOff)
		return reloadOutcomeDaemonOff
	}
	if err := s.Reload(); err != nil {
		_, _ = fmt.Fprintf(stderr, setupMsgReloadFailedFmt+"\n", err)
		return reloadOutcomeFailed
	}
	_, _ = fmt.Fprintln(stdout, setupMsgReloaded)
	return reloadOutcomeReloaded
}

// defaultDaemonDial wires maybeReloadDaemon to the production DaemonConn +
// Client pair. Kept as a var so tests can swap in a stub without reaching
// through a whole injection framework.
var defaultDaemonDial daemonDialer = func() (daemonSession, error) {
	conn, err := client.NewDaemonConn(flagRemote)
	if err != nil {
		return nil, err
	}
	return &daemonSessionAdapter{conn: conn}, nil
}

// daemonSessionAdapter bridges a *DaemonConn (IsRunning) + *Client (Reload)
// onto the single daemonSession interface. The adapter lazily constructs the
// Client on first Reload call — IsRunning alone doesn't need one.
type daemonSessionAdapter struct {
	conn *client.DaemonConn
}

func (a *daemonSessionAdapter) IsRunning() bool { return a.conn.IsRunning() }

func (a *daemonSessionAdapter) Reload() error {
	cl := client.New(a.conn.BaseURL, a.conn.APIKey)
	_, err := cl.Reload()
	return err
}
