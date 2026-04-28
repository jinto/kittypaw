package engine

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/llm"
)

// moaMockProvider is a per-model mock. Unlike mockProvider in e2e_test.go
// (queue-based, shared across calls), this is instantiated once per model
// so fan-out tests see truly independent provider state.
type moaMockProvider struct {
	text      string
	err       error
	delay     time.Duration
	callCount atomic.Int32
}

func (m *moaMockProvider) Generate(ctx context.Context, _ []core.LlmMessage) (*llm.Response, error) {
	m.callCount.Add(1)
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &llm.Response{
		Content: m.text,
		Usage:   &llm.TokenUsage{InputTokens: 10, OutputTokens: 20, Model: "mock"},
	}, nil
}

func (m *moaMockProvider) GenerateStream(ctx context.Context, msgs []core.LlmMessage, _ llm.TokenCallback) (*llm.Response, error) {
	return m.Generate(ctx, msgs)
}

func (m *moaMockProvider) GenerateWithTools(ctx context.Context, msgs []core.LlmMessage, _ []llm.Tool) (*llm.Response, error) {
	return m.Generate(ctx, msgs)
}

func (m *moaMockProvider) ContextWindow() int { return 128_000 }
func (m *moaMockProvider) MaxTokens() int     { return 4096 }

func moaResolver(providers map[string]llm.Provider) ProviderResolver {
	return func(model string) llm.Provider {
		if p, ok := providers[model]; ok {
			return p
		}
		return nil
	}
}

func TestMoA_HappyPath_3Models(t *testing.T) {
	providers := map[string]llm.Provider{
		"modelA":      &moaMockProvider{text: "answer A"},
		"modelB":      &moaMockProvider{text: "answer B"},
		"modelC":      &moaMockProvider{text: "answer C"},
		"synthesizer": &moaMockProvider{text: "synthesized"},
	}
	req := MoARequest{
		Prompt:           "question",
		Models:           []string{"modelA", "modelB", "modelC"},
		SynthesizerModel: "synthesizer",
	}
	result, err := QueryMoA(context.Background(), req, moaResolver(providers), nil)
	if err != nil {
		t.Fatalf("QueryMoA: %v", err)
	}
	if !result.Synthesized {
		t.Error("expected Synthesized=true")
	}
	if result.Text != "synthesized" {
		t.Errorf("Text: got %q, want %q", result.Text, "synthesized")
	}
	if result.Model != "synthesizer" {
		t.Errorf("Model: got %q, want synthesizer", result.Model)
	}
	if len(result.Candidates) != 3 {
		t.Fatalf("Candidates: got %d, want 3", len(result.Candidates))
	}
	for _, c := range result.Candidates {
		if c.Error != "" {
			t.Errorf("candidate %q unexpected error: %s", c.Model, c.Error)
		}
		if c.Text == "" {
			t.Errorf("candidate %q empty text", c.Model)
		}
	}
}

func TestMoA_PartialFailure(t *testing.T) {
	providers := map[string]llm.Provider{
		"modelA":      &moaMockProvider{text: "answer A"},
		"modelB":      &moaMockProvider{err: errors.New("rate limit")},
		"modelC":      &moaMockProvider{text: "answer C"},
		"synthesizer": &moaMockProvider{text: "synthesized from 2"},
	}
	req := MoARequest{
		Prompt:           "q",
		Models:           []string{"modelA", "modelB", "modelC"},
		SynthesizerModel: "synthesizer",
	}
	result, err := QueryMoA(context.Background(), req, moaResolver(providers), nil)
	if err != nil {
		t.Fatalf("QueryMoA: %v", err)
	}
	if !result.Synthesized {
		t.Error("expected Synthesized=true (2 successes)")
	}
	if len(result.Candidates) != 3 {
		t.Fatalf("Candidates: got %d, want 3", len(result.Candidates))
	}
	var errCount, okCount int
	for _, c := range result.Candidates {
		if c.Error != "" {
			errCount++
			if !strings.Contains(c.Error, "rate limit") {
				t.Errorf("candidate error should contain 'rate limit', got: %s", c.Error)
			}
		} else {
			okCount++
		}
	}
	if errCount != 1 {
		t.Errorf("errCount: got %d, want 1", errCount)
	}
	if okCount != 2 {
		t.Errorf("okCount: got %d, want 2", okCount)
	}
}

func TestMoA_AllFailed(t *testing.T) {
	providers := map[string]llm.Provider{
		"modelA": &moaMockProvider{err: errors.New("boom A")},
		"modelB": &moaMockProvider{err: errors.New("boom B")},
	}
	req := MoARequest{
		Prompt: "q",
		Models: []string{"modelA", "modelB"},
	}
	_, err := QueryMoA(context.Background(), req, moaResolver(providers), nil)
	if err == nil {
		t.Fatal("expected error when all models fail")
	}
	if !strings.Contains(err.Error(), "all") {
		t.Errorf("error should mention all-failed, got: %v", err)
	}
}

func TestMoA_SingleSuccess_SkipsSynth(t *testing.T) {
	synth := &moaMockProvider{text: "should not be called"}
	providers := map[string]llm.Provider{
		"modelA":      &moaMockProvider{text: "sole answer"},
		"modelB":      &moaMockProvider{err: errors.New("fail B")},
		"modelC":      &moaMockProvider{err: errors.New("fail C")},
		"synthesizer": synth,
	}
	req := MoARequest{
		Prompt:           "q",
		Models:           []string{"modelA", "modelB", "modelC"},
		SynthesizerModel: "synthesizer",
	}
	result, err := QueryMoA(context.Background(), req, moaResolver(providers), nil)
	if err != nil {
		t.Fatalf("QueryMoA: %v", err)
	}
	if result.Synthesized {
		t.Error("expected Synthesized=false for single success")
	}
	if result.Text != "sole answer" {
		t.Errorf("Text: got %q, want sole answer", result.Text)
	}
	if result.Model != "modelA" {
		t.Errorf("Model: got %q, want modelA", result.Model)
	}
	if synth.callCount.Load() != 0 {
		t.Errorf("synthesizer should not be called when only 1 candidate succeeds; callCount=%d",
			synth.callCount.Load())
	}
}

func TestMoA_CtxCancel_NoLeak(t *testing.T) {
	goroutineStart := runtime.NumGoroutine()
	providers := map[string]llm.Provider{
		"slow1": &moaMockProvider{text: "a", delay: 10 * time.Second},
		"slow2": &moaMockProvider{text: "b", delay: 10 * time.Second},
		"slow3": &moaMockProvider{text: "c", delay: 10 * time.Second},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = QueryMoA(ctx, MoARequest{
		Prompt: "q",
		Models: []string{"slow1", "slow2", "slow3"},
	}, moaResolver(providers), nil)

	// Allow any stragglers to unwind.
	time.Sleep(100 * time.Millisecond)

	delta := runtime.NumGoroutine() - goroutineStart
	if delta > 3 {
		t.Errorf("goroutine leak suspected: delta=%d (start=%d, now=%d)",
			delta, goroutineStart, runtime.NumGoroutine())
	}
}

func TestMoA_TooManyModels(t *testing.T) {
	req := MoARequest{
		Prompt: "q",
		Models: []string{"a", "b", "c", "d", "e", "f"}, // 6 > moaMaxModels (5)
	}
	_, err := QueryMoA(context.Background(), req, moaResolver(nil), nil)
	if err == nil {
		t.Fatal("expected error for len(models)>5")
	}
	if !strings.Contains(err.Error(), "too many") {
		t.Errorf("error should mention 'too many', got: %v", err)
	}
}

func TestMoA_ZeroModels(t *testing.T) {
	req := MoARequest{
		Prompt: "q",
		Models: nil,
	}
	_, err := QueryMoA(context.Background(), req, moaResolver(nil), nil)
	if err == nil {
		t.Fatal("expected error for zero models")
	}
	if !strings.Contains(err.Error(), "no") {
		t.Errorf("error should mention 'no', got: %v", err)
	}
}

func TestMoA_BudgetExhaustion(t *testing.T) {
	// Budget: 30 tokens. Each candidate's usage is 10+20=30. First spend
	// succeeds (cur=0 → 30); subsequent spends see cur=30 and CAS rejects
	// (30+30=60 > 30), triggering cancelAll. But responses were already
	// received, so text is preserved — partial-failure tolerance.
	budget := NewSharedBudget(30)
	providers := map[string]llm.Provider{
		"a":           &moaMockProvider{text: "ans a"},
		"b":           &moaMockProvider{text: "ans b"},
		"c":           &moaMockProvider{text: "ans c"},
		"synthesizer": &moaMockProvider{text: "synthesized"},
	}
	req := MoARequest{
		Prompt:           "q",
		Models:           []string{"a", "b", "c"},
		SynthesizerModel: "synthesizer",
	}
	result, err := QueryMoA(context.Background(), req, moaResolver(providers), budget)
	if err != nil {
		t.Fatalf("QueryMoA should tolerate budget exhaustion: %v", err)
	}
	var okCount int
	for _, c := range result.Candidates {
		if c.Error == "" {
			okCount++
		}
	}
	if okCount == 0 {
		t.Error("expected at least one candidate to succeed")
	}
	if budget.Used() < 30 {
		t.Errorf("budget used: got %d, want ≥ 30 (limit hit)", budget.Used())
	}
	if budget.Used() > 30 {
		// TrySpend rejects overshoots via CAS, so used stays at the limit.
		t.Errorf("budget overshoot: used=%d (limit=30)", budget.Used())
	}
}

// TestMoA_TruncateHelper verifies the synthesizer-input truncation logic.
func TestMoA_TruncateHelper(t *testing.T) {
	short := "hello"
	if got := moaTruncate(short, 100); got != short {
		t.Errorf("short truncate: got %q, want %q", got, short)
	}
	long := strings.Repeat("x", 200)
	got := moaTruncate(long, 50)
	if !strings.HasSuffix(got, "[truncated]") {
		t.Errorf("long truncate missing suffix: %q", got)
	}
	if len(got) != 50+len("\n...[truncated]") {
		t.Errorf("long truncate length: got %d, want %d", len(got), 50+len("\n...[truncated]"))
	}
}
