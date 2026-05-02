// Package janitor runs daily credential lifecycle GC: idle device
// reaping, revoked-device hard-delete, expired-refresh hard-delete.
//
// One in-process goroutine, ticker-driven, KST 04:00. The user-facing
// model (silly-wiggling-balloon.md follow-up) is "install → login → chat";
// device IDs, refresh tokens, and revocation are server-internal state.
// The janitor keeps that state from accumulating without forcing the
// user to manage it.
//
// Plan 24 — Credential Lifecycle Janitor.
package janitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kittypaw-app/kittyportal/internal/model"
)

// Policy carries the four time-based knobs the janitor uses each tick.
// Defaults (DefaultPolicy) match Plan 24 user-confirmed values.
type Policy struct {
	// IdleThreshold — devices whose latest activity (last_used_at, or
	// paired_at if never refreshed) predates now-IdleThreshold get soft-
	// revoked. 60 days.
	IdleThreshold time.Duration

	// RevokedRetention — devices revoked before now-RevokedRetention
	// get hard-deleted (refresh_tokens cascade). 90 days = forensic
	// replay window.
	RevokedRetention time.Duration

	// ExpiredRefreshRetention — refresh tokens whose expires_at predates
	// now-ExpiredRefreshRetention get hard-deleted. 30 days.
	ExpiredRefreshRetention time.Duration

	// RunHourKST — daily run hour in KST (0-23). 4 = 04:00 KST, lowest
	// traffic window.
	RunHourKST int
}

// DefaultPolicy carries the Plan 24 user-confirmed values. main.go uses
// this directly; tests construct ad-hoc Policy instances.
var DefaultPolicy = Policy{
	IdleThreshold:           60 * 24 * time.Hour,
	RevokedRetention:        90 * 24 * time.Hour,
	ExpiredRefreshRetention: 30 * 24 * time.Hour,
	RunHourKST:              4,
}

// Clock is the time-source seam for tests. RealClock is the production
// implementation; tests inject a mock that drives After channels manually.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// RealClock is the time package wrapper used in production.
type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Janitor owns the daily lifecycle sweep. Construct via New, run via Run.
// Tick is exposed for integration tests that drive a single sweep on a
// real DB without waiting for the ticker.
type Janitor struct {
	devices model.DeviceStore
	refresh model.RefreshTokenStore
	policy  Policy
	clock   Clock
}

// New constructs a Janitor. clock=nil falls back to RealClock so callers
// don't have to import the package's Clock type for production wiring.
func New(devices model.DeviceStore, refresh model.RefreshTokenStore, policy Policy, clock Clock) *Janitor {
	if clock == nil {
		clock = RealClock{}
	}
	return &Janitor{
		devices: devices,
		refresh: refresh,
		policy:  policy,
		clock:   clock,
	}
}

// Run blocks until ctx is canceled, firing Tick once per day at
// policy.RunHourKST. The first sleep aligns to the next 04:00 KST so a
// 14:00 deploy doesn't run cleanup at peak traffic minutes later.
//
// Errors inside Tick are logged but do not stop the loop — a transient
// DB blip on day N must not skip cleanup forever.
func (j *Janitor) Run(ctx context.Context) {
	slog.Info("janitor.start",
		"idle_threshold", j.policy.IdleThreshold,
		"revoked_retention", j.policy.RevokedRetention,
		"expired_refresh_retention", j.policy.ExpiredRefreshRetention,
		"run_hour_kst", j.policy.RunHourKST,
	)
	for {
		next := nextRunAt(j.clock.Now(), j.policy.RunHourKST)
		delay := next.Sub(j.clock.Now())
		select {
		case <-ctx.Done():
			slog.Info("janitor.stop")
			return
		case <-j.clock.After(delay):
		}
		j.Tick(ctx)
	}
}

// Tick runs one sweep: expired refresh delete → idle device reap →
// revoked device delete. Order matters only loosely (all three are
// independent on the data they touch — expired-refresh and revoked-
// device share no rows). The order here mirrors the cheapest-first
// principle (expired-refresh has the most rows in steady state).
//
// Each step's error is logged; a failure in one step does NOT short-
// circuit the others. A transient DB error reaping idle devices must
// not block the expired-refresh sweep that already succeeded.
func (j *Janitor) Tick(ctx context.Context) {
	now := j.clock.Now()

	expCutoff := now.Add(-j.policy.ExpiredRefreshRetention)
	expDeleted, err := j.refresh.DeleteExpiredOlderThan(ctx, expCutoff)
	if err != nil {
		logErr("DeleteExpiredOlderThan failed", err)
	}

	idleCutoff := now.Add(-j.policy.IdleThreshold)
	reaped, err := j.devices.ReapIdle(ctx, idleCutoff)
	if err != nil {
		logErr("ReapIdle failed", err)
	}

	revokedCutoff := now.Add(-j.policy.RevokedRetention)
	devDeleted, err := j.devices.DeleteRevokedOlderThan(ctx, revokedCutoff)
	if err != nil {
		logErr("DeleteRevokedOlderThan failed", err)
	}

	slog.Info("janitor.tick",
		"expired_refresh_deleted", expDeleted,
		"idle_devices_reaped", reaped,
		"revoked_devices_deleted", devDeleted,
	)
}

// logErr emits an Error slog line WITHOUT the raw err message — pgx
// errors can include parameter values (timestamps, IDs) that aren't
// secret here but the codebase convention (see auth.logStoreErr) is to
// omit raw err in JSON-formatted prod logs. Mirroring keeps the systemd
// journal output uniform.
func logErr(msg string, err error) {
	slog.Error("janitor: "+msg, "err_type", fmt.Sprintf("%T", err))
}

// nextRunAt returns the next time-of-day at hourKST (in Asia/Seoul) at
// or after now. If now is already past today's hourKST mark, the result
// is tomorrow's. Pure function — testable without a real clock.
func nextRunAt(now time.Time, hourKST int) time.Time {
	kst := time.FixedZone("KST", 9*3600)
	nowKST := now.In(kst)
	next := time.Date(nowKST.Year(), nowKST.Month(), nowKST.Day(), hourKST, 0, 0, 0, kst)
	if !next.After(nowKST) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
