//go:build groq_integration

package llm

import (
	"context"
	"os"
	"testing"

	"github.com/jinto/kittypaw/core"
)

// TestGroqLiveSmoke validates the wire round-trip against the real Groq Cloud
// endpoint when GROQ_API_KEY is set. Build-tagged so the default `make test`
// skips it (no key, no network); run with
//
//	GROQ_API_KEY=... go test -tags groq_integration -v -run TestGroqLiveSmoke ./llm/
//
// The default model is llama-3.3-70b-versatile (always available on free
// tier). Qwen3-32B is more accurate in Korean but is org-gated — set
// GROQ_TEST_MODEL=qwen/qwen3-32b after enabling the model at
// https://console.groq.com/settings/limits to exercise it.
func TestGroqLiveSmoke(t *testing.T) {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		t.Skip("GROQ_API_KEY not set; skipping live smoke")
	}
	model := os.Getenv("GROQ_TEST_MODEL")
	if model == "" {
		model = "llama-3.3-70b-versatile"
	}

	p, err := NewProvider("groq", apiKey, model, 256)
	if err != nil {
		t.Fatalf("NewProvider(groq): %v", err)
	}

	resp, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "안녕? 한 줄로 자기소개 해줘."},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("empty response content from Groq")
	}
	t.Logf("Groq model=%s response=%q", model, resp.Content)
}
