package server

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jinto/kittypaw/engine"
)

// GET /api/v1/reflection — list suggestion candidates.
func (s *Server) handleReflectionList(w http.ResponseWriter, r *http.Request) {
	kvs, err := s.store.ListUserContextPrefix("suggest_candidate:")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if kvs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"candidates": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": kvs})
}

// POST /api/v1/reflection/{key}/approve — approve a suggestion and create skill.
func (s *Server) handleReflectionApprove(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	fullKey := "suggest_candidate:" + key

	value, exists, err := s.store.GetUserContext(fullKey)
	if err != nil || !exists {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}

	// Parse label from "label|count|cron" format.
	desc := value
	if parts := strings.SplitN(value, "|", 2); len(parts) >= 1 {
		desc = parts[0]
	}

	// Generate skill via teach pipeline.
	result, err := engine.HandleTeach(r.Context(), desc, "reflection", s.session)
	if err != nil {
		slog.Error("reflection approve: teach failed", "key", key, "error", err)
		writeError(w, http.StatusInternalServerError, "skill generation failed")
		return
	}

	if err := engine.ApproveSkill(s.session.BaseDir, result); err != nil {
		slog.Error("reflection approve: save failed", "key", key, "error", err)
		writeError(w, http.StatusInternalServerError, "skill save failed")
		return
	}

	// Remove candidate.
	_, _ = s.store.DeleteUserContext(fullKey)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"skill_name": result.SkillName,
	})
}

// POST /api/v1/reflection/{key}/reject — reject a suggestion permanently.
func (s *Server) handleReflectionReject(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	fullKey := "suggest_candidate:" + key

	value, exists, err := s.store.GetUserContext(fullKey)
	if err != nil || !exists {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}

	// Store rejection.
	rejKey := "rejected_intent:" + key
	_ = s.store.SetUserContext(rejKey, value, "user_rejection")

	// Remove candidate.
	_, _ = s.store.DeleteUserContext(fullKey)

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// POST /api/v1/reflection/clear — clear all candidates.
func (s *Server) handleReflectionClear(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.store.DeleteUserContextPrefix("suggest_candidate:")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// POST /api/v1/reflection/run — trigger reflection cycle manually.
//
// Routes through Scheduler.TriggerReflectionTick so the manual run
// exercises the same path as the scheduled daily tick: reflection
// analysis, evolution check, AND weekly-report delivery. Calling
// engine.RunReflectionCycle directly (the prior shape) silently
// skipped weekly delivery, so the docs/index.html promise of the
// weekly Telegram report could only be tested by waiting for the
// real Sunday cron firing.
func (s *Server) handleReflectionRun(w http.ResponseWriter, r *http.Request) {
	if s.scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler not initialized")
		return
	}
	s.scheduler.TriggerReflectionTick(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// GET /api/v1/reflection/weekly-report — on-demand weekly report.
func (s *Server) handleWeeklyReport(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.store.ListUserContextPrefix("topic_pref:")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	report := engine.BuildWeeklyReport(prefs)
	writeJSON(w, http.StatusOK, map[string]any{"report": report})
}
