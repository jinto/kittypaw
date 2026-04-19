package server

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/engine"
)

// TenantRouter dispatches inbound events to tenant-scoped engine sessions.
//
// Lookup is strict by design: events with an empty TenantID or a TenantID
// that does not match a registered session are dropped. There is NO default
// fallback — a silent fallback in a multi-tenant deployment would route
// another user's messages into the default tenant's agent state (privacy
// leak). See family-multi-tenant spec constraint C1.
type TenantRouter struct {
	mu        sync.RWMutex
	sessions  map[string]*engine.Session
	dropCount atomic.Int64
	// mismatchCount tracks per-tenant chat_id ownership violations (AC-T7).
	// Keyed by the tenant ID the event claimed, not the real owner —
	// `tenant_routing_mismatch_total{from=<tenantID>}` is the spec's
	// external-facing metric label, so the local key mirrors that shape.
	mismatchCount sync.Map // map[string]*atomic.Int64
}

// NewTenantRouter returns an empty router. Callers must Register sessions
// before events arrive; unregistered tenants route to nil (drop).
func NewTenantRouter() *TenantRouter {
	return &TenantRouter{sessions: make(map[string]*engine.Session)}
}

// Register adds or replaces the session for tenantID.
func (r *TenantRouter) Register(tenantID string, sess *engine.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[tenantID] = sess
}

// Remove deletes the session for tenantID. Returns true if one was present.
func (r *TenantRouter) Remove(tenantID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[tenantID]; !ok {
		return false
	}
	delete(r.sessions, tenantID)
	return true
}

// Route returns the session matching event.TenantID, or nil if the event
// should be dropped. Empty or unknown TenantID increments the drop counter
// and logs a tenant_routing_drop event. Callers MUST check for nil and
// stop processing rather than substitute a default.
func (r *TenantRouter) Route(event core.Event) *engine.Session {
	if event.TenantID == "" {
		r.dropCount.Add(1)
		slog.Warn("tenant_routing_drop",
			"reason", "empty_tenant_id",
			"type", event.Type,
		)
		return nil
	}
	r.mu.RLock()
	sess, ok := r.sessions[event.TenantID]
	r.mu.RUnlock()
	if !ok {
		r.dropCount.Add(1)
		slog.Warn("tenant_routing_drop",
			"reason", "unknown_tenant",
			"tenant", event.TenantID,
			"type", event.Type,
		)
		return nil
	}
	return sess
}

// Sessions returns a snapshot of registered tenant IDs.
func (r *TenantRouter) Sessions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.sessions))
	for id := range r.sessions {
		ids = append(ids, id)
	}
	return ids
}

// Session returns the session registered for tenantID, or nil if none.
// Unlike Route, this does not count drops — use it for administrative
// lookups (HTTP handlers, tests) rather than event dispatch.
func (r *TenantRouter) Session(tenantID string) *engine.Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[tenantID]
}

// DropCount returns the cumulative number of events dropped because their
// TenantID was empty or unknown.
func (r *TenantRouter) DropCount() int64 {
	return r.dropCount.Load()
}

// RecordMismatch increments the per-tenant chat_id ownership violation
// counter. Callers use this *after* a successful Route() when the routed
// session's Config.AdminChatIDs rejects the event's chat_id — the event
// must be dropped and not fed to Session.Run (AC-T7).
func (r *TenantRouter) RecordMismatch(tenantID string) {
	v, _ := r.mismatchCount.LoadOrStore(tenantID, &atomic.Int64{})
	v.(*atomic.Int64).Add(1)
}

// MismatchCount returns the cumulative mismatch count for tenantID, or 0
// when no mismatches have been recorded for that tenant. Used by tests and
// the /metrics endpoint to expose `tenant_routing_mismatch_total{from=...}`.
func (r *TenantRouter) MismatchCount(tenantID string) int64 {
	v, ok := r.mismatchCount.Load(tenantID)
	if !ok {
		return 0
	}
	return v.(*atomic.Int64).Load()
}
