package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinto/gopaw/core"
)

// sseLines joins lines with \n to build an SSE response body.
func sseLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func newClaudeTestServer(handler http.HandlerFunc) (*httptest.Server, *ClaudeProvider) {
	srv := httptest.NewServer(handler)
	p := NewClaude("test-key", "claude-3-opus-20240229", 1024,
		WithClaudeHTTPClient(srv.Client()),
		WithClaudeBaseURL(srv.URL),
	)
	return srv, p
}

func TestClaudeJSONResponse(t *testing.T) {
	body := `{
		"content": [{"type":"text","text":"Hello, world!"}],
		"usage": {"input_tokens": 10, "output_tokens": 5},
		"model": "claude-3-opus-20240229"
	}`

	srv, p := newClaudeTestServer(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("anthropic-version"); got != claudeAPIVersion {
			t.Errorf("anthropic-version = %q, want %q", got, claudeAPIVersion)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	})
	defer srv.Close()

	resp, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if resp.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello, world!")
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
	if resp.Usage.Model != "claude-3-opus-20240229" {
		t.Errorf("Model = %q, want %q", resp.Usage.Model, "claude-3-opus-20240229")
	}
}

func TestClaudeSSEStream(t *testing.T) {
	sseBody := sseLines(
		"event: message_start",
		`data: {"type":"message_start","message":{"model":"claude-3-opus-20240229","usage":{"input_tokens":25}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":", world!"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","usage":{"output_tokens":8}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	srv, p := newClaudeTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	})
	defer srv.Close()

	var tokens []string
	resp, err := p.GenerateStream(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "Hi"},
	}, func(token string) {
		tokens = append(tokens, token)
	})
	if err != nil {
		t.Fatalf("GenerateStream() error: %v", err)
	}

	if resp.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello, world!")
	}
	if len(tokens) != 2 {
		t.Errorf("got %d token callbacks, want 2", len(tokens))
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.InputTokens != 25 {
		t.Errorf("InputTokens = %d, want 25", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 8 {
		t.Errorf("OutputTokens = %d, want 8", resp.Usage.OutputTokens)
	}
}

func TestClaudeSSEStreamNilCallback(t *testing.T) {
	// Calling parseSSEStream with nil onToken must not panic.
	sseBody := sseLines(
		"event: message_start",
		`data: {"type":"message_start","message":{"model":"claude-3-opus-20240229","usage":{"input_tokens":5}}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","usage":{"output_tokens":1}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	resp, err := (&ClaudeProvider{}).parseSSEStream(strings.NewReader(sseBody), nil)
	if err != nil {
		t.Fatalf("parseSSEStream(nil callback) error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
}

func TestClaudeSystemMessageSplit(t *testing.T) {
	var receivedBody string
	srv, p := newClaudeTestServer(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 4096)
		n, _ := r.Body.Read(b)
		receivedBody = string(b[:n])
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1},"model":"claude-3-opus-20240229"}`)
	})
	defer srv.Close()

	_, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleSystem, Content: "You are helpful."},
		{Role: core.RoleUser, Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if !strings.Contains(receivedBody, `"system"`) {
		t.Error("request body missing 'system' field")
	}
	// System messages should NOT appear in the messages array
	if strings.Contains(receivedBody, `"role":"system"`) {
		t.Error("system role should not be in messages array for Claude API")
	}
}

func TestClaudeRetryOn429(t *testing.T) {
	attempts := 0
	srv, p := newClaudeTestServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1},"model":"test"}`)
	})
	defer srv.Close()

	resp, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("Generate() after retries error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestClaudeContextWindow(t *testing.T) {
	p := NewClaude("key", "claude-3-opus-20240229", 1024)
	if p.ContextWindow() != claudeDefaultWindow {
		t.Errorf("ContextWindow() = %d, want %d", p.ContextWindow(), claudeDefaultWindow)
	}
	if p.MaxTokens() != 1024 {
		t.Errorf("MaxTokens() = %d, want 1024", p.MaxTokens())
	}

	// Non-claude model gets fallback window
	p2 := NewClaude("key", "some-other-model", 512)
	if p2.ContextWindow() != claudeFallbackWindow {
		t.Errorf("ContextWindow() = %d, want %d", p2.ContextWindow(), claudeFallbackWindow)
	}
}
