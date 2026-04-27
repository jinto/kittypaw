package engine

import (
	"context"
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

func (m *mediateMockProvider) GenerateStream(ctx context.Context, msgs []core.LlmMessage, _ llm.TokenCallback) (*llm.Response, error) {
	return m.Generate(ctx, msgs)
}

func (m *mediateMockProvider) ContextWindow() int { return 200000 }
func (m *mediateMockProvider) MaxTokens() int     { return 4096 }

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
