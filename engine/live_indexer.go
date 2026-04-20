package engine

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LiveIndexer glues Watcher → Debouncer → Indexer. One LiveIndexer per
// tenant (D2). AddWorkspace registers a root with the underlying Watcher;
// filesystem events are debounced and translated into IndexFile / RemoveFile
// calls on the Indexer.
type LiveIndexer struct {
	watcher   *Watcher
	debouncer *Debouncer
	indexer   Indexer
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	mu         sync.RWMutex
	workspaces map[string]string // id -> absolute root
	closed     bool

	overflow      *overflowHandler
	recoveryCount atomic.Int64
}

// DefaultLiveInterval and DefaultLiveCap are the production debounce window
// and cap per D3 (500ms interval, 2s cap).
const (
	DefaultLiveInterval = 500 * time.Millisecond
	DefaultLiveCap      = 2 * time.Second

	// DefaultOverflowDebounce coalesces a burst of overflow signals into one
	// recovery cycle.
	DefaultOverflowDebounce = 500 * time.Millisecond
	// DefaultOverflowBackoff enforces a minimum gap between recovery cycles
	// so a persistently-overflowing kernel queue can't drive a reindex loop.
	DefaultOverflowBackoff = 30 * time.Second
)

// NewLiveIndexer creates a LiveIndexer. Returns an error if the underlying
// fsnotify watcher cannot be allocated (OS limit, permission, etc.) — the
// caller should fall back to lazy mode (workspace still indexable via
// explicit Reindex).
func NewLiveIndexer(indexer Indexer, interval, cap time.Duration) (*LiveIndexer, error) {
	watcher, err := NewWatcher()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &LiveIndexer{
		watcher:    watcher,
		indexer:    indexer,
		ctx:        ctx,
		cancel:     cancel,
		workspaces: make(map[string]string),
	}
	l.debouncer = NewDebouncer(RealClock{}, interval, cap, l.flush)
	l.overflow = newOverflowHandler(RealClock{}, DefaultOverflowDebounce, DefaultOverflowBackoff, l.runRecovery)
	watcher.SetOverflowHandler(l.overflow.Signal)
	return l, nil
}

// AddWorkspace registers a workspace root with the watcher and records it
// for flush-time routing. Returns an error if AddWatch fails on the root
// directory — callers treat this as a signal to enter lazy mode.
func (l *LiveIndexer) AddWorkspace(workspaceID, rootPath string) error {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return err
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return errors.New("live indexer closed")
	}
	l.workspaces[workspaceID] = abs
	l.mu.Unlock()

	if err := l.watcher.AddWorkspace(workspaceID, rootPath); err != nil {
		l.mu.Lock()
		delete(l.workspaces, workspaceID)
		l.mu.Unlock()
		return err
	}
	slog.Info("live indexer: workspace added", "workspace_id", workspaceID, "root", abs)
	return nil
}

// PartialFailures returns the number of subtree add failures observed by the
// underlying watcher since process start. Surfaces the "healthy-looking but
// partially broken" state that fsnotify.Add errors otherwise hide. Atomic —
// safe before Start and after Close.
func (l *LiveIndexer) PartialFailures() int64 {
	return l.watcher.PartialAddFailures()
}

// RecoveryCount returns how many overflow-triggered full-reindex cycles have
// completed since process start. Atomic — safe before Start and after Close.
func (l *LiveIndexer) RecoveryCount() int64 {
	return l.recoveryCount.Load()
}

// RemoveWorkspace stops watching a workspace and drops its routing entry.
// Pending debounced events for files under this root will flush but find no
// workspace at flush time, becoming no-ops.
func (l *LiveIndexer) RemoveWorkspace(workspaceID string) {
	l.mu.Lock()
	delete(l.workspaces, workspaceID)
	l.mu.Unlock()
	l.watcher.RemoveWorkspace(workspaceID)
	slog.Info("live indexer: workspace removed", "workspace_id", workspaceID)
}

// Start launches the watcher and the event-consumer goroutine. Safe to call
// at most once. Serialized with Close via l.mu — if Close landed first the
// call is a no-op, preventing watcher.Start racing with watcher.Close when a
// tenant is torn down before its startup goroutine finishes.
func (l *LiveIndexer) Start() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.watcher.Start()
	l.wg.Add(1)
	go l.consume()
}

func (l *LiveIndexer) consume() {
	defer l.wg.Done()
	for ev := range l.watcher.Events() {
		l.debouncer.Schedule(ev.AbsPath, ev.Op)
	}
}

// flush is the Debouncer callback. Runs in a timer goroutine; must not hold
// l.mu while calling IndexFile (which may block on I/O).
func (l *LiveIndexer) flush(path string, op DebounceOp) {
	wsID, rootPath := l.workspaceFor(path)
	if wsID == "" {
		return // workspace removed while debouncing
	}

	switch op {
	case DebounceIndex:
		if err := l.indexer.IndexFile(l.ctx, wsID, rootPath, path); err != nil {
			if l.ctx.Err() == nil {
				slog.Warn("live index: IndexFile failed", "path", path, "error", err)
			}
		} else {
			slog.Debug("indexed live", "path", path, "op", "index")
		}
	case DebounceRemove:
		if err := l.indexer.RemoveFile(wsID, path); err != nil {
			if l.ctx.Err() == nil {
				slog.Warn("live index: RemoveFile failed", "path", path, "error", err)
			}
		} else {
			slog.Debug("indexed live", "path", path, "op", "remove")
		}
	}
}

// workspaceFor returns the workspace that owns path. Longest-root-prefix
// wins to support nested workspaces.
func (l *LiveIndexer) workspaceFor(path string) (string, string) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var bestID, bestRoot string
	for id, root := range l.workspaces {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			if len(root) > len(bestRoot) {
				bestID = id
				bestRoot = root
			}
		}
	}
	return bestID, bestRoot
}

// runRecovery is the overflowHandler callback. fsnotify's kernel queue has
// overflowed (Linux IN_Q_OVERFLOW, Windows buffer overrun) and we don't know
// which watch was affected — the only safe option is a full Reindex of every
// workspace managed by this LiveIndexer. Reindex reuses the walk + upsert +
// stale cleanup path, which both re-adds any files that appeared during the
// blackout and removes entries for files deleted during it.
func (l *LiveIndexer) runRecovery() {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return
	}
	snapshot := make(map[string]string, len(l.workspaces))
	for id, root := range l.workspaces {
		snapshot[id] = root
	}
	l.mu.RUnlock()

	slog.Warn("live indexer: overflow recovery started", "workspaces", len(snapshot))
	for id, root := range snapshot {
		select {
		case <-l.ctx.Done():
			return
		default:
		}
		if _, err := l.indexer.Reindex(l.ctx, id, root); err != nil {
			if l.ctx.Err() == nil {
				slog.Warn("live indexer: overflow recovery reindex failed",
					"workspace_id", id, "error", err)
			}
			continue
		}
		slog.Info("live indexer: overflow recovery reindexed workspace",
			"workspace_id", id, "root", root)
	}
	l.recoveryCount.Add(1)
}

// Close tears down the pipeline in an order that guarantees no in-flight
// IndexFile or recovery call outlives Close. Cancel ctx first so any active
// flush / Reindex aborts ASAP, then stop accepting fsnotify events, drain
// consume, wait on pending debounce timers, and finally wait on pending
// overflow recoveries. Returning only after this sequence lets TenantDeps
// safely close the underlying store.
func (l *LiveIndexer) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()

	l.cancel()
	err := l.watcher.Close()
	l.wg.Wait()
	l.debouncer.Close()
	l.overflow.Close()
	return err
}
