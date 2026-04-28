package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
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

func TestOpenAIBuildRequestBodyShape(t *testing.T) {
	// After Phase 13.3 the wire is plain non-streaming JSON. Pin
	// that — no stream/stream_options keys leak into the body.
	p := NewOpenAI("key", "gpt-4", 1024)
	body := p.buildRequestBody([]core.LlmMessage{{Role: core.RoleUser, Content: "Hi"}})
	if _, ok := body["stream"]; ok {
		t.Error("stream key must not appear in non-streaming request")
	}
	if _, ok := body["stream_options"]; ok {
		t.Error("stream_options must not appear in non-streaming request")
	}
	if body["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", body["model"])
	}
	if body["max_tokens"] != 1024 {
		t.Errorf("max_tokens = %v, want 1024", body["max_tokens"])
	}
}

func TestOpenAIRetryOn429(t *testing.T) {
	attempts := 0
	srv, p := newOpenAITestServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1},"model":"gpt-4"}`)
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

func TestOpenAIRetryOn503(t *testing.T) {
	attempts := 0
	srv, p := newOpenAITestServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1},"model":"gpt-4"}`)
	})
	defer srv.Close()

	resp, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("Generate() after 503 retry error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestOpenAIRetryExhausted(t *testing.T) {
	attempts := 0
	srv, p := newOpenAITestServer(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer srv.Close()

	_, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "Hi"},
	})
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
	if !strings.Contains(err.Error(), "retries exhausted") {
		t.Errorf("err = %q, want to contain 'retries exhausted'", err.Error())
	}
	// 1 initial + 3 retries = 4 attempts
	if attempts != 4 {
		t.Errorf("attempts = %d, want 4", attempts)
	}
}

func TestOpenAIRetryCancelledContext(t *testing.T) {
	srv, p := newOpenAITestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Generate(ctx, []core.LlmMessage{
		{Role: core.RoleUser, Content: "Hi"},
	})
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
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
