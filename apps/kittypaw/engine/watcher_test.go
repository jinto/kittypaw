package engine

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// waitForEvent waits up to timeout for a matching event predicate.
// Returns the matched event or fails the test.
func waitForEvent(t *testing.T, ch <-chan WatchEvent, timeout time.Duration, pred func(WatchEvent) bool) WatchEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case ev := <-ch:
			if pred(ev) {
				return ev
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for event")
	return WatchEvent{}
}

// collectEvents drains all events up to timeout (used to assert absence).
func collectEvents(ch <-chan WatchEvent, timeout time.Duration) []WatchEvent {
	var got []WatchEvent
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case ev := <-ch:
			got = append(got, ev)
		case <-time.After(20 * time.Millisecond):
		}
	}
	return got
}

func TestWatcher_CreateFileEmitsIndex(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()

	if err := w.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	w.Start()

	target := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := waitForEvent(t, w.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.WorkspaceID == "ws" && e.AbsPath == target && e.Op == DebounceIndex
	})
	if ev.RootPath != dir {
		t.Errorf("RootPath: got %q, want %q", ev.RootPath, dir)
	}
}

func TestWatcher_WriteEmitsIndex(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()
	if err := w.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	w.Start()

	// Drain any initial events (there should be none since we Started after
	// AddWorkspace but before writing).
	_ = collectEvents(w.Events(), 100*time.Millisecond)

	if err := os.WriteFile(target, []byte("v2 modified"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	waitForEvent(t, w.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.AbsPath == target && e.Op == DebounceIndex
	})
}

func TestWatcher_RemoveEmitsRemove(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(target, []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()
	_ = w.AddWorkspace("ws", dir)
	w.Start()
	_ = collectEvents(w.Events(), 100*time.Millisecond)

	if err := os.Remove(target); err != nil {
		t.Fatalf("remove: %v", err)
	}

	waitForEvent(t, w.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.AbsPath == target && e.Op == DebounceRemove
	})
}

func TestWatcher_ExcludedDirNotWatched(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()
	_ = w.AddWorkspace("ws", dir)
	w.Start()

	// Write a file inside .git/ — should not emit any event.
	gitFile := filepath.Join(gitDir, "config")
	if err := os.WriteFile(gitFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	events := collectEvents(w.Events(), 300*time.Millisecond)
	for _, ev := range events {
		if ev.AbsPath == gitFile {
			t.Errorf(".git file emitted event: %+v", ev)
		}
	}
}

func TestWatcher_NewDirectoryCreatesRecurse(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()
	_ = w.AddWorkspace("ws", dir)
	w.Start()

	newDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Give fsnotify time to see the dir create and Watcher to Add it.
	time.Sleep(100 * time.Millisecond)

	target := filepath.Join(newDir, "inner.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitForEvent(t, w.Events(), 2*time.Second, func(e WatchEvent) bool {
		return e.AbsPath == target && e.Op == DebounceIndex
	})
}

func TestWatcher_EditorTempFileIgnored(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()
	_ = w.AddWorkspace("ws", dir)
	w.Start()

	// vim-style swap file.
	swp := filepath.Join(dir, ".main.go.swp")
	if err := os.WriteFile(swp, []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// emacs-style lock file.
	lock := filepath.Join(dir, "#lock#")
	_ = os.WriteFile(lock, []byte(""), 0o644)

	events := collectEvents(w.Events(), 300*time.Millisecond)
	for _, ev := range events {
		if ev.AbsPath == swp || ev.AbsPath == lock {
			t.Errorf("temp file emitted: %+v", ev)
		}
	}
}

func TestWatcher_MultipleWorkspaces(t *testing.T) {
	// macOS kqueue delivers temp dir events with resolved symlink paths
	// (/private/var/...), but Go's t.TempDir returns the unresolved form
	// (/var/...). workspaceFor uses strict prefix match, so we resolve up
	// front to keep the routing deterministic.
	dirA, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval A: %v", err)
	}
	dirB, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval B: %v", err)
	}

	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()
	_ = w.AddWorkspace("A", dirA)
	_ = w.AddWorkspace("B", dirB)
	w.Start()

	fileA := filepath.Join(dirA, "a.txt")
	fileB := filepath.Join(dirB, "b.txt")
	_ = os.WriteFile(fileA, []byte("a"), 0o644)
	_ = os.WriteFile(fileB, []byte("b"), 0o644)

	var sawA, sawB bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (!sawA || !sawB) {
		select {
		case ev := <-w.Events():
			if ev.AbsPath == fileA && ev.WorkspaceID == "A" {
				sawA = true
			}
			if ev.AbsPath == fileB && ev.WorkspaceID == "B" {
				sawB = true
			}
			if ev.AbsPath == fileA && ev.WorkspaceID != "A" {
				t.Errorf("fileA routed to %q, want A", ev.WorkspaceID)
			}
			if ev.AbsPath == fileB && ev.WorkspaceID != "B" {
				t.Errorf("fileB routed to %q, want B", ev.WorkspaceID)
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !sawA || !sawB {
		t.Errorf("missed events: sawA=%v sawB=%v", sawA, sawB)
	}
}

func TestWatcher_CloseNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	for range 5 {
		w, err := NewWatcher()
		if err != nil {
			t.Skipf("fsnotify unavailable: %v", err)
		}
		_ = w.AddWorkspace("ws", t.TempDir())
		w.Start()
		if err := w.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}

	// Give any straggling goroutines time to exit.
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Allow some slack for test framework background goroutines.
	if after > before+2 {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestWatcher_PartialAddFailures_CountsSubdirErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-based unreachable-dir trick is ineffective as root")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "no_access")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	// Stripping all permissions forces walkDir / fs.Add to fail on this
	// subtree. Restore at test end so t.TempDir cleanup succeeds.
	if err := os.Chmod(sub, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()

	// Root Add should succeed despite the unreachable subdir.
	if err := w.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace should not fail on root when only a subdir is unreachable: %v", err)
	}

	if got := w.PartialAddFailures(); got < 1 {
		t.Errorf("PartialAddFailures: got %d, want >= 1", got)
	}
}

func TestWatcher_PartialAddFailures_SafeAfterClose(t *testing.T) {
	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	_ = w.AddWorkspace("ws", t.TempDir())
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Must not panic post-Close (atomic field, not guarded state).
	_ = w.PartialAddFailures()
}

func TestWatcher_OverflowError_TriggersHandler(t *testing.T) {
	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()

	var called atomic.Int64
	w.SetOverflowHandler(func() { called.Add(1) })

	w.handleError(fsnotify.ErrEventOverflow)
	if got := w.OverflowCount(); got != 1 {
		t.Errorf("OverflowCount after overflow: got %d, want 1", got)
	}
	if got := called.Load(); got != 1 {
		t.Errorf("handler invocations after overflow: got %d, want 1", got)
	}

	// Non-overflow error: count must stay, handler must NOT fire.
	w.handleError(errors.New("some generic backend hiccup"))
	if got := w.OverflowCount(); got != 1 {
		t.Errorf("OverflowCount after non-overflow: got %d, want 1", got)
	}
	if got := called.Load(); got != 1 {
		t.Errorf("handler invocations after non-overflow: got %d, want 1", got)
	}
}

func TestWatcher_OverflowError_NoHandler_NoPanic(t *testing.T) {
	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer w.Close()

	// No handler registered — must not panic.
	w.handleError(fsnotify.ErrEventOverflow)
	if got := w.OverflowCount(); got != 1 {
		t.Errorf("OverflowCount: got %d, want 1", got)
	}
}

func TestWatcher_OverflowCount_SafeAfterClose(t *testing.T) {
	w, err := NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	w.handleError(fsnotify.ErrEventOverflow)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := w.OverflowCount(); got != 1 {
		t.Errorf("post-Close OverflowCount: got %d, want 1", got)
	}
}

func TestIsEditorTempFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{".main.go.swp", true},
		{".main.go.swo", true},
		{".main.go.swx", true},
		{"main.go~", true},
		{"scratch.tmp", true},
		{"#lock#", true},
		{"main.go", false},
		{"README.md", false},
		{"tempest.py", false}, // contains "temp" but no suffix match
	}
	for _, c := range cases {
		if got := isEditorTempFile(c.name); got != c.want {
			t.Errorf("isEditorTempFile(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
