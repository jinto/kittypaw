package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
)

// AC-RELOAD-SYNC: pin the load-bearing contract that `POST /api/v1/reload`
// returns ONLY AFTER spawner.Reconcile completes. `cli/cmd_setup.go`'s
// maybeReloadDaemon → runChat sequence assumes handleReload is synchronous
// — if someone converts Reconcile to a background goroutine, the subsequent
// chat REPL will race the new channel set and connect to a server still
// holding the old config.
//
// The test wires a Server directly (no New()) with:
//   - a minimal config on disk (so core.LoadConfig succeeds),
//   - a reloadReconcile hook that blocks on a barrier,
//
// then asserts the HTTP response is NOT delivered while the hook blocks, and
// IS delivered promptly once the hook returns.
func TestHandleReload_WaitsForReconcile(t *testing.T) {
	// Arrange: write a throwaway config so core.LoadConfig succeeds. We set
	// HOME so ConfigPath resolves into the temp dir without polluting the
	// real ~/.kittypaw.
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

	barrier := make(chan struct{})
	started := make(chan struct{})
	var callN int32

	srv := &Server{
		config: &cfg,
		reloadReconcile: func(_ string, _ []core.ChannelConfig) error {
			atomic.AddInt32(&callN, 1)
			close(started)
			<-barrier
			return nil
		},
	}
	handler := http.HandlerFunc(srv.handleReload)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Act: fire reload from a goroutine so we can observe it's still in flight.
	done := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := http.Post(ts.URL, "application/json", nil)
		if err != nil {
			errCh <- err
			return
		}
		done <- resp
	}()

	// Assert: Reconcile began…
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile hook never ran")
	}
	// …and the HTTP response is still blocked.
	select {
	case resp := <-done:
		resp.Body.Close()
		t.Fatal("handleReload returned BEFORE Reconcile completed — sync contract broken")
	case err := <-errCh:
		t.Fatalf("HTTP error: %v", err)
	case <-time.After(100 * time.Millisecond):
		// expected: still blocked
	}

	// Unblock and assert the response arrives within a reasonable window.
	close(barrier)
	select {
	case resp := <-done:
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	case err := <-errCh:
		t.Fatalf("HTTP error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("handleReload did not return after Reconcile unblocked")
	}

	if n := atomic.LoadInt32(&callN); n != 1 {
		t.Fatalf("Reconcile called %d times, want 1", n)
	}
}
