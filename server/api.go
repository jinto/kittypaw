package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/engine"
)

var safeSkillName = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

// writeJSON serialises v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON encode", "error", err)
	}
}

// writeError writes a structured {"error": msg} response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeBody deserialises the request body into dst and reports errors.
// Limits request body to 1 MB to prevent memory exhaustion.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// GET /health
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": "gopaw"})
}

// ---------------------------------------------------------------------------
// GET /api/v1/status
// ---------------------------------------------------------------------------

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	stats, err := s.store.TodayStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total_runs":   stats.TotalRuns,
		"successful":   stats.Successful,
		"failed":       stats.Failed,
		"auto_retries": stats.AutoRetries,
		"total_tokens": stats.TotalTokens,
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/executions
// ---------------------------------------------------------------------------

func (s *Server) handleExecutions(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	skill := r.URL.Query().Get("skill")
	if skill != "" && !safeSkillName.MatchString(skill) {
		writeError(w, http.StatusBadRequest, "invalid skill name")
		return
	}

	var (
		execs any
		err   error
	)
	if skill != "" {
		execs, err = s.store.SearchExecutions(skill, limit)
	} else {
		execs, err = s.store.RecentExecutions(limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if execs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"executions": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": execs})
}

// ---------------------------------------------------------------------------
// GET /api/v1/agents
// ---------------------------------------------------------------------------

func (s *Server) handleAgents(w http.ResponseWriter, _ *http.Request) {
	agents, err := s.store.ListAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if agents == nil {
		writeJSON(w, http.StatusOK, map[string]any{"agents": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

// ---------------------------------------------------------------------------
// GET /api/v1/skills
// ---------------------------------------------------------------------------

func (s *Server) handleSkills(w http.ResponseWriter, _ *http.Request) {
	skills, err := core.LoadAllSkills()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type skillItem struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
		Version     uint32 `json:"version"`
		Trigger     string `json:"trigger"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}
	items := make([]skillItem, 0, len(skills))
	for _, s := range skills {
		items = append(items, skillItem{
			Name:        s.Skill.Name,
			Description: s.Skill.Description,
			Enabled:     s.Skill.Enabled,
			Version:     s.Skill.Version,
			Trigger:     s.Skill.Trigger.Type,
			CreatedAt:   s.Skill.CreatedAt,
			UpdatedAt:   s.Skill.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": items})
}

// ---------------------------------------------------------------------------
// POST /api/v1/skills/run
// ---------------------------------------------------------------------------

func (s *Server) handleSkillsRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Build a synthetic event that triggers the named skill.
	payload := core.ChatPayload{
		ChatID: "api",
		Text:   "/run " + body.Name,
	}
	raw, _ := json.Marshal(payload)
	event := core.Event{Type: core.EventWebChat, Payload: raw}

	output, err := s.session.Run(r.Context(), event, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": output})
}

// ---------------------------------------------------------------------------
// POST /api/v1/skills/teach
// ---------------------------------------------------------------------------

func (s *Server) handleSkillsTeach(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Description string `json:"description"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Description == "" {
		writeError(w, http.StatusBadRequest, "description is required")
		return
	}

	result, err := engine.HandleTeach(r.Context(), body.Description, "api", s.session)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// POST /api/v1/skills/teach/approve
// ---------------------------------------------------------------------------

func (s *Server) handleTeachApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Code        string `json:"code"`
		Trigger     string `json:"trigger"`
		Schedule    string `json:"schedule"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Name == "" || body.Code == "" {
		writeError(w, http.StatusBadRequest, "name and code are required")
		return
	}

	trigger := body.Trigger
	if trigger == "" {
		trigger = "manual"
	}
	validTriggers := map[string]bool{"manual": true, "schedule": true, "keyword": true, "once": true, "natural": true}
	if !validTriggers[trigger] {
		writeError(w, http.StatusBadRequest, "invalid trigger type: "+trigger)
		return
	}
	// Validate syntax before saving — don't trust client-supplied code.
	ok, syntaxErr := engine.SyntaxCheck(r.Context(), body.Code, nil)
	if !ok {
		writeError(w, http.StatusBadRequest, "syntax check failed: "+syntaxErr)
		return
	}

	result := &engine.TeachResult{
		SkillName:   body.Name,
		Code:        body.Code,
		SyntaxOK:    true,
		Description: body.Description,
		Trigger:     core.SkillTrigger{Type: trigger, Cron: body.Schedule},
		Permissions: engine.DetectPermissions(body.Code),
	}
	if err := engine.ApproveSkill(result); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "name": body.Name})
}

// ---------------------------------------------------------------------------
// DELETE /api/v1/skills/{name}
// ---------------------------------------------------------------------------

func (s *Server) handleSkillsDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := core.DeleteSkill(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------------------------------------------------------------------------
// POST /api/v1/skills/{name}/enable
// ---------------------------------------------------------------------------

func (s *Server) handleSkillEnable(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := core.EnableSkill(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------------------------------------------------------------------------
// POST /api/v1/skills/{name}/disable
// ---------------------------------------------------------------------------

func (s *Server) handleSkillDisable(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := core.DisableSkill(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------------------------------------------------------------------------
// POST /api/v1/skills/{name}/explain
// ---------------------------------------------------------------------------

func (s *Server) handleSkillExplain(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	skill, code, err := core.LoadSkill(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if skill == nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}

	// Ask the LLM to explain the skill.
	prompt := "Explain the following JavaScript skill in plain language.\n\nName: " + skill.Name +
		"\nDescription: " + skill.Description +
		"\nCode:\n```js\n" + code + "\n```"

	messages := []core.LlmMessage{
		{Role: core.RoleUser, Content: prompt},
	}
	resp, err := s.session.Provider.Generate(r.Context(), messages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":        skill.Name,
		"explanation": resp.Content,
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/chat
// ---------------------------------------------------------------------------

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text      string `json:"text"`
		SessionID string `json:"session_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	sessionID := body.SessionID
	if sessionID == "" {
		sessionID = "api"
	}

	payload := core.ChatPayload{
		ChatID:    sessionID,
		Text:      body.Text,
		SessionID: sessionID,
	}
	raw, _ := json.Marshal(payload)
	event := core.Event{Type: core.EventWebChat, Payload: raw}

	output, err := s.session.Run(r.Context(), event, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"response": output})
}

// ---------------------------------------------------------------------------
// GET /api/v1/config/check
// ---------------------------------------------------------------------------

func (s *Server) handleConfigCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"channels":       len(s.config.Channels),
		"agents":         len(s.config.Agents),
		"models":         len(s.config.Models),
		"mcp_servers":    len(s.config.MCPServers),
		"profiles":       len(s.config.Profiles),
		"autonomy_level": string(s.config.AutonomyLevel),
		"features": map[string]any{
			"progressive_retry":  s.config.Features.ProgressiveRetry,
			"context_compaction": s.config.Features.ContextCompaction,
			"model_routing":      s.config.Features.ModelRouting,
			"background_agents":  s.config.Features.BackgroundAgents,
			"daily_token_limit":  s.config.Features.DailyTokenLimit,
		},
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/skills/{id}/fixes
// ---------------------------------------------------------------------------

func (s *Server) handleSkillFixes(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "id")
	if skillID == "" {
		writeError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	fixes, err := s.store.ListFixes(skillID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fixes == nil {
		writeJSON(w, http.StatusOK, map[string]any{"fixes": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fixes": fixes})
}

// ---------------------------------------------------------------------------
// POST /api/v1/fixes/{id}/approve
// ---------------------------------------------------------------------------

func (s *Server) handleFixApprove(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	fixID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid fix id")
		return
	}

	// Load fix to get skill_id and new code.
	fix, err := s.store.GetFix(fixID)
	if err != nil {
		writeError(w, http.StatusNotFound, "fix not found")
		return
	}

	// Load current disk code and skill metadata for stale check.
	skill, currentCode, loadErr := core.LoadSkill(fix.SkillID)
	if loadErr != nil || skill == nil {
		writeError(w, http.StatusNotFound, "skill not found on disk")
		return
	}

	applied, err := s.store.ApplyFix(fixID, currentCode)
	if err != nil {
		if strings.Contains(err.Error(), "stale") {
			writeError(w, http.StatusConflict, "code has changed since fix was generated")
			return
		}
		writeError(w, http.StatusInternalServerError, "fix application failed")
		return
	}
	if !applied {
		writeError(w, http.StatusNotFound, "fix not found or already applied")
		return
	}

	// Apply the new code to disk using the already-loaded skill.
	skill.Version++
	if saveErr := core.SaveSkill(skill, fix.NewCode); saveErr != nil {
		// Revert DB state since disk write failed.
		_ = s.store.RevertFix(fixID)
		writeError(w, http.StatusInternalServerError, "failed to save fix to disk")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------------------------------------------------------------------------
// GET /api/v1/suggestions
// ---------------------------------------------------------------------------

// Suggestions are stored as user-context keys prefixed with "suggestion:".
// Each value is a JSON blob with at minimum a skill_id and description.

func (s *Server) handleSuggestionsList(w http.ResponseWriter, _ *http.Request) {
	kvs, err := s.store.ListUserContextPrefix("suggestion:")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type suggestion struct {
		SkillID     string `json:"skill_id"`
		Description string `json:"description"`
	}
	items := make([]suggestion, 0, len(kvs))
	for _, kv := range kvs {
		var sg suggestion
		if err := json.Unmarshal([]byte(kv.Value), &sg); err != nil {
			// Fall back: treat the entire value as a description.
			sg = suggestion{SkillID: kv.Key, Description: kv.Value}
		}
		if sg.SkillID == "" {
			sg.SkillID = kv.Key
		}
		items = append(items, sg)
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": items})
}

// ---------------------------------------------------------------------------
// POST /api/v1/suggestions/{skill_id}/accept
// ---------------------------------------------------------------------------

func (s *Server) handleSuggestionsAccept(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "skill_id")
	if skillID == "" {
		writeError(w, http.StatusBadRequest, "skill_id is required")
		return
	}

	key := "suggestion:" + skillID
	val, ok, err := s.store.GetUserContext(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "suggestion not found")
		return
	}

	// Parse the suggestion so we can create the skill from it.
	var sg struct {
		Description string `json:"description"`
		Code        string `json:"code"`
		Trigger     string `json:"trigger"`
	}
	_ = json.Unmarshal([]byte(val), &sg)

	if sg.Code != "" {
		trigger := sg.Trigger
		if trigger == "" {
			trigger = "manual"
		}
		skill := &core.Skill{
			Name:        skillID,
			Version:     1,
			Description: sg.Description,
			Enabled:     true,
			Format:      core.SkillFormatNative,
			Trigger:     core.SkillTrigger{Type: trigger},
		}
		if err := core.SaveSkill(skill, sg.Code); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Remove the suggestion.
	_, _ = s.store.DeleteUserContext(key)

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "skill_id": skillID})
}

// ---------------------------------------------------------------------------
// POST /api/v1/suggestions/{skill_id}/dismiss
// ---------------------------------------------------------------------------

func (s *Server) handleSuggestionsDismiss(w http.ResponseWriter, r *http.Request) {
	skillID := chi.URLParam(r, "skill_id")
	if skillID == "" {
		writeError(w, http.StatusBadRequest, "skill_id is required")
		return
	}

	key := "suggestion:" + skillID
	deleted, err := s.store.DeleteUserContext(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "suggestion not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------------------------------------------------------------------------
// GET /api/v1/memory/search?q=...
// ---------------------------------------------------------------------------

func (s *Server) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	results, err := s.store.SearchExecutions(q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// ---------------------------------------------------------------------------
// GET /api/v1/agents/{id}/checkpoints
// ---------------------------------------------------------------------------

func (s *Server) handleCheckpointsList(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	cps, err := s.store.ListCheckpoints(agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cps == nil {
		writeJSON(w, http.StatusOK, map[string]any{"checkpoints": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkpoints": cps})
}

// ---------------------------------------------------------------------------
// POST /api/v1/agents/{id}/checkpoints
// ---------------------------------------------------------------------------

func (s *Server) handleCheckpointsCreate(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent id is required")
		return
	}

	var body struct {
		Label string `json:"label"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Label == "" {
		writeError(w, http.StatusBadRequest, "label is required")
		return
	}

	cpID, err := s.store.CreateCheckpoint(agentID, body.Label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"id":      cpID,
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/checkpoints/{id}/rollback
// ---------------------------------------------------------------------------

func (s *Server) handleCheckpointRollback(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	cpID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid checkpoint id")
		return
	}

	deleted, err := s.store.RollbackToCheckpoint(cpID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"turns_deleted": deleted,
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/users/link
// ---------------------------------------------------------------------------

func (s *Server) handleUsersLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GlobalUserID  string `json:"global_user_id"`
		Channel       string `json:"channel"`
		ChannelUserID string `json:"channel_user_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.GlobalUserID == "" || body.Channel == "" || body.ChannelUserID == "" {
		writeError(w, http.StatusBadRequest, "global_user_id, channel, and channel_user_id are required")
		return
	}
	if err := s.store.LinkIdentity(body.GlobalUserID, body.Channel, body.ChannelUserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------------------------------------------------------------------------
// GET /api/v1/users/{id}/identities
// ---------------------------------------------------------------------------

func (s *Server) handleUsersIdentities(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user id is required")
		return
	}

	ids, err := s.store.ListIdentities(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ids == nil {
		writeJSON(w, http.StatusOK, map[string]any{"identities": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"identities": ids})
}

// ---------------------------------------------------------------------------
// DELETE /api/v1/users/{id}/identities/{channel}
// ---------------------------------------------------------------------------

func (s *Server) handleUsersUnlink(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	channel := chi.URLParam(r, "channel")
	if userID == "" || channel == "" {
		writeError(w, http.StatusBadRequest, "user id and channel are required")
		return
	}
	if err := s.store.UnlinkIdentity(userID, channel); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------------------------------------------------------------------------
// POST /api/v1/reload
// ---------------------------------------------------------------------------

func (s *Server) handleReload(w http.ResponseWriter, _ *http.Request) {
	cfgPath, err := core.ConfigPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := core.LoadConfig(cfgPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reload failed: "+err.Error())
		return
	}

	// Swap the live config under lock. The session shares the pointer so it
	// sees the new values on the next Run() call.
	s.configMu.Lock()
	*s.config = *cfg
	s.configMu.Unlock()
	slog.Info("config reloaded")

	// Reconcile channels with the new config.
	result := map[string]any{"success": true}
	if s.spawner != nil {
		if err := s.spawner.Reconcile(cfg.Channels); err != nil {
			slog.Warn("reload: channel reconcile partial failure", "error", err)
			result["warnings"] = []string{err.Error()}
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// GET /api/v1/channels
// ---------------------------------------------------------------------------

func (s *Server) handleChannels(w http.ResponseWriter, _ *http.Request) {
	if s.spawner == nil {
		writeJSON(w, http.StatusOK, []ChannelStatus{})
		return
	}
	writeJSON(w, http.StatusOK, s.spawner.List())
}

