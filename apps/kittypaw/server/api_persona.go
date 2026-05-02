package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/engine"
)

// GET /api/v1/persona/evolution — list pending evolutions.
func (s *Server) handleEvolutionList(w http.ResponseWriter, r *http.Request) {
	kvs, err := s.store.ListUserContextPrefix("evolution:pending:")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if kvs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"evolutions": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evolutions": kvs})
}

// POST /api/v1/persona/evolution/{id}/approve — apply evolution to SOUL.md.
func (s *Server) handleEvolutionApprove(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	if err := core.ValidateProfileID(profileID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid profile id")
		return
	}
	if err := engine.ApproveEvolution(s.session.BaseDir, s.store, profileID); err != nil {
		writeError(w, http.StatusBadRequest, "evolution approval failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// POST /api/v1/persona/evolution/{id}/reject — reject evolution proposal.
func (s *Server) handleEvolutionReject(w http.ResponseWriter, r *http.Request) {
	profileID := chi.URLParam(r, "id")
	if err := core.ValidateProfileID(profileID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid profile id")
		return
	}
	if err := engine.RejectEvolution(s.store, profileID); err != nil {
		writeError(w, http.StatusBadRequest, "evolution rejection failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
