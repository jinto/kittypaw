package server

import (
	"path/filepath"
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/sandbox"
	"github.com/jinto/kittypaw/store"
)

// TestServer_New_WiresTenantFieldsPerTenant is the TDD lead for PR-1:
// server.New must build one engine.Session per tenant with TenantID,
// TenantRegistry (shared pointer), and Fanout (family-only) wired.
// Until this test passes, Plan B's cross-tenant Share.read + Fanout
// paths are dead code — see Plan C items C9/C11 in TASKS.md.
func TestServer_New_WiresTenantFieldsPerTenant(t *testing.T) {
	root := t.TempDir()

	familyDeps := buildTenantDeps(t, root, "family", &core.Config{IsFamily: true})
	aliceDeps := buildTenantDeps(t, root, "alice", &core.Config{})

	srv := New([]*TenantDeps{familyDeps, aliceDeps}, "test")

	famSess := srv.tenants.Session("family")
	if famSess == nil {
		t.Fatal("family Session not registered on TenantRouter")
	}
	aliceSess := srv.tenants.Session("alice")
	if aliceSess == nil {
		t.Fatal("alice Session not registered on TenantRouter")
	}

	// --- TenantID set on each session.
	if famSess.TenantID != "family" {
		t.Errorf("family.TenantID = %q, want %q", famSess.TenantID, "family")
	}
	if aliceSess.TenantID != "alice" {
		t.Errorf("alice.TenantID = %q, want %q", aliceSess.TenantID, "alice")
	}

	// --- Same *core.TenantRegistry pointer on every session.
	if famSess.TenantRegistry == nil {
		t.Fatal("family.TenantRegistry is nil")
	}
	if famSess.TenantRegistry != aliceSess.TenantRegistry {
		t.Errorf("tenants must share one TenantRegistry; got %p vs %p",
			famSess.TenantRegistry, aliceSess.TenantRegistry)
	}

	// --- Fanout: family gets it; personal does NOT.
	if famSess.Fanout == nil {
		t.Error("family.Fanout must be non-nil (Fanout.send/broadcast capability)")
	}
	if aliceSess.Fanout != nil {
		t.Error("personal tenant.Fanout must be nil (I5 — personal cannot reach personal)")
	}

	// --- Defense in depth: registry.Get resolves both tenants.
	if famSess.TenantRegistry.Get("family") == nil {
		t.Error("registry missing family entry")
	}
	if famSess.TenantRegistry.Get("alice") == nil {
		t.Error("registry missing alice entry")
	}
}

// TestServer_New_LegacySingleTenant_NoFanout enforces backward
// compatibility: a single "default" tenant (legacy install) boots with
// Fanout=nil and TenantRegistry non-nil. We intentionally do NOT gate
// TenantRegistry on multi-tenant — personal→family Share.read works
// the same whether there are 1 or N tenants.
func TestServer_New_LegacySingleTenant_NoFanout(t *testing.T) {
	root := t.TempDir()
	defaultDeps := buildTenantDeps(t, root, DefaultTenantID, &core.Config{})

	srv := New([]*TenantDeps{defaultDeps}, "test")

	sess := srv.tenants.Session(DefaultTenantID)
	if sess == nil {
		t.Fatal("default Session not registered")
	}
	if sess.TenantID != DefaultTenantID {
		t.Errorf("TenantID = %q, want %q", sess.TenantID, DefaultTenantID)
	}
	if sess.Fanout != nil {
		t.Error("legacy single-tenant must have Fanout=nil")
	}
	if sess.TenantRegistry == nil {
		t.Error("TenantRegistry must be non-nil even in single-tenant mode")
	}
	if ids := srv.tenants.Sessions(); len(ids) != 1 || ids[0] != DefaultTenantID {
		t.Errorf("Sessions() = %v, want [%s]", ids, DefaultTenantID)
	}
}

// buildTenantDeps constructs the minimum set of per-tenant dependencies
// needed to drive server.New through its wiring path. Store is a real
// tempdir SQLite (server.New calls Store methods during startup);
// Provider is nil because no Run() is executed in these tests.
func buildTenantDeps(t *testing.T, root, id string, cfg *core.Config) *TenantDeps {
	t.Helper()

	baseDir := filepath.Join(root, id)
	tenant := &core.Tenant{ID: id, BaseDir: baseDir, Config: cfg}
	if err := tenant.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs %s: %v", id, err)
	}

	dbPath := filepath.Join(tenant.DataDir(), "kittypaw.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store %s: %v", dbPath, err)
	}
	t.Cleanup(func() { _ = st.Close() })

	sbox := sandbox.New(core.SandboxConfig{TimeoutSecs: 5})

	secrets, _ := core.LoadSecretsFrom(filepath.Join(baseDir, "secrets.json"))
	pkgMgr := core.NewPackageManagerFrom(baseDir, secrets)
	apiTokenMgr := core.NewAPITokenManager(baseDir, secrets)

	return &TenantDeps{
		Tenant:      tenant,
		Store:       st,
		Sandbox:     sbox,
		PkgMgr:      pkgMgr,
		APITokenMgr: apiTokenMgr,
		Secrets:     secrets,
	}
}
