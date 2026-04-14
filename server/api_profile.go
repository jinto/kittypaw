package server

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/jinto/kittypaw/core"
)

// GET /api/v1/profiles — list profiles with preset status.
func (s *Server) handleProfileList(w http.ResponseWriter, _ *http.Request) {
	profiles, err := s.store.ListActiveProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	base := s.session.BaseDir

	type profileEntry struct {
		ID           string `json:"id"`
		Description  string `json:"description"`
		Active       bool   `json:"active"`
		HasSoul      bool   `json:"has_soul"`
		PresetStatus string `json:"preset_status"`
		PresetID     string `json:"preset_id,omitempty"`
	}

	var entries []profileEntry
	for _, pm := range profiles {
		e := profileEntry{
			ID:          pm.ID,
			Description: pm.Description,
			Active:      pm.Active,
		}
		// Check if SOUL.md exists on disk.
		soulPath := filepath.Join(base, "profiles", pm.ID, "SOUL.md")
		if _, err := os.Stat(soulPath); err == nil {
			e.HasSoul = true
		}
		status := core.PresetStatus(base, pm.ID)
		switch status.Kind {
		case core.StatusPreset:
			e.PresetStatus = "preset"
			e.PresetID = status.PresetID
		case core.StatusCustom:
			e.PresetStatus = "custom"
			e.PresetID = status.PresetID
		default:
			e.PresetStatus = "unknown"
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []profileEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": entries})
}

// POST /api/v1/profiles — create a new profile.
func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		PresetID    string `json:"preset_id"`
		Nick        string `json:"nick"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if err := core.ValidateProfileID(req.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate preset before creating DB entry (prevent orphan rows).
	if req.PresetID != "" {
		if _, ok := core.Presets[req.PresetID]; !ok {
			writeError(w, http.StatusBadRequest, "unknown preset: "+req.PresetID)
			return
		}
	}

	// Create DB entry.
	if err := s.store.UpsertProfileMeta(req.ID, req.Description, "[]", "api"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Apply preset if specified (already validated above).
	if req.PresetID != "" {
		if err := core.ApplyPreset(s.session.BaseDir, req.ID, req.PresetID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "id": req.ID})
}

// POST /api/v1/profiles/{id}/activate — activate or switch to a profile.
// Optional JSON body: {"preset_id": "..."} applies a preset before activating.
func (s *Server) handleProfileActivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := core.ValidateProfileID(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Verify profile exists before activating.
	if _, exists, err := s.store.GetProfileMeta(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !exists {
		writeError(w, http.StatusNotFound, "profile not found: "+id)
		return
	}

	// Optional: apply preset if specified in body.
	var body struct {
		PresetID string `json:"preset_id"`
	}
	if r.ContentLength > 0 {
		if !decodeBody(w, r, &body) {
			return
		}
	}
	if body.PresetID != "" {
		if err := core.ApplyPreset(s.session.BaseDir, id, body.PresetID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if err := s.store.SetProfileActive(id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}
