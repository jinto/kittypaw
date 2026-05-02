package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestResolveProfileName_MentionOverride(t *testing.T) {
	cfg := core.DefaultConfig()
	st := openTestStore(t)
	got := ResolveProfileName(&cfg, "telegram", "user-1", "english-teacher", st)
	if got != "english-teacher" {
		t.Errorf("got %q, want %q", got, "english-teacher")
	}
}

func TestResolveProfileName_SessionOverride(t *testing.T) {
	cfg := core.DefaultConfig()
	st := openTestStore(t)
	// Set active_profile for this agent.
	if err := st.SetUserContext("active_profile:user-1", "custom-bot", "agent"); err != nil {
		t.Fatal(err)
	}
	got := ResolveProfileName(&cfg, "telegram", "user-1", "", st)
	if got != "custom-bot" {
		t.Errorf("got %q, want %q", got, "custom-bot")
	}
}

func TestResolveProfileName_ChannelBinding(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Profiles = []core.ProfileConfig{
		{ID: "tg-bot", Nick: "TG", Channels: []string{"telegram"}},
		{ID: "slack-bot", Nick: "SL", Channels: []string{"slack"}},
	}
	st := openTestStore(t)
	got := ResolveProfileName(&cfg, "telegram", "user-1", "", st)
	if got != "tg-bot" {
		t.Errorf("got %q, want %q", got, "tg-bot")
	}
}

func TestResolveProfileName_Default(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.DefaultProfile = "my-default"
	st := openTestStore(t)
	got := ResolveProfileName(&cfg, "web", "user-1", "", st)
	if got != "my-default" {
		t.Errorf("got %q, want %q", got, "my-default")
	}
}

func TestResolveProfileName_NilStore(t *testing.T) {
	cfg := core.DefaultConfig()
	// nil store should not panic, just skip session override.
	got := ResolveProfileName(&cfg, "web", "user-1", "", nil)
	if got != cfg.DefaultProfile {
		t.Errorf("got %q, want %q", got, cfg.DefaultProfile)
	}
}

// --- T5: Profile.switch integration ---

func TestProfileSwitch_SetsContext(t *testing.T) {
	st := openTestStore(t)

	// Create a profile directory so LoadProfile succeeds.
	base := t.TempDir()
	profDir := filepath.Join(base, "profiles", "new-persona")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "SOUL.md"), []byte("test soul"), 0o644); err != nil {
		t.Fatal(err)
	}

	// We can't easily call executeProfile directly without ConfigDir pointing to
	// our temp dir, so test the store round-trip that Profile.switch performs.
	agentID := "user-42"
	key := fmt.Sprintf("active_profile:%s", agentID)
	if err := st.SetUserContext(key, "new-persona", "agent"); err != nil {
		t.Fatal(err)
	}

	// ResolveProfileName should pick up the session override.
	cfg := core.DefaultConfig()
	got := ResolveProfileName(&cfg, "web", agentID, "", st)
	if got != "new-persona" {
		t.Errorf("got %q, want %q", got, "new-persona")
	}
}

// --- resolveProvider ---

func TestResolveProvider_EmptyReturnsDefault(t *testing.T) {
	mock := &mockProvider{}
	sess := &Session{
		Provider: mock,
		Config:   &core.Config{LLM: core.LLMConfig{Provider: "anthropic", Model: "default"}},
	}
	if got := sess.resolveProvider(""); got != mock {
		t.Error("empty model should return session default provider")
	}
}

func TestResolveProvider_NamedModel(t *testing.T) {
	mock := &mockProvider{}
	cfg := core.DefaultConfig()
	cfg.LLM = core.LLMConfig{Provider: "anthropic", APIKey: "test-key", Model: "default-model", MaxTokens: 1024}
	cfg.Models = []core.ModelConfig{
		{Name: "fast", Provider: "anthropic", APIKey: "test-key", Model: "claude-3-haiku", MaxTokens: 2048},
	}
	sess := &Session{Provider: mock, Config: &cfg}
	got := sess.resolveProvider("fast")
	if got == mock {
		t.Error("named model should create a new provider")
	}
	if got.MaxTokens() != 2048 {
		t.Errorf("MaxTokens = %d, want 2048 (from named model config)", got.MaxTokens())
	}
}

func TestResolveProvider_UnknownModelFallsBack(t *testing.T) {
	mock := &mockProvider{}
	cfg := core.DefaultConfig()
	cfg.LLM = core.LLMConfig{Provider: "anthropic", APIKey: "test-key", Model: "default-model", MaxTokens: 1024}
	sess := &Session{Provider: mock, Config: &cfg}
	// Raw model IDs not in config should fall back to default (security: no API key leakage).
	if got := sess.resolveProvider("claude-3-opus-20240229"); got != mock {
		t.Error("unknown model should fall back to session default provider")
	}
}

func TestResolveProvider_InvalidProviderFallsBack(t *testing.T) {
	mock := &mockProvider{}
	cfg := core.DefaultConfig()
	cfg.LLM = core.LLMConfig{Provider: "nonexistent", Model: "x"}
	sess := &Session{Provider: mock, Config: &cfg}
	if got := sess.resolveProvider("any-model"); got != mock {
		t.Error("invalid provider should fall back to session default")
	}
}

func TestProfileSwitch_OverriddenByMention(t *testing.T) {
	st := openTestStore(t)
	agentID := "user-42"
	key := fmt.Sprintf("active_profile:%s", agentID)
	if err := st.SetUserContext(key, "session-profile", "agent"); err != nil {
		t.Fatal(err)
	}

	cfg := core.DefaultConfig()
	// @mention should win over session override.
	got := ResolveProfileName(&cfg, "web", agentID, "mention-profile", st)
	if got != "mention-profile" {
		t.Errorf("got %q, want %q", got, "mention-profile")
	}
}

// ---------------------------------------------------------------------------
// augmentSystemPromptWithSuggestion
// ---------------------------------------------------------------------------

func newSuggestionTestMessages() []core.LlmMessage {
	return []core.LlmMessage{
		{Role: core.RoleSystem, Content: "## base prompt"},
	}
}

func TestAugmentSystemPromptWithSuggestion_FirstTurnInjects(t *testing.T) {
	st := openTestStore(t)
	// Reflection has detected an intent — store it the way RunReflectionCycle does.
	if err := st.SetUserContext(
		"suggest_candidate:abc123", "환율 조회|3|0 8 * * *", "reflection",
	); err != nil {
		t.Fatalf("seed candidate: %v", err)
	}
	msgs := newSuggestionTestMessages()
	turns := []core.ConversationTurn{
		{Role: core.RoleUser, Content: "hello"}, // just-added first turn
	}

	augmentSystemPromptWithSuggestion(msgs, st, turns)

	if !strings.Contains(msgs[0].Content, "환율 조회") {
		t.Errorf("expected suggestion label in system prompt; got: %q", msgs[0].Content)
	}
	// Surface time recorded so the next session does not re-surface.
	if v, ok, _ := st.GetUserContext("surfaced_at:abc123"); !ok || v == "" {
		t.Errorf("surfaced_at not recorded; got ok=%v v=%q", ok, v)
	}
}

func TestAugmentSystemPromptWithSuggestion_NotFirstTurnSkips(t *testing.T) {
	st := openTestStore(t)
	_ = st.SetUserContext("suggest_candidate:abc123", "환율 조회|3|0 8 * * *", "reflection")
	msgs := newSuggestionTestMessages()
	turns := []core.ConversationTurn{
		{Role: core.RoleUser, Content: "first"},
		{Role: core.RoleAssistant, Content: "answered"},
		{Role: core.RoleUser, Content: "second"},
	}

	augmentSystemPromptWithSuggestion(msgs, st, turns)

	if strings.Contains(msgs[0].Content, "환율 조회") {
		t.Error("mid-session turn must not surface suggestion")
	}
	if _, ok, _ := st.GetUserContext("surfaced_at:abc123"); ok {
		t.Error("surfaced_at must not be recorded when no surface happened")
	}
}

func TestAugmentSystemPromptWithSuggestion_SilenceWindowSuppresses(t *testing.T) {
	st := openTestStore(t)
	_ = st.SetUserContext("suggest_candidate:abc123", "환율 조회|3|0 8 * * *", "reflection")
	// Pretend the candidate was surfaced 1 hour ago — well within the
	// 7-day silence window. Must stay suppressed even on a first turn.
	_ = st.SetUserContext(
		"surfaced_at:abc123",
		time.Now().Add(-1*time.Hour).UTC().Format(time.RFC3339),
		"suggestion",
	)
	msgs := newSuggestionTestMessages()
	turns := []core.ConversationTurn{{Role: core.RoleUser, Content: "hi"}}

	augmentSystemPromptWithSuggestion(msgs, st, turns)

	if strings.Contains(msgs[0].Content, "환율 조회") {
		t.Error("candidate surfaced inside silence window must stay suppressed")
	}
}

func TestAugmentSystemPromptWithSuggestion_AfterSilenceResurfaces(t *testing.T) {
	st := openTestStore(t)
	_ = st.SetUserContext("suggest_candidate:abc123", "환율 조회|3|0 8 * * *", "reflection")
	// Surfaced 8 days ago — silence window has elapsed. Must surface again.
	_ = st.SetUserContext(
		"surfaced_at:abc123",
		time.Now().Add(-8*24*time.Hour).UTC().Format(time.RFC3339),
		"suggestion",
	)
	msgs := newSuggestionTestMessages()
	turns := []core.ConversationTurn{{Role: core.RoleUser, Content: "hi"}}

	augmentSystemPromptWithSuggestion(msgs, st, turns)

	if !strings.Contains(msgs[0].Content, "환율 조회") {
		t.Error("candidate past silence window must re-surface")
	}
}

func TestAugmentSystemPromptWithSuggestion_NoCandidatesNoOp(t *testing.T) {
	st := openTestStore(t)
	msgs := newSuggestionTestMessages()
	turns := []core.ConversationTurn{{Role: core.RoleUser, Content: "hi"}}

	augmentSystemPromptWithSuggestion(msgs, st, turns)

	if msgs[0].Content != "## base prompt" {
		t.Errorf("no-candidate path must not mutate prompt; got %q", msgs[0].Content)
	}
}

func TestAugmentSystemPromptWithSuggestion_MalformedValueSkipped(t *testing.T) {
	st := openTestStore(t)
	// Empty label after split — must skip this candidate but still
	// look at the next one.
	_ = st.SetUserContext("suggest_candidate:bad", "  |3|0 8 * * *", "reflection")
	_ = st.SetUserContext("suggest_candidate:good", "주가 알림|5|0 9 * * 1-5", "reflection")
	msgs := newSuggestionTestMessages()
	turns := []core.ConversationTurn{{Role: core.RoleUser, Content: "hi"}}

	augmentSystemPromptWithSuggestion(msgs, st, turns)

	if strings.Contains(msgs[0].Content, "  |") {
		t.Error("malformed candidate must not be surfaced")
	}
	if !strings.Contains(msgs[0].Content, "주가 알림") {
		t.Error("subsequent well-formed candidate must surface")
	}
}

// ---------------------------------------------------------------------------
// appendSuggestionForBranchResponse
// ---------------------------------------------------------------------------

func newSuggestionBranchTestSession(t *testing.T) *Session {
	t.Helper()
	st := openTestStore(t)
	return &Session{Store: st}
}

func newWebChatEvent(sessionID string) core.Event {
	payload, _ := json.Marshal(core.ChatPayload{
		ChatID:    sessionID,
		SessionID: sessionID,
		Text:      "환율",
	})
	return core.Event{Type: core.EventWebChat, Payload: payload}
}

func TestAppendSuggestionForBranchResponse_FirstTurnAppends(t *testing.T) {
	s := newSuggestionBranchTestSession(t)
	if err := s.Store.SetUserContext(
		"suggest_candidate:abc123", "환율 조회|3|0 8 * * *", "reflection-test",
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	event := newWebChatEvent("session-fresh")
	got := appendSuggestionForBranchResponse(s, event, "현재 환율은 1480원입니다")

	if !strings.Contains(got, "💡") {
		t.Errorf("first-turn branch must append suggestion suffix; got %q", got)
	}
	if !strings.Contains(got, "환율 조회") {
		t.Errorf("suffix must include candidate label; got %q", got)
	}
	if v, ok, _ := s.Store.GetUserContext("surfaced_at:abc123"); !ok || v == "" {
		t.Errorf("surfaced_at not recorded; ok=%v v=%q", ok, v)
	}
}

func TestAppendSuggestionForBranchResponse_NotFirstTurnSkips(t *testing.T) {
	s := newSuggestionBranchTestSession(t)
	_ = s.Store.SetUserContext("suggest_candidate:abc123", "환율 조회|3|0 8 * * *", "reflection-test")

	// Pre-existing assistant turn for this agent_id ⇒ not first turn.
	channelName := core.EventWebChat.ChannelName()
	agentID := channelName + "-session-existing"
	state := &core.AgentState{
		AgentID:      agentID,
		SystemPrompt: SystemPrompt,
		Turns: []core.ConversationTurn{
			{Role: core.RoleUser, Content: "이전"},
			{Role: core.RoleAssistant, Content: "응답"},
		},
	}
	if err := s.Store.SaveState(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	event := newWebChatEvent("session-existing")
	got := appendSuggestionForBranchResponse(s, event, "현재 환율은 1480원")

	if strings.Contains(got, "💡") {
		t.Errorf("subsequent turn must not append suggestion; got %q", got)
	}
	if _, ok, _ := s.Store.GetUserContext("surfaced_at:abc123"); ok {
		t.Errorf("surfaced_at must not be recorded when no surface happened")
	}
}

func TestAppendSuggestionForBranchResponse_NoCandidateUnchanged(t *testing.T) {
	s := newSuggestionBranchTestSession(t)
	event := newWebChatEvent("session-empty")
	got := appendSuggestionForBranchResponse(s, event, "현재 환율은 1480원")

	if got != "현재 환율은 1480원" {
		t.Errorf("no-candidate path must not mutate response; got %q", got)
	}
}

func TestAppendSuggestionForBranchResponse_SilenceWindowSuppresses(t *testing.T) {
	s := newSuggestionBranchTestSession(t)
	_ = s.Store.SetUserContext("suggest_candidate:abc123", "환율 조회|3|0 8 * * *", "reflection-test")
	_ = s.Store.SetUserContext(
		"surfaced_at:abc123",
		time.Now().Add(-1*time.Hour).UTC().Format(time.RFC3339),
		"suggestion",
	)

	event := newWebChatEvent("session-silenced")
	got := appendSuggestionForBranchResponse(s, event, "현재 환율은 1480원")

	if strings.Contains(got, "💡") {
		t.Errorf("silenced candidate must not surface; got %q", got)
	}
}
