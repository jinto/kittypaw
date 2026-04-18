package engine

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

// RecoverTenantPanic is the single panic-recovery chokepoint for tenant
// goroutines — one tenant's panic must not take down the shared daemon
// (family-multi-tenant spec AC-T8). site names the caller
// ("scheduler.runSkill", "server.dispatchLoop") so structured logs
// identify which layer caught the panic. Nil-safe on sess and
// sess.Health for bare-struct test fixtures.
func RecoverTenantPanic(sess *Session, site string, r any) {
	tenant := ""
	if sess != nil {
		tenant = sess.TenantID
	}
	slog.Error("tenant_panic_recovered",
		"tenant", tenant,
		"site", site,
		"panic", fmt.Sprintf("%v", r),
		"stack", string(debug.Stack()),
	)
	if sess != nil && sess.Health != nil {
		sess.Health.MarkDegraded(time.Now())
	}
}

// MarkTenantReady promotes Health back to Ready on clean completion so a
// transient panic self-heals on the next successful iteration. Nil-safe.
func MarkTenantReady(sess *Session) {
	if sess == nil || sess.Health == nil {
		return
	}
	sess.Health.MarkReady()
}

// runWithTenantRecover executes fn under a deferred recover. A panic
// marks the tenant Degraded via RecoverTenantPanic; clean completion
// promotes it back to Ready. Use from every worker goroutine where a
// single panic should not wedge the tenant.
func runWithTenantRecover(sess *Session, site string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			RecoverTenantPanic(sess, site, r)
			return
		}
		MarkTenantReady(sess)
	}()
	fn()
}
