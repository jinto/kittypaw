package server

import (
	"path/filepath"
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/sandbox"
	"github.com/jinto/kittypaw/store"
)

// TestServer_New_WiresAccountFieldsPerAccount is the TDD lead for PR-1:
// server.New must build one engine.Session per account with AccountID,
// AccountRegistry (shared pointer), and Fanout (family-only) wired.
// Until this test passes, Plan B's cross-account Share.read + Fanout
// paths are dead code — see Plan C items C9/C11 in TASKS.md.
func TestServer_New_WiresAccountFieldsPerAccount(t *testing.T) {
	root := t.TempDir()

	familyDeps := buildAccountDeps(t, root, "family", &core.Config{IsFamily: true})
	aliceDeps := buildAccountDeps(t, root, "alice", &core.Config{})

	srv := New([]*AccountDeps{familyDeps, aliceDeps}, "test")

	famSess := srv.accounts.Session("family")
	if famSess == nil {
		t.Fatal("family Session not registered on AccountRouter")
	}
	aliceSess := srv.accounts.Session("alice")
	if aliceSess == nil {
		t.Fatal("alice Session not registered on AccountRouter")
	}

	// --- AccountID set on each session.
	if famSess.AccountID != "family" {
		t.Errorf("family.AccountID = %q, want %q", famSess.AccountID, "family")
	}
	if aliceSess.AccountID != "alice" {
		t.Errorf("alice.AccountID = %q, want %q", aliceSess.AccountID, "alice")
	}

	// --- Same *core.AccountRegistry pointer on every session.
	if famSess.AccountRegistry == nil {
		t.Fatal("family.AccountRegistry is nil")
	}
	if famSess.AccountRegistry != aliceSess.AccountRegistry {
		t.Errorf("accounts must share one AccountRegistry; got %p vs %p",
			famSess.AccountRegistry, aliceSess.AccountRegistry)
	}

	// --- Fanout: family gets it; personal does NOT.
	if famSess.Fanout == nil {
		t.Error("family.Fanout must be non-nil (Fanout.send/broadcast capability)")
	}
	if aliceSess.Fanout != nil {
		t.Error("personal account.Fanout must be nil (I5 — personal cannot reach personal)")
	}

	// --- Defense in depth: registry.Get resolves both accounts.
	if famSess.AccountRegistry.Get("family") == nil {
		t.Error("registry missing family entry")
	}
	if famSess.AccountRegistry.Get("alice") == nil {
		t.Error("registry missing alice entry")
	}
}

// TestServer_New_LegacySingleAccount_NoFanout enforces backward
// compatibility: a single "default" account (legacy install) boots with
// Fanout=nil and AccountRegistry non-nil. We intentionally do NOT gate
// AccountRegistry on multi-account — personal→family Share.read works
// the same whether there are 1 or N accounts.
func TestServer_New_LegacySingleAccount_NoFanout(t *testing.T) {
	root := t.TempDir()
	defaultDeps := buildAccountDeps(t, root, DefaultAccountID, &core.Config{})

	srv := New([]*AccountDeps{defaultDeps}, "test")

	sess := srv.accounts.Session(DefaultAccountID)
	if sess == nil {
		t.Fatal("default Session not registered")
	}
	if sess.AccountID != DefaultAccountID {
		t.Errorf("AccountID = %q, want %q", sess.AccountID, DefaultAccountID)
	}
	if sess.Fanout != nil {
		t.Error("legacy single-account must have Fanout=nil")
	}
	if sess.AccountRegistry == nil {
		t.Error("AccountRegistry must be non-nil even in single-account mode")
	}
	if ids := srv.accounts.Sessions(); len(ids) != 1 || ids[0] != DefaultAccountID {
		t.Errorf("Sessions() = %v, want [%s]", ids, DefaultAccountID)
	}
}

// buildAccountDeps constructs the minimum set of per-account dependencies
// needed to drive server.New through its wiring path. Store is a real
// tempdir SQLite (server.New calls Store methods during startup);
// Provider is nil because no Run() is executed in these tests.
func buildAccountDeps(t *testing.T, root, id string, cfg *core.Config) *AccountDeps {
	t.Helper()

	baseDir := filepath.Join(root, id)
	account := &core.Account{ID: id, BaseDir: baseDir, Config: cfg}
	if err := account.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs %s: %v", id, err)
	}

	dbPath := filepath.Join(account.DataDir(), "kittypaw.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store %s: %v", dbPath, err)
	}
	t.Cleanup(func() { _ = st.Close() })

	sbox := sandbox.New(core.SandboxConfig{TimeoutSecs: 5})

	secrets, _ := core.LoadSecretsFrom(filepath.Join(baseDir, "secrets.json"))
	pkgMgr := core.NewPackageManagerFrom(baseDir, secrets)
	apiTokenMgr := core.NewAPITokenManager(baseDir, secrets)

	return &AccountDeps{
		Account:     account,
		Store:       st,
		Sandbox:     sbox,
		PkgMgr:      pkgMgr,
		APITokenMgr: apiTokenMgr,
		Secrets:     secrets,
	}
}
