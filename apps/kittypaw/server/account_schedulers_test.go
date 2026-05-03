package server

import (
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestServerNewCreatesSchedulerPerAccount(t *testing.T) {
	root := t.TempDir()
	teamCfg := core.DefaultConfig()
	teamCfg.IsFamily = true
	teamCfg.TeamSpace.Members = []string{"alice", "bob"}
	aliceCfg := core.DefaultConfig()
	bobCfg := core.DefaultConfig()

	teamDeps := buildAccountDeps(t, root, "team", &teamCfg)
	aliceDeps := buildAccountDeps(t, root, "alice", &aliceCfg)
	bobDeps := buildAccountDeps(t, root, "bob", &bobCfg)

	srv := New([]*AccountDeps{teamDeps, aliceDeps, bobDeps}, "test")

	if srv.schedulers == nil {
		t.Fatal("schedulers is nil")
	}
	if got := srv.schedulers.Len(); got != 3 {
		t.Fatalf("scheduler count = %d, want 3", got)
	}
	for _, accountID := range []string{"team", "alice", "bob"} {
		if !srv.schedulers.Has(accountID) {
			t.Fatalf("scheduler for %q missing", accountID)
		}
	}
}

func TestAddRemoveAccountMaintainsScheduler(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	alice := accountForDirectAdd(root, "alice", false, nil)
	if err := srv.AddAccount(alice); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if !srv.schedulers.Has("alice") {
		t.Fatal("scheduler for alice missing after AddAccount")
	}

	if err := srv.RemoveAccount("alice"); err != nil {
		t.Fatalf("RemoveAccount: %v", err)
	}
	if srv.schedulers.Has("alice") {
		t.Fatal("scheduler for alice still registered after RemoveAccount")
	}
}
