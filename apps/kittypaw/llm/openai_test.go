package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinto/gopaw/core"
)

func newOpenAITestServer(handler http.HandlerFunc) (*httptest.Server, *OpenAIProvider) {
	srv := httptest.NewServer(handler)
	p := NewOpenAI("test-key", "gpt-4", 1024,
		WithHTTPClient(srv.Client()),
		WithBaseURL(srv.URL),
	)
	return srv, p
}

func TestOpenAIJSONResponse(t *testing.T) {
	body := `{
		"choices": [{"message": {"content": "Hello!"}}],
		"usage": {"prompt_tokens": 12, "completion_tokens": 3},
		"model": "gpt-4"
	}`

	srv, p := newOpenAITestServer(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
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
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.InputTokens != 12 {
		t.Errorf("InputTokens = %d, want 12", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", resp.Usage.OutputTokens)
	}
}

func TestOpenAISSEStream(t *testing.T) {
	sseBody := sseLines(
		`data: {"choices":[{"delta":{"content":"Hello"}}],"model":"gpt-4"}`,
		"",
		`data: {"choices":[{"delta":{"content":", world!"}}],"model":"gpt-4"}`,
		"",
		`data: {"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":7},"model":"gpt-4"}`,
		"",
		"data: [DONE]",
	)

	srv, p := newOpenAITestServer(func(w http.ResponseWriter, r *http.Request) {
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
	if resp.Usage.InputTokens != 20 {
		t.Errorf("InputTokens = %d, want 20", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 7 {
		t.Errorf("OutputTokens = %d, want 7", resp.Usage.OutputTokens)
	}
}

func TestOpenAISSEStreamNilCallback(t *testing.T) {
	sseBody := sseLines(
		`data: {"choices":[{"delta":{"content":"ok"}}],"model":"gpt-4"}`,
		"",
		"data: [DONE]",
	)

	resp, err := (&OpenAIProvider{}).parseSSEStream(strings.NewReader(sseBody), nil)
	if err != nil {
		t.Fatalf("parseSSEStream(nil callback) error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
}

func TestOpenAIStreamOptionsPresent(t *testing.T) {
	// When baseURL is the default OpenAI URL, stream_options must be present.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}

		// stream must be true
		if req["stream"] != true {
			t.Error("stream should be true")
		}

		// stream_options must exist with include_usage: true
		so, ok := req["stream_options"].(map[string]any)
		if !ok {
			t.Fatal("stream_options missing in streaming request")
		}
		if so["include_usage"] != true {
			t.Errorf("include_usage = %v, want true", so["include_usage"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n")
	}))
	defer srv.Close()

	// Use default base URL but point client at test server via custom transport.
	p := NewOpenAI("key", "gpt-4", 1024,
		WithHTTPClient(srv.Client()),
		// Keep baseURL as default so stream_options is included.
	)
	// Override internal baseURL to test server while keeping the default URL for the guard check.
	// We need a different approach: verify via buildRequestBody directly.
	body := p.buildRequestBody([]core.LlmMessage{{Role: core.RoleUser, Content: "Hi"}}, true)
	so, ok := body["stream_options"].(map[string]any)
	if !ok {
		t.Fatal("stream_options missing when baseURL is default")
	}
	if so["include_usage"] != true {
		t.Errorf("include_usage = %v, want true", so["include_usage"])
	}

	srv.Close()
}

func TestOpenAIStreamOptionsAbsentForNonStreaming(t *testing.T) {
	p := NewOpenAI("key", "gpt-4", 1024)
	body := p.buildRequestBody([]core.LlmMessage{{Role: core.RoleUser, Content: "Hi"}}, false)
	if _, ok := body["stream_options"]; ok {
		t.Error("stream_options should NOT be present in non-streaming request")
	}
	if _, ok := body["stream"]; ok {
		t.Error("stream should NOT be present in non-streaming request")
	}
}

func TestOpenAIStreamOptionsAbsentForOllama(t *testing.T) {
	// Custom base URL (e.g. Ollama) should NOT get stream_options.
	p := NewOpenAI("", "llama3", 1024,
		WithBaseURL("http://localhost:11434/v1/chat/completions"),
	)
	body := p.buildRequestBody([]core.LlmMessage{{Role: core.RoleUser, Content: "Hi"}}, true)
	if _, ok := body["stream_options"]; ok {
		t.Error("stream_options should NOT be present for custom base URL (Ollama)")
	}
	// stream itself should still be true
	if body["stream"] != true {
		t.Error("stream should be true even for Ollama")
	}
}

func TestOpenAINoAuthHeaderWhenKeyEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"model":"test"}`)
	}))
	defer srv.Close()

	p := NewOpenAI("", "llama3", 1024,
		WithHTTPClient(srv.Client()),
		WithBaseURL(srv.URL),
	)

	_, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header should be empty for no API key, got %q", gotAuth)
	}
}
