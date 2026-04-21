//go:build integration

package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinto/kittypaw/store"
)

// TestLiveIndexer_Integration_CreateModifyDelete asserts the full pipeline:
// fsnotify → Watcher → Debouncer → FTS5Indexer → SearchWorkspaceFTS
// reflects file changes within the debounce window.
func TestLiveIndexer_Integration_CreateModifyDelete(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}

	// Real store + indexer.
	dbPath := filepath.Join(t.TempDir(), "kittypaw.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()

	// Register workspace in the store.
	ws := &store.Workspace{ID: "live-int", Name: "live-int", RootPath: dir}
	if err := st.SaveWorkspace(ws); err != nil {
		t.Fatalf("save workspace: %v", err)
	}

	idx := NewFTS5Indexer(st)
	// Short debounce for faster test turnaround.
	li, err := NewLiveIndexer(idx, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("live indexer: %v", err)
	}
	defer li.Close()

	if err := li.AddWorkspace("live-int", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	li.Start()

	target := filepath.Join(dir, "alpha.go")

	// --- Phase 1: Create → Index reflected ---
	if err := os.WriteFile(target, []byte("package main\nfunc UniqueAlphaSymbol() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForFTS(t, st, "UniqueAlphaSymbol", 2*time.Second, 1)

	// --- Phase 2: Modify → old symbol gone, new symbol present ---
	if err := os.WriteFile(target, []byte("package main\nfunc UniqueBetaSymbol() {}\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	waitForFTS(t, st, "UniqueBetaSymbol", 2*time.Second, 1)
	waitForFTS(t, st, "UniqueAlphaSymbol", 2*time.Second, 0)

	// --- Phase 3: Delete → removed from index ---
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove: %v", err)
	}
	waitForFTS(t, st, "UniqueBetaSymbol", 2*time.Second, 0)
}

// TestLiveIndexer_Integration_NewSubdirRecurse asserts a directory created
// at runtime is watched recursively.
func TestLiveIndexer_Integration_NewSubdirRecurse(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "kittypaw.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	_ = st.SaveWorkspace(&store.Workspace{ID: "sub-int", Name: "sub-int", RootPath: dir})

	idx := NewFTS5Indexer(st)
	li, _ := NewLiveIndexer(idx, 50*time.Millisecond, 200*time.Millisecond)
	defer li.Close()
	_ = li.AddWorkspace("sub-int", dir)
	li.Start()

	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	time.Sleep(150 * time.Millisecond) // let Watcher Add nested

	target := filepath.Join(sub, "inner.go")
	if err := os.WriteFile(target, []byte("package nested\nfunc UniqueNestedSymbol() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForFTS(t, st, "UniqueNestedSymbol", 2*time.Second, 1)
}

func waitForFTS(t *testing.T, st *store.Store, query string, timeout time.Duration, wantCount int) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		results, _, err := st.SearchWorkspaceFTS(query, "", "", 20, 0)
		if err == nil && len(results) == wantCount {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	results, _, _ := st.SearchWorkspaceFTS(query, "", "", 20, 0)
	t.Fatalf("FTS wait timeout: query=%q got %d results, want %d", query, len(results), wantCount)
}
