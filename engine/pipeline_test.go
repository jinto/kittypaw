package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/llm"
)

// mediateMockProvider is a minimal llm.Provider used to drive
// mediateSkillOutput tests without spinning up a real backend. The
// streaming path delegates to Generate so a single response/err is
// enough to cover all routes.
type mediateMockProvider struct {
	response string
	err      error
	calls    int
}

func (m *mediateMockProvider) Generate(_ context.Context, _ []core.LlmMessage) (*llm.Response, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return &llm.Response{Content: m.response}, nil
}

func (m *mediateMockProvider) GenerateWithTools(ctx context.Context, msgs []core.LlmMessage, _ []llm.Tool) (*llm.Response, error) {
	return m.Generate(ctx, msgs)
}

func (m *mediateMockProvider) ContextWindow() int { return 200000 }
func (m *mediateMockProvider) MaxTokens() int     { return 4096 }

// toolUseScriptedProvider scripts a tool-use loop: the first N calls
// return a tool_use response (with the supplied tool_use input), the
// rest return a final text response. This shape is what
// mediateSkillOutputWithTools drives, so the test can assert the loop
// terminates correctly and the tool input is forwarded.
type toolUseScriptedProvider struct {
	// scripted tool_use turns (one entry consumed per GenerateWithTools call)
	toolUses []map[string]any
	// final text returned once toolUses is exhausted
	finalText string
	// when nonzero, GenerateWithTools always returns tool_use (loop cap)
	infinite bool
	calls    int
}

func (p *toolUseScriptedProvider) Generate(_ context.Context, _ []core.LlmMessage) (*llm.Response, error) {
	return &llm.Response{Content: p.finalText, StopReason: "end_turn"}, nil
}

func (p *toolUseScriptedProvider) GenerateWithTools(_ context.Context, _ []core.LlmMessage, _ []llm.Tool) (*llm.Response, error) {
	p.calls++
	if p.infinite {
		return &llm.Response{
			ContentBlocks: []core.ContentBlock{{
				Type:  core.BlockTypeToolUse,
				ID:    "toolu_inf",
				Name:  "code_exec",
				Input: map[string]any{"code": "1+1"},
			}},
			StopReason: "tool_use",
		}, nil
	}
	if len(p.toolUses) > 0 {
		input := p.toolUses[0]
		p.toolUses = p.toolUses[1:]
		return &llm.Response{
			ContentBlocks: []core.ContentBlock{{
				Type:  core.BlockTypeToolUse,
				ID:    "toolu_test",
				Name:  "code_exec",
				Input: input,
			}},
			StopReason: "tool_use",
		}, nil
	}
	return &llm.Response{
		Content:    p.finalText,
		StopReason: "end_turn",
	}, nil
}

func (p *toolUseScriptedProvider) ContextWindow() int { return 200000 }
func (p *toolUseScriptedProvider) MaxTokens() int     { return 4096 }

func TestMediateSkillOutput_NilProvider(t *testing.T) {
	sess := &Session{Provider: nil}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "원화로 환율", "raw output")
	if got != "raw output" {
		t.Fatalf("nil provider must return raw output verbatim, got %q", got)
	}
}

func TestMediateSkillOutput_NilSession(t *testing.T) {
	got := mediateSkillOutput(context.Background(), nil, "exchange-rate", "원화로 환율", "raw output")
	if got != "raw output" {
		t.Fatalf("nil session must return raw output verbatim, got %q", got)
	}
}

func TestMediateSkillOutput_EmptyUserText(t *testing.T) {
	mock := &mediateMockProvider{response: "should not be reached"}
	sess := &Session{Provider: mock}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "", "raw")
	if got != "raw" {
		t.Fatalf("empty user text must skip LLM and return raw, got %q", got)
	}
	if mock.calls != 0 {
		t.Fatalf("LLM must not be called when user text is empty, got %d calls", mock.calls)
	}
}

func TestMediateSkillOutput_EmptyRawOutput(t *testing.T) {
	mock := &mediateMockProvider{response: "x"}
	sess := &Session{Provider: mock}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "원화로", "")
	if got != "" {
		t.Fatalf("empty raw must return empty (caller already handled this), got %q", got)
	}
	if mock.calls != 0 {
		t.Fatalf("LLM must not be called when raw output is empty, got %d calls", mock.calls)
	}
}

func TestMediateSkillOutput_LLMError(t *testing.T) {
	mock := &mediateMockProvider{err: errors.New("provider down")}
	sess := &Session{Provider: mock}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "원화로 환율", "raw EUR-base")
	if got != "raw EUR-base" {
		t.Fatalf("LLM error must fall back to raw, got %q", got)
	}
	if mock.calls != 1 {
		t.Fatalf("expected exactly 1 call, got %d", mock.calls)
	}
}

func TestMediateSkillOutput_EmptyResponseFallsBack(t *testing.T) {
	mock := &mediateMockProvider{response: "   \n   "}
	sess := &Session{Provider: mock}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "원화로", "raw")
	if got != "raw" {
		t.Fatalf("whitespace-only LLM response must fall back to raw, got %q", got)
	}
}

func TestMediateSkillOutput_PassThrough(t *testing.T) {
	mock := &mediateMockProvider{response: "1 USD = 1477원\n1 EUR = 1684원"}
	sess := &Session{Provider: mock}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "원화로 환율", "raw EUR-base output")
	if !strings.Contains(got, "1477원") {
		t.Fatalf("expected LLM-mediated response, got %q", got)
	}
	if mock.calls != 1 {
		t.Fatalf("expected exactly 1 LLM call, got %d", mock.calls)
	}
}

func TestBuildMediatePrompt_ContainsContractRules(t *testing.T) {
	prompt := buildMediatePrompt("exchange-rate", "원화로 환율", "1 USD = 1477 KRW")
	checks := []string{
		"exchange-rate",
		"원화로 환율",
		"1 USD = 1477 KRW",
		"수치/사실은 변경 X",
		"fabrication 금지",
		"메타 안내",
		"부족할 때만",
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n--- prompt ---\n%s\n----", want, prompt)
		}
	}
}

func TestMediateSkillOutput_LongRawTruncated(t *testing.T) {
	// Raw output well past the 8 kB cap still produces a valid LLM call
	// (truncation marker added). Use raw with no numbers so the
	// fact-preservation guard short-circuits to true (overlap N/A).
	long := strings.Repeat("A", mediateSkillRawOutputCap+500)
	mock := &mediateMockProvider{response: "summary"}
	sess := &Session{Provider: mock}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "환율", long)
	if got != "summary" {
		t.Fatalf("expected mediated summary, got %q", got)
	}
}

func TestMediateSkillOutput_FabricationFallsBack(t *testing.T) {
	// Raw has numbers; LLM response has none → fabrication signature.
	// Caller must receive raw, not the LLM hallucination.
	raw := "1 USD = 1477.04 KRW\n1 EUR = 1684.32 KRW"
	mock := &mediateMockProvider{response: "환율 정보를 가져오지 못했습니다. 다른 사이트를 확인해주세요."}
	sess := &Session{Provider: mock}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "환율", raw)
	if got != raw {
		t.Fatalf("fabrication (zero numeric overlap) must fall back to raw\n  got: %q\n  raw: %q", got, raw)
	}
}

func TestMediateSkillOutput_PartialNumberOverlapPasses(t *testing.T) {
	// LLM kept some raw numbers but reformatted units — should pass.
	raw := "1 USD = 1477.04 KRW"
	mock := &mediateMockProvider{response: "1 USD = 1477.04원"}
	sess := &Session{Provider: mock}
	got := mediateSkillOutput(context.Background(), sess, "exchange-rate", "원화로 환율", raw)
	if got != "1 USD = 1477.04원" {
		t.Fatalf("LLM kept the raw number — expected mediated response, got %q", got)
	}
}

func TestMediationPreservesFacts_RawHasNoNumbers(t *testing.T) {
	if !mediationPreservesFacts("hello world", "anything goes") {
		t.Fatal("when raw has no numbers, guard must abstain (return true)")
	}
}

func TestMediationPreservesFacts_ZeroOverlap(t *testing.T) {
	if mediationPreservesFacts("1 2 3", "9 8 7") {
		t.Fatal("disjoint numbers must signal fabrication (return false)")
	}
}

func TestMediationPreservesFacts_AnyOverlap(t *testing.T) {
	if !mediationPreservesFacts("1 2 3", "9 8 3") {
		t.Fatal("any shared number must pass (return true)")
	}
}

// TestRecordPipelineTurn_AppendsBothTurns guards the cross-turn fix
// from the 2026-04-27 transcript: a follow-up legacy-LLM turn must
// see the prior branch dispatch in conversation history.
func TestRecordPipelineTurn_AppendsBothTurns(t *testing.T) {
	st := openTestStore(t)
	sess := &Session{Store: st}

	payload, _ := json.Marshal(core.ChatPayload{
		ChatID:    "test-chat",
		Text:      "환율 알려줘",
		SessionID: "test-session",
	})
	event := core.Event{Type: core.EventWebChat, Payload: payload}

	if err := sess.recordPipelineTurn(event, "환율 알려줘", "1 USD = 1477.04 KRW"); err != nil {
		t.Fatalf("recordPipelineTurn: %v", err)
	}

	// agentID derivation mirrors recordPipelineTurn's fallback path
	// (no ResolveUser hit on a fresh store) — channel-name + session id.
	state, err := st.LoadState("web-test-session")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(state.Turns) != 2 {
		t.Fatalf("expected 2 turns (user + assistant), got %d", len(state.Turns))
	}
	if state.Turns[0].Role != core.RoleUser || state.Turns[0].Content != "환율 알려줘" {
		t.Errorf("turn 0 not user query: %+v", state.Turns[0])
	}
	if state.Turns[1].Role != core.RoleAssistant || state.Turns[1].Content != "1 USD = 1477.04 KRW" {
		t.Errorf("turn 1 not branch response: %+v", state.Turns[1])
	}
}

func TestStripBranchControlMarker_RemovesInstallAck(t *testing.T) {
	in := "✅ '환율 조회' 스킬을 설치했어요.\n\n📈 환율\n1 USD = 1477 KRW"
	want := "📈 환율\n1 USD = 1477 KRW"
	got := stripBranchControlMarker(in)
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestStripBranchControlMarker_NoMarkerPassThrough(t *testing.T) {
	in := "📈 환율\n1 USD = 1477 KRW"
	if got := stripBranchControlMarker(in); got != in {
		t.Errorf("untouched response should pass through, got %q", got)
	}
}

func TestRecordPipelineTurn_StripsAckBeforeStoring(t *testing.T) {
	// History append must not propagate the install-ack marker to the
	// next turn's legacy-LLM context — otherwise the LLM sees the ack
	// pattern and copies it back into its own response (2026-04-27
	// regression: '스킬을 설치했어요' count=2 in flow_installed_dispatch).
	st := openTestStore(t)
	sess := &Session{Store: st}
	payload, _ := json.Marshal(core.ChatPayload{
		ChatID:    "test-chat",
		Text:      "네",
		SessionID: "test-session",
	})
	event := core.Event{Type: core.EventWebChat, Payload: payload}
	if err := sess.recordPipelineTurn(event, "네", "✅ '환율 조회' 스킬을 설치했어요.\n\n📈 환율\n1 USD = 1477.04 KRW"); err != nil {
		t.Fatal(err)
	}
	state, _ := st.LoadState("web-test-session")
	if len(state.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(state.Turns))
	}
	stored := state.Turns[1].Content
	if strings.Contains(stored, "스킬을 설치했어요") {
		t.Errorf("ack marker leaked into history: %q", stored)
	}
	if !strings.Contains(stored, "1477.04") {
		t.Errorf("data part dropped from history: %q", stored)
	}
}

func TestMediateWithTools_CodeExecLoop(t *testing.T) {
	// Provider scripts: 1) tool_use with arithmetic on raw, 2) final text.
	// Asserts the loop forwards the LLM-issued code through executeCode
	// and the final response (which preserves a raw number) reaches the
	// caller.
	p := &toolUseScriptedProvider{
		toolUses: []map[string]any{
			{"code": "const u=1477.04, e=0.85383; (u/e).toFixed(2)"},
		},
		finalText: "1 EUR = 1730.20 KRW (raw 1477.04 보존)",
	}
	sess := &Session{Provider: p}
	out := mediateSkillOutputWithTools(context.Background(), sess, "exchange-rate", "원화로 환율", "1 USD = 1477.04 KRW, 1 USD = 0.85383 EUR")
	if !strings.Contains(out, "1730.20") {
		t.Fatalf("final text not delivered, got: %q", out)
	}
	if !strings.Contains(out, "1477.04") {
		t.Errorf("raw number not preserved in final text: %q", out)
	}
	if p.calls != 2 {
		t.Errorf("expected 2 GenerateWithTools calls (tool_use + final), got %d", p.calls)
	}
}

func TestMediateWithTools_LoopCapped(t *testing.T) {
	// Provider keeps returning tool_use forever. After the cap the
	// loop must fall back to the raw output rather than spin.
	p := &toolUseScriptedProvider{infinite: true}
	sess := &Session{Provider: p}
	raw := "1 USD = 1477.04 KRW"
	out := mediateSkillOutputWithTools(context.Background(), sess, "exchange-rate", "원화로", raw)
	if out != raw {
		t.Fatalf("loop cap should fall back to raw, got %q", out)
	}
	if p.calls != mediateMaxToolIterations {
		t.Errorf("expected exactly %d calls before cap, got %d", mediateMaxToolIterations, p.calls)
	}
}

func TestMediateWithTools_FabricationGuardFalls(t *testing.T) {
	// Provider goes straight to a final text with zero numeric overlap
	// with the raw — fabrication signature. Caller must receive raw.
	p := &toolUseScriptedProvider{
		finalText: "환율 정보를 가져오지 못했습니다. 사이트를 확인하세요.",
	}
	sess := &Session{Provider: p}
	raw := "1 USD = 1477.04 KRW"
	out := mediateSkillOutputWithTools(context.Background(), sess, "exchange-rate", "원화로", raw)
	if out != raw {
		t.Fatalf("zero-overlap final text must fall back to raw, got %q", out)
	}
}

func TestMediateWithTools_NilProviderFallsBack(t *testing.T) {
	sess := &Session{Provider: nil}
	out := mediateSkillOutputWithTools(context.Background(), sess, "x", "원화로", "1 USD = 1477 KRW")
	if out != "1 USD = 1477 KRW" {
		t.Fatalf("nil provider must return raw, got %q", out)
	}
}

func TestClassifyIntent_ModifierFollowupRouting(t *testing.T) {
	// queryHasModifier=true + RecentSkillOutput populated + short →
	// IntentModifierFollowup with cached raw in Params.
	state := NewPipelineState()
	state.RecordSkillOutput("1 USD = 1477.04 KRW")
	intent := classifyIntent("원화로 환율", state, nil)
	if intent.Kind != IntentModifierFollowup {
		t.Fatalf("expected IntentModifierFollowup, got %v", intent.Kind)
	}
	raw, _ := intent.Params["raw_output"].(string)
	if raw == "" {
		t.Errorf("raw_output not propagated to intent.Params")
	}
}

func TestClassifyIntent_ModifierFollowup_NoCacheBypasses(t *testing.T) {
	// queryHasModifier=true but cache empty → must NOT route to
	// ModifierFollowup; falls through to legacy fallback (or other
	// branches if applicable). Without raw output the mediation has
	// nothing to mediate.
	state := NewPipelineState()
	intent := classifyIntent("원화로 환율", state, nil)
	if intent.Kind == IntentModifierFollowup {
		t.Fatalf("empty cache must not route to ModifierFollowup")
	}
}

func TestClassifyIntent_LongModifierBypassesFollowup(t *testing.T) {
	// Modifier in a long sentence is a fresh request, not a follow-up.
	state := NewPipelineState()
	state.RecordSkillOutput("data")
	long := strings.Repeat("원화로 환율 한 번 알려줘 자세히 ", 3) // > 30 runes
	intent := classifyIntent(long, state, nil)
	if intent.Kind == IntentModifierFollowup {
		t.Fatalf("long modifier query should not route to ModifierFollowup, got %+v", intent)
	}
}

func TestRecordSkillOutput_RoundTrip(t *testing.T) {
	ps := NewPipelineState()
	if got := ps.RecentSkillOutput(); got != "" {
		t.Fatalf("fresh state must return empty, got %q", got)
	}
	ps.RecordSkillOutput("1 USD = 1477.04 KRW")
	if got := ps.RecentSkillOutput(); got != "1 USD = 1477.04 KRW" {
		t.Errorf("got %q, want roundtrip", got)
	}
}

func TestRecordSkillOutput_EmptyIgnored(t *testing.T) {
	ps := NewPipelineState()
	ps.RecordSkillOutput("first")
	ps.RecordSkillOutput("") // must not overwrite with empty
	if got := ps.RecentSkillOutput(); got != "first" {
		t.Errorf("empty record should be no-op, got %q", got)
	}
}

func TestQueryHasModifier_PositiveCases(t *testing.T) {
	cases := []string{
		"원화로 환율",
		"엔으로 환율",
		"기준으로 다시",
		"원화 기준의 환율",
		"간단히 환율",
		"자세히 알려줘",
		"다시 계산",
		"환산해줘",
		"USD에서 KRW로 변환",
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			if !queryHasModifier(q) {
				t.Errorf("expected modifier detection for %q", q)
			}
		})
	}
}

func TestQueryHasModifier_NegativeCases(t *testing.T) {
	cases := []string{
		"환율",
		"환율 알려줘",
		"오늘 환율",
		"내일 날씨",
		"엔화는?",
		"주식",
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			if queryHasModifier(q) {
				t.Errorf("unexpected modifier detection for %q (would deflect a fresh-retrieval query)", q)
			}
		})
	}
}

func TestClearSkillOutput_RemovesCache(t *testing.T) {
	ps := NewPipelineState()
	ps.RecordSkillOutput("1 USD = 1477.04 KRW")
	if ps.RecentSkillOutput() == "" {
		t.Fatal("setup failed: cache empty after Record")
	}
	ps.ClearSkillOutput()
	if got := ps.RecentSkillOutput(); got != "" {
		t.Errorf("after Clear, cache must be empty, got %q", got)
	}
}

func TestClearSkillOutput_NilSafe(t *testing.T) {
	var ps *PipelineState
	ps.ClearSkillOutput() // must not panic
}

func TestRecordSkillOutput_NilSafe(t *testing.T) {
	var ps *PipelineState
	ps.RecordSkillOutput("x") // must not panic
	if got := ps.RecentSkillOutput(); got != "" {
		t.Errorf("nil ps must return empty, got %q", got)
	}
}

func TestAugmentSystemPromptWithRecentSkillOutput_AppendsBlock(t *testing.T) {
	ps := NewPipelineState()
	ps.RecordSkillOutput("1 USD = 1477.04 KRW")

	messages := []core.LlmMessage{
		{Role: core.RoleSystem, Content: "base prompt"},
		{Role: core.RoleUser, Content: "원화로 환율"},
	}
	augmentSystemPromptWithRecentSkillOutput(messages, "원화로 환율", ps)

	got := messages[0].Content
	if !strings.Contains(got, "Cross-turn context") {
		t.Errorf("system message must carry cross-turn block, got %q", got)
	}
	if !strings.Contains(got, "1 USD = 1477.04 KRW") {
		t.Errorf("system message must carry the cached skill output, got %q", got)
	}
	if !strings.HasPrefix(got, "base prompt") {
		t.Errorf("base prompt must be preserved as prefix, got %q", got)
	}
}

func TestAugmentSystemPromptWithRecentSkillOutput_NoOpWhenLong(t *testing.T) {
	ps := NewPipelineState()
	ps.RecordSkillOutput("data")

	long := strings.Repeat("긴 질문 ", 10) // > 30 runes
	messages := []core.LlmMessage{
		{Role: core.RoleSystem, Content: "base"},
	}
	augmentSystemPromptWithRecentSkillOutput(messages, long, ps)
	if messages[0].Content != "base" {
		t.Errorf("long query must not augment, got %q", messages[0].Content)
	}
}

func TestAugmentSystemPromptWithRecentSkillOutput_NoOpWhenNoCache(t *testing.T) {
	ps := NewPipelineState()
	messages := []core.LlmMessage{
		{Role: core.RoleSystem, Content: "base"},
	}
	augmentSystemPromptWithRecentSkillOutput(messages, "원화로 환율", ps)
	if messages[0].Content != "base" {
		t.Errorf("empty cache must not augment, got %q", messages[0].Content)
	}
}

func TestAugmentSystemPromptWithRecentSkillOutput_NoOpWhenEmptyMessages(t *testing.T) {
	ps := NewPipelineState()
	ps.RecordSkillOutput("data")
	var messages []core.LlmMessage
	augmentSystemPromptWithRecentSkillOutput(messages, "원화로", ps)
	// Just must not panic.
}

func TestRecordPipelineTurn_NextLoadSeesPriorTurns(t *testing.T) {
	// Two consecutive branch dispatches under the same agentID must
	// accumulate in history — this is what gives the 3rd turn (legacy
	// LLM) a 2-turn context.
	st := openTestStore(t)
	sess := &Session{Store: st}
	mkEvent := func(text string) core.Event {
		payload, _ := json.Marshal(core.ChatPayload{
			ChatID:    "test-chat",
			Text:      text,
			SessionID: "test-session",
		})
		return core.Event{Type: core.EventWebChat, Payload: payload}
	}

	if err := sess.recordPipelineTurn(mkEvent("환율"), "환율", "1 USD = 1477.04 KRW"); err != nil {
		t.Fatal(err)
	}
	if err := sess.recordPipelineTurn(mkEvent("원화로"), "원화로", "1 USD = 1477.04 KRW (raw)"); err != nil {
		t.Fatal(err)
	}

	state, _ := st.LoadState("web-test-session")
	if len(state.Turns) != 4 {
		t.Fatalf("expected 4 turns (2 user + 2 assistant), got %d", len(state.Turns))
	}
}
