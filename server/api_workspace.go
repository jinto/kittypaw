package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jinto/kittypaw/store"
)

// ---------------------------------------------------------------------------
// GET /api/v1/workspaces
// ---------------------------------------------------------------------------

func (s *Server) handleWorkspacesList(w http.ResponseWriter, _ *http.Request) {
	wss, err := s.store.ListWorkspaces()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type wsJSON struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		RootPath  string `json:"root_path"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]wsJSON, len(wss))
	for i, ws := range wss {
		out[i] = wsJSON{ID: ws.ID, Name: ws.Name, RootPath: ws.RootPath, CreatedAt: ws.CreatedAt}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// POST /api/v1/workspaces
// ---------------------------------------------------------------------------

func (s *Server) handleWorkspacesCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Path == "" || !filepath.IsAbs(body.Path) {
		writeError(w, http.StatusBadRequest, "absolute path is required")
		return
	}

	// Canonicalize BEFORE validation to prevent /../ path traversal.
	canonical := filepath.Clean(body.Path)
	if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = resolved
	}

	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "path does not exist or is not a directory")
		return
	}

	name := body.Name
	if name == "" {
		name = filepath.Base(canonical)
	}

	id := fmt.Sprintf("ws-%d", time.Now().UnixNano())
	ws := &store.Workspace{ID: id, Name: name, RootPath: canonical}
	if err := s.store.SaveWorkspace(ws); err != nil {
		writeError(w, http.StatusConflict, "workspace already registered or path conflict")
		return
	}

	if err := s.session.RefreshAllowedPaths(); err != nil {
		slog.Error("workspace create: cache refresh failed", "error", err)
	}

	// Trigger async background indexing.
	if s.session.Indexer != nil {
		go func() {
			if _, err := s.session.Indexer.Index(context.Background(), id, canonical); err != nil {
				slog.Warn("workspace create: indexing failed", "id", id, "error", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "name": name, "root_path": canonical,
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/v1/workspaces/{id}
// ---------------------------------------------------------------------------

func (s *Server) handleWorkspacesDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "workspace id is required")
		return
	}

	// Remove index before deleting workspace.
	if s.session.Indexer != nil {
		if err := s.session.Indexer.Remove(id); err != nil {
			slog.Warn("workspace delete: index removal failed", "id", id, "error", err)
		}
	}

	if err := s.store.DeleteWorkspace(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Fail-closed: if cache refresh fails after delete, deny all paths
	// rather than keeping stale (potentially revoked) permissions.
	if err := s.session.RefreshAllowedPaths(); err != nil {
		slog.Error("workspace delete: cache refresh failed, denying all paths", "error", err)
		s.session.ClearAllowedPaths()
	}

	w.WriteHeader(http.StatusNoContent)
}
