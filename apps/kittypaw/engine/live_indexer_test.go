package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// mockIndexer records IndexFile / RemoveFile / Reindex calls for assertions.
type mockIndexer struct {
	mu          sync.Mutex
	indexed     map[string]int
	removed     map[string]int
	reindexed   map[string]int
	indexErr    error
	reindexHook func(ctx context.Context) // optional: called inside Reindex for blocking/ctx tests
}

func newMockIndexer() *mockIndexer {
	return &mockIndexer{
		indexed:   map[string]int{},
		removed:   map[string]int{},
		reindexed: map[string]int{},
	}
}

func (m *mockIndexer) Index(ctx context.Context, workspaceID, rootPath string) (*IndexResult, error) {
	return &IndexResult{}, nil
}

func (m *mockIndexer) IndexFile(ctx context.Context, workspaceID, rootPath, absPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexed[absPath]++
	return m.indexErr
}

func (m *mockIndexer) Remove(workspaceID string) error { return nil }

func (m *mockIndexer) RemoveFile(workspaceID, absPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed[absPath]++
	return nil
}

func (m *mockIndexer) Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error) {
	return &SearchResult{}, nil
}

func (m *mockIndexer) Stats(ctx context.Context, opts StatsOptions) (*IndexStats, error) {
	return &IndexStats{}, nil
}

func (m *mockIndexer) Reindex(ctx context.Context, workspaceID, rootPath string) (*IndexResult, error) {
	m.mu.Lock()
	m.reindexed[workspaceID]++
	hook := m.reindexHook
	m.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	return &IndexResult{}, nil
}

func (m *mockIndexer) reindexCount(wsID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reindexed[wsID]
}

func (m *mockIndexer) Close() error { return nil }

func (m *mockIndexer) indexCount(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.indexed[path]
}

func (m *mockIndexer) removeCount(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removed[path]
}

func TestLiveIndexer_EndToEnd_CreateTriggersIndex(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	mi := newMockIndexer()
	// Short interval for fast tests.
	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	defer li.Close()

	if err := li.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	li.Start()

	target := filepath.Join(dir, "live.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mi.indexCount(target) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if mi.indexCount(target) < 1 {
		t.Fatalf("expected IndexFile called for %s, got %d", target, mi.indexCount(target))
	}
}

func TestLiveIndexer_EndToEnd_DeleteTriggersRemove(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	target := filepath.Join(dir, "vanish.txt")
	if err := os.WriteFile(target, []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	mi := newMockIndexer()
	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	defer li.Close()

	if err := li.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	li.Start()

	// Let any startup events settle.
	time.Sleep(80 * time.Millisecond)

	if err := os.Remove(target); err != nil {
		t.Fatalf("remove: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mi.removeCount(target) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if mi.removeCount(target) < 1 {
		t.Fatalf("expected RemoveFile called for %s, got %d", target, mi.removeCount(target))
	}
}

func TestLiveIndexer_Debounces_ContinuousWrites(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	target := filepath.Join(dir, "busy.txt")

	mi := newMockIndexer()
	// 100ms interval, 500ms cap.
	li, err := NewLiveIndexer(mi, 100*time.Millisecond, 500*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	defer li.Close()
	_ = li.AddWorkspace("ws", dir)
	li.Start()

	// 10 rapid writes within 200ms.
	for i := range 10 {
		_ = os.WriteFile(target, []byte{byte(i)}, 0o644)
		time.Sleep(20 * time.Millisecond)
	}

	// Wait beyond the cap.
	time.Sleep(700 * time.Millisecond)

	// Debouncer should coalesce to 1-2 flushes (depending on cap boundary),
	// never 10.
	count := mi.indexCount(target)
	if count == 0 {
		t.Fatalf("no flush after 10 writes")
	}
	if count > 3 {
		t.Errorf("expected <=3 coalesced flushes, got %d", count)
	}
}

func TestLiveIndexer_RemoveWorkspace_StopsRouting(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	mi := newMockIndexer()
	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	defer li.Close()

	_ = li.AddWorkspace("ws", dir)
	li.Start()

	li.RemoveWorkspace("ws")

	// Any writes after RemoveWorkspace should not trigger IndexFile.
	target := filepath.Join(dir, "after.txt")
	_ = os.WriteFile(target, []byte("x"), 0o644)

	time.Sleep(300 * time.Millisecond)
	if c := mi.indexCount(target); c != 0 {
		t.Errorf("IndexFile called after RemoveWorkspace: %d", c)
	}
}

func TestLiveIndexer_CloseDropsPending(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	mi := newMockIndexer()
	// Long interval so pending writes aren't flushed before Close.
	li, err := NewLiveIndexer(mi, 5*time.Second, 10*time.Second)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	_ = li.AddWorkspace("ws", dir)
	li.Start()

	target := filepath.Join(dir, "pending.txt")
	_ = os.WriteFile(target, []byte("x"), 0o644)

	// Give Watcher time to see the event and Debouncer to Schedule.
	time.Sleep(80 * time.Millisecond)

	if err := li.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// Wait longer than the debounce interval — confirm Close dropped it.
	time.Sleep(200 * time.Millisecond)

	if c := mi.indexCount(target); c != 0 {
		t.Errorf("pending flush fired after Close: count=%d", c)
	}
}

func TestLiveIndexer_PartialFailures_DelegatesToWatcher(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod-based unreachable-dir trick is ineffective as root")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "no_access")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.Chmod(sub, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	mi := newMockIndexer()
	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}

	if err := li.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	if got := li.PartialFailures(); got < 1 {
		t.Errorf("PartialFailures via delegate: got %d, want >= 1", got)
	}

	// After Close, PartialFailures must still be callable without panic.
	if err := li.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = li.PartialFailures()
}

// TestLiveIndexer_DirRemove_CascadesToIndexer verifies the wiring so a
// fsnotify-emitted dir Remove reaches indexer.RemoveFile with the dir's
// path — the store-level cascade is covered by store tests. This is the
// canary for the "prefix delete" feature in live-indexer form.
func TestLiveIndexer_DirRemove_CascadesToIndexer(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	sub := filepath.Join(dir, "vanishing")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	mi := newMockIndexer()
	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	defer li.Close()
	if err := li.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	li.Start()
	_ = os.WriteFile(filepath.Join(sub, "b.txt"), []byte("y"), 0o644)

	// Settle pre-existing events.
	time.Sleep(300 * time.Millisecond)

	if err := os.RemoveAll(sub); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mi.removeCount(sub) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c := mi.removeCount(sub); c < 1 {
		t.Fatalf("expected RemoveFile called with dir path %s, got %d", sub, c)
	}
}

// TestLiveIndexer_Overflow_ReindexesAllWorkspaces verifies the full wiring:
// Watcher.handleError(ErrEventOverflow) → overflowHandler.Signal → timer
// fires → runRecovery → Reindex for every registered workspace. Uses
// fakeClock to drive the handler deterministically.
func TestLiveIndexer_Overflow_ReindexesAllWorkspaces(t *testing.T) {
	mi := newMockIndexer()
	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	defer li.Close()

	// Swap in a fakeClock-driven overflow handler for deterministic timing.
	li.overflow.Close()
	fc := newFakeClock()
	li.overflow = newOverflowHandler(fc, 10*time.Millisecond, 100*time.Millisecond, li.runRecovery)
	li.watcher.SetOverflowHandler(li.overflow.Signal)

	dirA, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval A: %v", err)
	}
	dirB, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval B: %v", err)
	}
	if err := li.AddWorkspace("A", dirA); err != nil {
		t.Fatalf("AddWorkspace A: %v", err)
	}
	if err := li.AddWorkspace("B", dirB); err != nil {
		t.Fatalf("AddWorkspace B: %v", err)
	}

	li.overflow.Signal()
	fc.Advance(11 * time.Millisecond)
	li.overflow.inFlight.Wait()

	if got := mi.reindexCount("A"); got != 1 {
		t.Errorf("Reindex(A): got %d, want 1", got)
	}
	if got := mi.reindexCount("B"); got != 1 {
		t.Errorf("Reindex(B): got %d, want 1", got)
	}
	if got := li.RecoveryCount(); got != 1 {
		t.Errorf("RecoveryCount: got %d, want 1", got)
	}
}

// TestLiveIndexer_OverflowWiring_EndToEnd verifies Watcher.handleError routes
// into the overflow handler without needing to swap clocks. The real handler
// runs with a 500ms debounce, so we wait for the fire/run cycle.
func TestLiveIndexer_OverflowWiring_EndToEnd(t *testing.T) {
	mi := newMockIndexer()
	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	defer li.Close()

	// Shorten the real-clock handler so the test finishes fast while still
	// exercising the production RealClock path.
	li.overflow.Close()
	li.overflow = newOverflowHandler(RealClock{}, 20*time.Millisecond, 1*time.Second, li.runRecovery)
	li.watcher.SetOverflowHandler(li.overflow.Signal)

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if err := li.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	// Inject overflow directly on the Watcher's error path.
	li.watcher.handleError(fsnotify.ErrEventOverflow)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mi.reindexCount("ws") >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := mi.reindexCount("ws"); got != 1 {
		t.Errorf("Reindex: got %d, want 1", got)
	}
}

// TestLiveIndexer_Close_DuringRecovery_CtxCancelled verifies Close cancels
// ctx so an in-flight Reindex can return early, preventing goroutine leak.
func TestLiveIndexer_Close_DuringRecovery_CtxCancelled(t *testing.T) {
	mi := newMockIndexer()
	entered := make(chan struct{})
	mi.reindexHook = func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
	}

	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}

	// Shorten the overflow handler so the test finishes fast.
	li.overflow.Close()
	li.overflow = newOverflowHandler(RealClock{}, 5*time.Millisecond, 1*time.Second, li.runRecovery)
	li.watcher.SetOverflowHandler(li.overflow.Signal)

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if err := li.AddWorkspace("ws", dir); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	li.watcher.handleError(fsnotify.ErrEventOverflow)

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Reindex hook never entered")
	}

	done := make(chan struct{})
	go func() {
		_ = li.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after ctx cancel — likely goroutine leak")
	}
}

// TestLiveIndexer_Close_NoGoroutineLeak_WithOverflow drives the create/close
// cycle with an overflow signal in flight each iteration to ensure neither
// the overflow handler nor runRecovery leaks goroutines.
func TestLiveIndexer_Close_NoGoroutineLeak_WithOverflow(t *testing.T) {
	before := runtime.NumGoroutine()

	for range 3 {
		mi := newMockIndexer()
		li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
		if err != nil {
			t.Skipf("live indexer unavailable: %v", err)
		}

		li.overflow.Close()
		li.overflow = newOverflowHandler(RealClock{}, 5*time.Millisecond, 1*time.Second, li.runRecovery)
		li.watcher.SetOverflowHandler(li.overflow.Signal)

		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("eval: %v", err)
		}
		_ = li.AddWorkspace("ws", dir)
		li.Start()

		li.watcher.handleError(fsnotify.ErrEventOverflow)

		if err := li.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}

	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+3 {
		t.Errorf("goroutine leak with overflow: before=%d after=%d", before, after)
	}
}

func TestLiveIndexer_AddWorkspaceAfterClose_Errors(t *testing.T) {
	mi := newMockIndexer()
	li, err := NewLiveIndexer(mi, 50*time.Millisecond, 200*time.Millisecond)
	if err != nil {
		t.Skipf("live indexer unavailable: %v", err)
	}
	_ = li.Close()

	err = li.AddWorkspace("ws", t.TempDir())
	if err == nil {
		t.Errorf("expected error after Close, got nil")
	}
}
