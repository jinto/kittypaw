package server

import (
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/engine"
)

func TestTenantRouter_RouteRegistered(t *testing.T) {
	alice := &engine.Session{BaseDir: "/tmp/alice"}
	bob := &engine.Session{BaseDir: "/tmp/bob"}

	r := NewTenantRouter()
	r.Register("alice", alice)
	r.Register("bob", bob)

	got := r.Route(core.Event{TenantID: "alice", Type: core.EventTelegram})
	if got != alice {
		t.Errorf("Route(alice) got %p, want %p", got, alice)
	}
	got = r.Route(core.Event{TenantID: "bob", Type: core.EventTelegram})
	if got != bob {
		t.Errorf("Route(bob) got %p, want %p", got, bob)
	}
}

// TestTenantRouter_NoFallback enforces C1: empty or unknown TenantID must
// drop — never fall through to a default tenant (cross-tenant leak risk).
func TestTenantRouter_NoFallback(t *testing.T) {
	alice := &engine.Session{BaseDir: "/tmp/alice"}
	r := NewTenantRouter()
	r.Register("alice", alice)
	r.Register("default", alice) // default exists, but unknown must still drop

	tests := []struct {
		name  string
		event core.Event
	}{
		{"empty_tenant_id", core.Event{TenantID: "", Type: core.EventTelegram}},
		{"unknown_tenant", core.Event{TenantID: "charlie", Type: core.EventTelegram}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Route(tt.event)
			if got != nil {
				t.Errorf("Route() = %p, want nil (drop, no fallback)", got)
			}
		})
	}

	if n := r.DropCount(); n != 2 {
		t.Errorf("DropCount = %d, want 2", n)
	}
}

func TestTenantRouter_RemoveAndSessions(t *testing.T) {
	r := NewTenantRouter()
	r.Register("alice", &engine.Session{})
	r.Register("bob", &engine.Session{})

	if got := r.Route(core.Event{TenantID: "alice"}); got == nil {
		t.Error("alice should be routable")
	}

	if !r.Remove("alice") {
		t.Error("Remove(alice) = false, want true")
	}
	if r.Remove("alice") {
		t.Error("Remove(alice) second call = true, want false")
	}

	if got := r.Route(core.Event{TenantID: "alice"}); got != nil {
		t.Error("alice should be gone after Remove")
	}

	ids := r.Sessions()
	if len(ids) != 1 || ids[0] != "bob" {
		t.Errorf("Sessions() = %v, want [bob]", ids)
	}
}

// TestTenantRouter_MismatchCounters locks in the AC-T7 metric surface:
// RecordMismatch bumps a per-tenant counter; MismatchCount reads only that
// tenant's count and returns 0 for untouched tenants without allocating.
// The /metrics endpoint surfaces this as
// `tenant_routing_mismatch_total{from=<tenantID>}`.
func TestTenantRouter_MismatchCounters(t *testing.T) {
	r := NewTenantRouter()

	if n := r.MismatchCount("alice"); n != 0 {
		t.Errorf("MismatchCount(alice) initial = %d, want 0", n)
	}

	r.RecordMismatch("alice")
	r.RecordMismatch("alice")
	r.RecordMismatch("bob")

	if n := r.MismatchCount("alice"); n != 2 {
		t.Errorf("MismatchCount(alice) = %d, want 2", n)
	}
	if n := r.MismatchCount("bob"); n != 1 {
		t.Errorf("MismatchCount(bob) = %d, want 1", n)
	}
	if n := r.MismatchCount("charlie"); n != 0 {
		t.Errorf("MismatchCount(charlie) = %d, want 0 (unseen tenant)", n)
	}
}

func TestTenantRouter_ConcurrentAccess(t *testing.T) {
	r := NewTenantRouter()
	sess := &engine.Session{}
	r.Register("alice", sess)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = r.Route(core.Event{TenantID: "alice"})
				_ = r.Route(core.Event{TenantID: "unknown"})
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if dc := r.DropCount(); dc != 1000 {
		t.Errorf("DropCount = %d, want 1000", dc)
	}
}
