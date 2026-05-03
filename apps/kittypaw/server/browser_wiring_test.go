package server

import (
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestBuildAccountSessionWiresBrowserController(t *testing.T) {
	root := t.TempDir()
	cfg := core.DefaultConfig()
	cfg.Workspace.LiveIndex = false
	td := buildAccountDeps(t, root, "alice", &cfg)

	sess := buildAccountSession(td, core.NewAccountRegistry(root, "alice"), nil)
	if sess.BrowserController == nil {
		t.Fatal("BrowserController not wired")
	}
}
