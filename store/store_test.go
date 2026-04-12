package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/jinto/gopaw/core"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenAndMigrate(t *testing.T) {
	st := openTestStore(t)

	var count int
	err := st.db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count)
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 14 {
		t.Fatalf("expected 14 migrations, got %d", count)
	}
}

func TestAgentStateRoundTrip(t *testing.T) {
	st := openTestStore(t)

	// LoadState for a non-existent agent returns nil, nil.
	got, err := st.LoadState("ghost")
	if err != nil {
		t.Fatalf("load ghost: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent agent")
	}

	// Save and reload.
	state := &core.AgentState{
		AgentID:      "agent-1",
		SystemPrompt: "You are helpful.",
		Turns: []core.ConversationTurn{
			{Role: core.RoleUser, Content: "hi", Timestamp: "2026-04-13 10:00:00"},
			{Role: core.RoleAssistant, Content: "hello", Code: "console.log(1)", Result: "1", Timestamp: "2026-04-13 10:00:01"},
		},
	}
	if err := st.SaveState(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	loaded, err := st.LoadState("agent-1")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded.AgentID != state.AgentID {
		t.Errorf("agent_id: got %q, want %q", loaded.AgentID, state.AgentID)
	}
	if loaded.SystemPrompt != state.SystemPrompt {
		t.Errorf("system_prompt: got %q, want %q", loaded.SystemPrompt, state.SystemPrompt)
	}
	if len(loaded.Turns) != 2 {
		t.Fatalf("turns len: got %d, want 2", len(loaded.Turns))
	}
	turn := loaded.Turns[1]
	if turn.Role != core.RoleAssistant || turn.Content != "hello" || turn.Code != "console.log(1)" || turn.Result != "1" {
		t.Errorf("turn[1] mismatch: %+v", turn)
	}
}

func TestAddTurnAndList(t *testing.T) {
	st := openTestStore(t)

	// AddTurn implicitly creates the agent.
	err := st.AddTurn("bot-a", &core.ConversationTurn{
		Role: core.RoleUser, Content: "ping", Timestamp: "2026-04-13 11:00:00",
	})
	if err != nil {
		t.Fatalf("add turn: %v", err)
	}

	agents, err := st.ListAgents()
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 || agents[0].AgentID != "bot-a" {
		t.Fatalf("expected [bot-a], got %+v", agents)
	}
	if agents[0].TurnCount != 1 {
		t.Errorf("turn count: got %d, want 1", agents[0].TurnCount)
	}

	// Add another user turn and a non-user turn.
	st.AddTurn("bot-a", &core.ConversationTurn{
		Role: core.RoleUser, Content: "pong", Timestamp: "2026-04-13 11:00:01",
	})
	st.AddTurn("bot-a", &core.ConversationTurn{
		Role: core.RoleAssistant, Content: "ack", Timestamp: "2026-04-13 11:00:02",
	})

	count, err := st.CountUserMessagesTotal()
	if err != nil {
		t.Fatalf("count user messages: %v", err)
	}
	if count != 2 {
		t.Errorf("user message count: got %d, want 2", count)
	}
}

func TestStorageKV(t *testing.T) {
	st := openTestStore(t)
	ns := "weather"

	// Get missing key.
	_, found, err := st.StorageGet(ns, "city")
	if err != nil {
		t.Fatalf("get missing: %v", err)
	}
	if found {
		t.Fatal("expected not found for missing key")
	}

	// Set and get.
	if err := st.StorageSet(ns, "city", "Seoul"); err != nil {
		t.Fatalf("set: %v", err)
	}
	val, found, err := st.StorageGet(ns, "city")
	if err != nil || !found {
		t.Fatalf("get after set: found=%v err=%v", found, err)
	}
	if val != "Seoul" {
		t.Errorf("value: got %q, want %q", val, "Seoul")
	}

	// Overwrite.
	st.StorageSet(ns, "city", "Busan")
	val, _, _ = st.StorageGet(ns, "city")
	if val != "Busan" {
		t.Errorf("overwritten value: got %q, want %q", val, "Busan")
	}

	// Delete and verify gone.
	if err := st.StorageDelete(ns, "city"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, found, _ = st.StorageGet(ns, "city")
	if found {
		t.Fatal("key should be gone after delete")
	}

	// Delete is idempotent.
	if err := st.StorageDelete(ns, "city"); err != nil {
		t.Fatalf("delete idempotent: %v", err)
	}

	// List returns sorted keys.
	st.StorageSet(ns, "beta", "2")
	st.StorageSet(ns, "alpha", "1")
	st.StorageSet(ns, "gamma", "3")
	keys, err := st.StorageList(ns)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 3 || keys[0] != "alpha" || keys[1] != "beta" || keys[2] != "gamma" {
		t.Errorf("sorted keys: got %v", keys)
	}
}

func TestExecutionHistory(t *testing.T) {
	st := openTestStore(t)

	// Use distinct timestamps so ORDER BY started_at DESC is deterministic.
	for i := 0; i < 3; i++ {
		ts := fmt.Sprintf("2026-04-13 12:00:%02d", i)
		rec := &ExecutionRecord{
			SkillID:       "sk-1",
			SkillName:     "greeter",
			StartedAt:     ts,
			FinishedAt:    ts,
			DurationMs:    100,
			ResultSummary: "said hello",
			Success:       true,
		}
		if err := st.RecordExecution(rec); err != nil {
			t.Fatalf("record exec %d: %v", i, err)
		}
	}

	// RecentExecutions returns most recent first (by started_at DESC).
	recs, err := st.RecentExecutions(10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("recent count: got %d, want 3", len(recs))
	}
	if recs[0].StartedAt <= recs[1].StartedAt {
		t.Errorf("expected descending started_at order: %q <= %q", recs[0].StartedAt, recs[1].StartedAt)
	}

	// SkillExecutionCount.
	cnt, err := st.SkillExecutionCount("sk-1")
	if err != nil {
		t.Fatalf("skill exec count: %v", err)
	}
	if cnt != 3 {
		t.Errorf("skill count: got %d, want 3", cnt)
	}

	// SearchExecutions via FTS on skill_name.
	found, err := st.SearchExecutions("greeter", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found) != 3 {
		t.Errorf("search results: got %d, want 3", len(found))
	}

	// SearchExecutions via FTS on result_summary.
	found2, err := st.SearchExecutions("hello", 10)
	if err != nil {
		t.Fatalf("search result_summary: %v", err)
	}
	if len(found2) != 3 {
		t.Errorf("search result_summary results: got %d, want 3", len(found2))
	}

	// CleanupOldExecutions: nothing old yet.
	deleted, err := st.CleanupOldExecutions(1)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 0 {
		t.Errorf("cleanup deleted: got %d, want 0", deleted)
	}
}

func TestTodayStats(t *testing.T) {
	st := openTestStore(t)

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	usage := `{"total_tokens": 100, "input_tokens": 60, "output_tokens": 40}`

	// Two successes with usage.
	for i := 0; i < 2; i++ {
		st.RecordExecution(&ExecutionRecord{
			SkillID:    "sk-a",
			SkillName:  "alpha",
			StartedAt:  now,
			Success:    true,
			RetryCount: 1,
			UsageJSON:  usage,
		})
	}
	// One failure without usage.
	st.RecordExecution(&ExecutionRecord{
		SkillID:   "sk-a",
		SkillName: "alpha",
		StartedAt: now,
		Success:   false,
	})

	stats, err := st.TodayStats()
	if err != nil {
		t.Fatalf("today stats: %v", err)
	}
	if stats.TotalRuns != 3 {
		t.Errorf("total runs: got %d, want 3", stats.TotalRuns)
	}
	if stats.Successful != 2 {
		t.Errorf("successful: got %d, want 2", stats.Successful)
	}
	if stats.Failed != 1 {
		t.Errorf("failed: got %d, want 1", stats.Failed)
	}
	if stats.AutoRetries != 2 {
		t.Errorf("auto retries: got %d, want 2", stats.AutoRetries)
	}
	if stats.TotalTokens != 200 {
		t.Errorf("total tokens: got %d, want 200", stats.TotalTokens)
	}
}

func TestUserContext(t *testing.T) {
	st := openTestStore(t)

	// Set and get.
	if err := st.SetUserContext("pref.lang", "ko", "user"); err != nil {
		t.Fatalf("set: %v", err)
	}
	val, found, err := st.GetUserContext("pref.lang")
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if val != "ko" {
		t.Errorf("value: got %q, want %q", val, "ko")
	}

	// Prefix listing.
	st.SetUserContext("pref.tz", "Asia/Seoul", "user")
	st.SetUserContext("other.key", "x", "system")
	list, err := st.ListUserContextPrefix("pref.")
	if err != nil {
		t.Fatalf("list prefix: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("prefix count: got %d, want 2", len(list))
	}
	if list[0].Key != "pref.lang" || list[1].Key != "pref.tz" {
		t.Errorf("prefix keys: got %v", list)
	}

	// Delete existing key.
	deleted, err := st.DeleteUserContext("pref.lang")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Error("expected delete to return true")
	}

	// Delete missing key returns false.
	deleted, err = st.DeleteUserContext("no-such-key")
	if err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	if deleted {
		t.Error("expected delete of missing key to return false")
	}
}

func TestIdentityLinking(t *testing.T) {
	st := openTestStore(t)

	// Resolve missing returns false.
	_, found, err := st.ResolveUser("telegram", "tg-123")
	if err != nil {
		t.Fatalf("resolve missing: %v", err)
	}
	if found {
		t.Fatal("expected not found for unlinked identity")
	}

	// Link and resolve.
	if err := st.LinkIdentity("user-1", "telegram", "tg-123"); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := st.LinkIdentity("user-1", "slack", "sl-456"); err != nil {
		t.Fatalf("link slack: %v", err)
	}
	gid, found, err := st.ResolveUser("telegram", "tg-123")
	if err != nil || !found {
		t.Fatalf("resolve: found=%v err=%v", found, err)
	}
	if gid != "user-1" {
		t.Errorf("global id: got %q, want %q", gid, "user-1")
	}

	// List identities.
	ids, err := st.ListIdentities("user-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("identity count: got %d, want 2", len(ids))
	}

	// Unlink then resolve returns false.
	if err := st.UnlinkIdentity("user-1", "telegram"); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	_, found, _ = st.ResolveUser("telegram", "tg-123")
	if found {
		t.Fatal("expected not found after unlink")
	}
}

func TestCheckpoints(t *testing.T) {
	st := openTestStore(t)
	agent := "cp-agent"

	// Add 3 turns.
	for i := 0; i < 3; i++ {
		st.AddTurn(agent, &core.ConversationTurn{
			Role: core.RoleUser, Content: "msg", Timestamp: "2026-04-13 10:00:00",
		})
	}

	// Create checkpoint after 3 turns.
	cpID, err := st.CreateCheckpoint(agent, "before-experiment")
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}

	// Add 2 more turns.
	for i := 0; i < 2; i++ {
		st.AddTurn(agent, &core.ConversationTurn{
			Role: core.RoleAssistant, Content: "extra", Timestamp: "2026-04-13 10:00:01",
		})
	}

	// Verify 5 turns before rollback.
	state, _ := st.LoadState(agent)
	if len(state.Turns) != 5 {
		t.Fatalf("turns before rollback: got %d, want 5", len(state.Turns))
	}

	// Rollback.
	deleted, err := st.RollbackToCheckpoint(cpID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if deleted != 2 {
		t.Errorf("rollback deleted: got %d, want 2", deleted)
	}

	// Verify only 3 turns remain.
	state, _ = st.LoadState(agent)
	if len(state.Turns) != 3 {
		t.Errorf("turns after rollback: got %d, want 3", len(state.Turns))
	}
}

func TestSkillFixes(t *testing.T) {
	st := openTestStore(t)

	// Record two fixes.
	if err := st.RecordFix("sk-1", "nil pointer", "old1", "new1"); err != nil {
		t.Fatalf("record fix 1: %v", err)
	}
	if err := st.RecordFix("sk-1", "timeout", "old2", "new2"); err != nil {
		t.Fatalf("record fix 2: %v", err)
	}

	// ListFixes returns DESC by created_at. Both share the same timestamp
	// so we just verify count and content, then pick the latest ID for apply.
	fixes, err := st.ListFixes("sk-1")
	if err != nil {
		t.Fatalf("list fixes: %v", err)
	}
	if len(fixes) != 2 {
		t.Fatalf("fix count: got %d, want 2", len(fixes))
	}
	msgs := map[string]bool{fixes[0].ErrorMsg: true, fixes[1].ErrorMsg: true}
	if !msgs["nil pointer"] || !msgs["timeout"] {
		t.Errorf("unexpected fix messages: %v", msgs)
	}

	// ApplyFix.
	applied, err := st.ApplyFix(fixes[0].ID)
	if err != nil {
		t.Fatalf("apply fix: %v", err)
	}
	if !applied {
		t.Error("expected apply to return true")
	}

	// ApplyFix again is idempotent (returns false).
	applied, err = st.ApplyFix(fixes[0].ID)
	if err != nil {
		t.Fatalf("apply fix again: %v", err)
	}
	if applied {
		t.Error("expected second apply to return false")
	}
}

func TestPermissions(t *testing.T) {
	st := openTestStore(t)
	ws := "ws-1"

	// Create the workspace that permission rules reference via FK.
	_, err := st.db.Exec(
		`INSERT INTO workspaces (id, name, root_path) VALUES (?, ?, ?)`,
		ws, "test workspace", "/tmp/test")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// File rules.
	if err := st.SaveFileRule(&FilePermissionRule{
		ID: "fr-1", WorkspaceID: ws, PathPattern: "/tmp/*",
		CanRead: true, CanWrite: true,
	}); err != nil {
		t.Fatalf("save file rule: %v", err)
	}
	rules, err := st.ListFileRules(ws)
	if err != nil {
		t.Fatalf("list file rules: %v", err)
	}
	if len(rules) != 1 || rules[0].PathPattern != "/tmp/*" {
		t.Errorf("file rules: got %+v", rules)
	}
	if !rules[0].CanRead || !rules[0].CanWrite || rules[0].CanDelete {
		t.Errorf("file rule booleans: %+v", rules[0])
	}

	// Delete file rule.
	if err := st.DeleteFileRule("fr-1"); err != nil {
		t.Fatalf("delete file rule: %v", err)
	}
	rules, _ = st.ListFileRules(ws)
	if len(rules) != 0 {
		t.Errorf("expected 0 file rules after delete, got %d", len(rules))
	}

	// Network rules.
	if err := st.SaveNetworkRule(&NetworkPermissionRule{
		ID: "nr-1", WorkspaceID: ws, DomainPattern: "*.example.com", AllowedMethods: "GET,POST",
	}); err != nil {
		t.Fatalf("save network rule: %v", err)
	}
	nrules, err := st.ListNetworkRules(ws)
	if err != nil {
		t.Fatalf("list network rules: %v", err)
	}
	if len(nrules) != 1 || nrules[0].DomainPattern != "*.example.com" {
		t.Errorf("network rules: got %+v", nrules)
	}

	// Global paths.
	if err := st.SaveGlobalPath(&GlobalPath{
		ID: "gp-1", Path: "/usr/local/bin", AccessType: "read",
	}); err != nil {
		t.Fatalf("save global path: %v", err)
	}
	gps, err := st.ListGlobalPaths()
	if err != nil {
		t.Fatalf("list global paths: %v", err)
	}
	if len(gps) != 1 || gps[0].Path != "/usr/local/bin" {
		t.Errorf("global paths: got %+v", gps)
	}
}

func TestCapabilities(t *testing.T) {
	st := openTestStore(t)

	// Not granted yet.
	has, err := st.HasCapabilityGrant("net_access")
	if err != nil {
		t.Fatalf("has before grant: %v", err)
	}
	if has {
		t.Fatal("expected no grant before granting")
	}

	// Grant.
	if err := st.GrantCapability("net_access"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	has, _ = st.HasCapabilityGrant("net_access")
	if !has {
		t.Fatal("expected grant after granting")
	}

	// Grant is idempotent.
	if err := st.GrantCapability("net_access"); err != nil {
		t.Fatalf("grant idempotent: %v", err)
	}

	// Revoke.
	if err := st.RevokeCapability("net_access"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	has, _ = st.HasCapabilityGrant("net_access")
	if has {
		t.Fatal("expected no grant after revoke")
	}
}

func TestProfiles(t *testing.T) {
	st := openTestStore(t)

	// Get non-existent returns false.
	_, found, err := st.GetProfileMeta("phantom")
	if err != nil {
		t.Fatalf("get phantom: %v", err)
	}
	if found {
		t.Fatal("expected not found for non-existent profile")
	}

	// Upsert and get.
	if err := st.UpsertProfileMeta("p-1", "dev profile", `["code","debug"]`, "admin"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	p, found, err := st.GetProfileMeta("p-1")
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if p.Description != "dev profile" || p.CreatedBy != "admin" {
		t.Errorf("profile mismatch: %+v", p)
	}

	// Default is active (schema defaults active=1).
	if !p.Active {
		t.Error("expected new profile to be active by default")
	}

	// ListActiveProfiles sees it immediately.
	active, err := st.ListActiveProfiles()
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 || active[0].ID != "p-1" {
		t.Errorf("active profiles: got %+v", active)
	}

	// UpdateEquippedSkills.
	if err := st.UpdateEquippedSkills("p-1", `["code","debug","deploy"]`); err != nil {
		t.Fatalf("update equipped: %v", err)
	}
	p, _, _ = st.GetProfileMeta("p-1")
	if p.EquippedSkills != `["code","debug","deploy"]` {
		t.Errorf("equipped skills: got %q", p.EquippedSkills)
	}
}

func TestScheduling(t *testing.T) {
	st := openTestStore(t)

	// GetLastRun for unknown skill returns nil.
	lr, err := st.GetLastRun("cron-skill")
	if err != nil {
		t.Fatalf("get last run unknown: %v", err)
	}
	if lr != nil {
		t.Fatal("expected nil for unknown skill")
	}

	// SetLastRun and round-trip.
	now := time.Date(2026, 4, 13, 14, 30, 0, 0, time.UTC)
	if err := st.SetLastRun("cron-skill", now); err != nil {
		t.Fatalf("set last run: %v", err)
	}
	lr, err = st.GetLastRun("cron-skill")
	if err != nil {
		t.Fatalf("get last run: %v", err)
	}
	if lr == nil {
		t.Fatal("expected non-nil last run")
	}
	if !lr.Equal(now) {
		t.Errorf("last run: got %v, want %v", lr, now)
	}

	// IncrementFailureCount x3.
	for i := 0; i < 3; i++ {
		if err := st.IncrementFailureCount("cron-skill"); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
	}
	fc, err := st.GetFailureCount("cron-skill")
	if err != nil {
		t.Fatalf("get failure count: %v", err)
	}
	if fc != 3 {
		t.Errorf("failure count: got %d, want 3", fc)
	}

	// ResetFailureCount.
	if err := st.ResetFailureCount("cron-skill"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	fc, _ = st.GetFailureCount("cron-skill")
	if fc != 0 {
		t.Errorf("failure count after reset: got %d, want 0", fc)
	}
}

func TestAudit(t *testing.T) {
	st := openTestStore(t)

	events := []struct {
		typ, detail, severity string
	}{
		{"login", "user logged in", "info"},
		{"exec", "ran skill X", "info"},
		{"error", "skill X failed", "warn"},
	}
	for _, e := range events {
		if err := st.RecordAudit(e.typ, e.detail, e.severity); err != nil {
			t.Fatalf("record audit %q: %v", e.typ, err)
		}
	}

	// RecentAuditEvents(2) returns only the 2 most recent in DESC order.
	recent, err := st.RecentAuditEvents(2)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2, got %d", len(recent))
	}
	if recent[0].EventType != "error" {
		t.Errorf("most recent event type: got %q, want %q", recent[0].EventType, "error")
	}
	if recent[1].EventType != "exec" {
		t.Errorf("second event type: got %q, want %q", recent[1].EventType, "exec")
	}
}
