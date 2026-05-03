package server

import (
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/engine"
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

func TestAccountSchedulersReplaceReturnsPreviousScheduler(t *testing.T) {
	schedulers := NewAccountSchedulers()
	first := engine.NewScheduler(&engine.Session{}, nil)
	second := engine.NewScheduler(&engine.Session{}, nil)
	third := engine.NewScheduler(&engine.Session{}, nil)

	schedulers.Register("alice", first)
	if got := schedulers.Replace("alice", second); got != first {
		t.Fatalf("first Replace returned %p, want first scheduler %p", got, first)
	}
	first.Wait()
	if got := schedulers.Replace("alice", third); got != second {
		t.Fatalf("second Replace returned %p, want second scheduler %p", got, second)
	}
	second.Wait()
	if !schedulers.Has("alice") {
		t.Fatal("alice scheduler missing after Replace")
	}
	if got := schedulers.Len(); got != 1 {
		t.Fatalf("scheduler count = %d, want 1", got)
	}
}
