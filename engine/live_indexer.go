package engine

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
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
}

// DefaultLiveInterval and DefaultLiveCap are the production debounce window
// and cap per D3 (500ms interval, 2s cap).
const (
	DefaultLiveInterval = 500 * time.Millisecond
	DefaultLiveCap      = 2 * time.Second
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

// Close tears down the pipeline in an order that guarantees no in-flight
// IndexFile call outlives Close: cancel ctx first so any active flush
// callback that already entered IndexFile aborts its tx ASAP, then stop
// accepting events, drain consume, and wait on pending debounce timers
// (debouncer.Close now waits for in-flight flush callbacks). Returning
// only after this sequence lets TenantDeps.Close safely close the store.
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
	return err
}
