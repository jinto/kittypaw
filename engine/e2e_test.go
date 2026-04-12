package engine

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/llm"
	"github.com/jinto/gopaw/sandbox"
	"github.com/jinto/gopaw/store"
)

// --- test helpers ---

func skipWithoutRuntime(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("deno"); err == nil {
		return
	}
	if _, err := exec.LookPath("node"); err == nil {
		return
	}
	t.Skip("no JS runtime (deno or node) available")
}

// mockProvider is a queue-based mock that pops responses on each Generate call.
type mockProvider struct {
	responses []*llm.Response
	callIdx   int
}

func (m *mockProvider) Generate(ctx context.Context, msgs []core.LlmMessage) (*llm.Response, error) {
	return m.GenerateStream(ctx, msgs, nil)
}

func (m *mockProvider) GenerateStream(ctx context.Context, msgs []core.LlmMessage, onToken llm.TokenCallback) (*llm.Response, error) {
	if m.callIdx >= len(m.responses) {
		return nil, context.DeadlineExceeded
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func (m *mockProvider) ContextWindow() int { return 128_000 }
func (m *mockProvider) MaxTokens() int     { return 4096 }

func mockResp(code string) *llm.Response {
	return &llm.Response{
		Content: code,
		Usage:   &llm.TokenUsage{InputTokens: 10, OutputTokens: 5, Model: "mock"},
	}
}

func newTestSession(t *testing.T, responses ...*llm.Response) *Session {
	t.Helper()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := core.DefaultConfig()

	return &Session{
		Provider: &mockProvider{responses: responses},
		Sandbox:  sandbox.New(cfg.Sandbox),
		Store:    st,
		Config:   &cfg,
	}
}

func webChatEvent(text string) core.Event {
	payload, _ := json.Marshal(core.ChatPayload{
		ChatID:    "test-chat",
		Text:      text,
		SessionID: "test-session",
	})
	return core.Event{Type: core.EventWebChat, Payload: payload}
}

// --- E2E tests ---

func TestE2ESimpleReturn(t *testing.T) {
	skipWithoutRuntime(t)

	sess := newTestSession(t, mockResp(`return "Hello from agent";`))
	event := webChatEvent("say hello")

	output, err := sess.Run(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if output != "Hello from agent" {
		t.Errorf("output = %q, want %q", output, "Hello from agent")
	}
}

func TestE2ESkillCall(t *testing.T) {
	skipWithoutRuntime(t)

	code := `
		Storage.set("greeting", "hi there");
		const result = Storage.get("greeting");
		return result;
	`
	sess := newTestSession(t, mockResp(code))
	event := webChatEvent("store something")

	output, err := sess.Run(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	// The skill call chain: sandbox → resolveSkillCall → executeStorage → Store
	// Storage.get returns {value: "..."} which is then JSON.stringify'd by the sandbox.
	if output == "" || output == "null" {
		t.Errorf("expected non-empty output from Storage round-trip, got %q", output)
	}
	t.Logf("Storage round-trip output: %s", output)

	// Verify the value was persisted in the real SQLite store.
	val, ok, err := sess.Store.StorageGet("default", "greeting")
	if err != nil {
		t.Fatalf("StorageGet error: %v", err)
	}
	if !ok {
		t.Fatal("expected greeting key to exist in store")
	}
	if val == "" {
		t.Error("expected non-empty value for greeting key")
	}
	t.Logf("Store value for 'greeting': %s", val)
}

func TestE2EErrorRetry(t *testing.T) {
	skipWithoutRuntime(t)

	mock := &mockProvider{responses: []*llm.Response{
		mockResp(`throw new Error("boom");`),
		mockResp(`return "recovered";`),
	}}

	cfg := core.DefaultConfig()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sess := &Session{
		Provider: mock,
		Sandbox:  sandbox.New(cfg.Sandbox),
		Store:    st,
		Config:   &cfg,
	}
	event := webChatEvent("try something")

	output, err := sess.Run(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if output != "recovered" {
		t.Errorf("output = %q, want %q", output, "recovered")
	}
	// The LLM should have been called twice: once for the error, once for recovery.
	if mock.callIdx != 2 {
		t.Errorf("mock.callIdx = %d, want 2", mock.callIdx)
	}
}
