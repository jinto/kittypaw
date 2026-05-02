package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestGeminiGenerateContentRequestAndResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-3.1-pro-preview:generateContent" {
			t.Errorf("path = %q, want model generateContent endpoint", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "test-key" {
			t.Errorf("key query = %q, want test-key", got)
		}

		var body geminiGenerateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.SystemInstruction == nil || len(body.SystemInstruction.Parts) != 1 || body.SystemInstruction.Parts[0].Text != "You are concise." {
			t.Fatalf("systemInstruction = %+v, want system text", body.SystemInstruction)
		}
		if len(body.Contents) != 2 {
			t.Fatalf("contents len = %d, want 2", len(body.Contents))
		}
		if body.Contents[0].Role != "user" || body.Contents[0].Parts[0].Text != "Hi" {
			t.Fatalf("first content = %+v, want user Hi", body.Contents[0])
		}
		if body.Contents[1].Role != "model" || body.Contents[1].Parts[0].Text != "Hello" {
			t.Fatalf("second content = %+v, want model Hello", body.Contents[1])
		}
		if body.GenerationConfig == nil || body.GenerationConfig.MaxOutputTokens != 1024 {
			t.Fatalf("generationConfig = %+v, want maxOutputTokens 1024", body.GenerationConfig)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [{
				"content": {"role": "model", "parts": [{"text": "Gemini says hi"}]},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 11, "candidatesTokenCount": 4},
			"modelVersion": "gemini-3.1-pro-preview"
		}`)
	}))
	defer srv.Close()

	p := NewGemini("test-key", "gemini-3.1-pro-preview", 1024,
		WithGeminiHTTPClient(srv.Client()),
		WithGeminiBaseURL(srv.URL+"/v1beta/models"),
	)

	resp, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleSystem, Content: "You are concise."},
		{Role: core.RoleUser, Content: "Hi"},
		{Role: core.RoleAssistant, Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if resp.Content != "Gemini says hi" {
		t.Errorf("Content = %q, want %q", resp.Content, "Gemini says hi")
	}
	if resp.StopReason != "STOP" {
		t.Errorf("StopReason = %q, want STOP", resp.StopReason)
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.InputTokens != 11 {
		t.Errorf("InputTokens = %d, want 11", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 4 {
		t.Errorf("OutputTokens = %d, want 4", resp.Usage.OutputTokens)
	}
}
