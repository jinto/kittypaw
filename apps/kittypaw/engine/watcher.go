package engine

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// WatchEvent is the Debouncer-facing view of a filesystem change.
type WatchEvent struct {
	WorkspaceID string
	RootPath    string
	AbsPath     string
	Op          DebounceOp
}

// Watcher wraps fsnotify with multi-workspace support. One Watcher per tenant
// hosts all of that tenant's workspaces (D2). Directories are added
// recursively at AddWorkspace and whenever new directories are created at
// runtime. Excluded directories (.git, node_modules, etc.) and editor temp
// files are filtered here so the Debouncer never sees them.
type Watcher struct {
	fs      *fsnotify.Watcher
	eventCh chan WatchEvent
	stopCh  chan struct{}
	wg      sync.WaitGroup

	mu         sync.Mutex
	workspaces map[string]string // workspace_id -> absolute rootPath
	closed     bool
}

// NewWatcher creates a Watcher backed by fsnotify. Returns an error if the
// underlying watcher cannot be created (OS limit hit, etc.) — callers should
// fall back to lazy mode.
func NewWatcher() (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		fs:         fs,
		eventCh:    make(chan WatchEvent, 256),
		stopCh:     make(chan struct{}),
		workspaces: make(map[string]string),
	}, nil
}

// Events returns the channel that yields filtered filesystem events.
func (w *Watcher) Events() <-chan WatchEvent { return w.eventCh }

// Errors exposes fsnotify's internal error channel for observability.
func (w *Watcher) Errors() <-chan error { return w.fs.Errors }

// AddWorkspace registers a workspace and recursively adds its directory tree.
// A first-directory Add failure returns the error so the caller can drop
// into lazy mode; subsequent subdirectory errors are logged and skipped
// (usually transient permission issues).
func (w *Watcher) AddWorkspace(workspaceID, rootPath string) error {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return err
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return errors.New("watcher closed")
	}
	w.workspaces[workspaceID] = absRoot
	w.mu.Unlock()

	return filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			slog.Debug("watcher: walk error", "path", path, "error", werr)
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != absRoot && excludedDirs[d.Name()] {
			return filepath.SkipDir
		}
		if addErr := w.fs.Add(path); addErr != nil {
			if path == absRoot {
				return addErr // propagate — root add failure is terminal
			}
			slog.Debug("watcher: add subdir failed", "path", path, "error", addErr)
		}
		return nil
	})
}

// RemoveWorkspace unregisters a workspace. Best-effort Remove on each dir;
// fsnotify also drops entries automatically when dirs are deleted.
func (w *Watcher) RemoveWorkspace(workspaceID string) {
	w.mu.Lock()
	root, ok := w.workspaces[workspaceID]
	delete(w.workspaces, workspaceID)
	w.mu.Unlock()
	if !ok {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || !d.IsDir() {
			return nil
		}
		_ = w.fs.Remove(path)
		return nil
	})
}

// Start launches the background event dispatch goroutine.
func (w *Watcher) Start() {
	w.wg.Add(1)
	go w.run()
}

// Close stops dispatch, closes fsnotify, and drains pending events.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()

	close(w.stopCh)
	err := w.fs.Close()
	w.wg.Wait()
	close(w.eventCh)
	return err
}

func (w *Watcher) run() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			w.handleEvent(ev)
		case err, ok := <-w.fs.Errors:
			// Drain Errors unconditionally — a blocked error send on the
			// fsnotify side would freeze the event loop. Kernel-queue
			// overflow (inotify IN_Q_OVERFLOW) means we have already lost
			// events for this workspace; log so operators can trigger a
			// manual Reindex. No automatic recovery in v2.
			if !ok {
				return
			}
			slog.Warn("watcher: backend error", "error", err)
		}
	}
}

func (w *Watcher) handleEvent(ev fsnotify.Event) {
	name := filepath.Base(ev.Name)
	if excludedFiles[name] {
		return
	}
	if isEditorTempFile(name) {
		return
	}

	wsID, rootPath := w.workspaceFor(ev.Name)
	if wsID == "" {
		return
	}

	// Remove/Rename first — a Create|Write combined op on an overwritten file
	// still wants Index semantics, but a pure Remove/Rename does not.
	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		// Don't stat — file is gone. Send Remove; if it was a dir, RemoveFile
		// is a no-op at the file table level. For a new path after rename,
		// fsnotify emits a separate Create event.
		w.send(wsID, rootPath, ev.Name, DebounceRemove)
		return
	}

	if ev.Has(fsnotify.Create) {
		info, err := os.Stat(ev.Name)
		if err != nil {
			return
		}
		if info.IsDir() {
			if excludedDirs[name] {
				return
			}
			// Newly-created dir: add to watcher and re-walk to catch any
			// files that landed before we wired up the watch.
			if addErr := w.fs.Add(ev.Name); addErr != nil {
				slog.Debug("watcher: add runtime dir failed", "path", ev.Name, "error", addErr)
				return
			}
			w.walkAndIndex(wsID, rootPath, ev.Name)
			return
		}
		w.send(wsID, rootPath, ev.Name, DebounceIndex)
		return
	}

	if ev.Has(fsnotify.Write) {
		w.send(wsID, rootPath, ev.Name, DebounceIndex)
		return
	}
	// Chmod or any residual op: ignore.
}

// walkAndIndex sends Index events for every file inside a newly-created
// directory and adds any subdirectories to the watcher. Called when
// handleEvent detects a Create on a directory.
func (w *Watcher) walkAndIndex(wsID, rootPath, dir string) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			if path != dir {
				_ = w.fs.Add(path)
			}
			return nil
		}
		if excludedFiles[d.Name()] || isEditorTempFile(d.Name()) {
			return nil
		}
		w.send(wsID, rootPath, path, DebounceIndex)
		return nil
	})
}

// workspaceFor returns the (id, root) for the workspace that owns path.
// Longest-root-prefix match wins so nested workspaces still route correctly.
func (w *Watcher) workspaceFor(path string) (string, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var bestID, bestRoot string
	for id, root := range w.workspaces {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			if len(root) > len(bestRoot) {
				bestID = id
				bestRoot = root
			}
		}
	}
	return bestID, bestRoot
}

// send delivers a WatchEvent or drops it if Close has been called.
func (w *Watcher) send(wsID, rootPath, path string, op DebounceOp) {
	select {
	case w.eventCh <- WatchEvent{WorkspaceID: wsID, RootPath: rootPath, AbsPath: path, Op: op}:
	case <-w.stopCh:
	}
}

// isEditorTempFile filters vim/emacs/VSCode temporary files that shouldn't
// hit the index. Suffix-based because vim puts .swp *inside* the target
// directory as `.file.swp`.
func isEditorTempFile(name string) bool {
	for _, suffix := range []string{".swp", ".swo", ".swx", ".tmp"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	if strings.HasSuffix(name, "~") {
		return true
	}
	if strings.HasPrefix(name, "#") && strings.HasSuffix(name, "#") {
		return true
	}
	return false
}
