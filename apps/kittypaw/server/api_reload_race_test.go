package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/jinto/kittypaw/client"
	"github.com/jinto/kittypaw/core"
)

// AC-RACE: re-runs the Reload→observe loop 50 times under -race to confirm
// that the happens-before edge established by handleReload (POST returns
// after Reconcile completes) is also a memory fence from the client's
// perspective. In production, `cli/cmd_setup.maybeReloadDaemon` returns and
// the same goroutine immediately calls `runChat`, which reaches back to the
// server via a new HTTP request. That HTTP round-trip carries a happens-
// before edge, but if a future refactor collapses Reload into an in-process
// channel swap, this test must still pass — it pins the guarantee the CLI
// side depends on.
//
// Run with: `go test -race -count 50 -run TestAutoEntryNoRace ./server/`.
func TestAutoEntryNoRace(t *testing.T) {
	if testing.Short() {
		t.Skip("AC-RACE loops the full reload path; skip in short mode")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	kpDir := filepath.Join(home, ".kittypaw")
	tenantCfgDir := filepath.Join(kpDir, "tenants", core.DefaultTenantID)
	if err := os.MkdirAll(tenantCfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := core.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "test"
	cfg.LLM.Model = "claude-test"
	if err := core.WriteConfigAtomic(&cfg, filepath.Join(tenantCfgDir, "config.toml")); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var reconciled atomic.Int64
	srv := &Server{
		config: &cfg,
		reloadReconcile: func(_ string, _ []core.ChannelConfig) error {
			// Bump the counter from inside Reconcile — the CLI path relies on
			// this write being observable the moment Reload returns.
			reconciled.Add(1)
			return nil
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(srv.handleReload))
	defer ts.Close()

	cl := client.New(ts.URL, "")
	const iters = 50
	for i := 0; i < iters; i++ {
		before := reconciled.Load()
		if _, err := cl.Reload(); err != nil {
			t.Fatalf("iter %d: Reload: %v", i, err)
		}
		// Immediate observation. No sleep, no fence — if the sync contract
		// holds, the counter must have advanced by exactly one.
		after := reconciled.Load()
		if after != before+1 {
			t.Fatalf("iter %d: reconciled advanced by %d, want 1", i, after-before)
		}
	}
}
