//go:build ollama_integration

package llm

import (
	"context"
	"os"
	"testing"

	"github.com/jinto/kittypaw/core"
)

// TestOllamaLiveSmoke validates the wire round-trip against an Ollama
// daemon. No API key required; just a running daemon. Build-tagged so the
// default `make test` skips it.
//
// Run with:
//
//	OLLAMA_TEST_MODEL=qwen3:30b-a3b \
//	  go test -tags ollama_integration -v -run TestOllamaLiveSmoke ./llm/
//
// Override `OLLAMA_BASE_URL` to point at a remote daemon — e.g. SSH-tunnelled
// to a beefier Mac:
//
//	ssh -fN -L 11500:localhost:11434 m3-mac
//	OLLAMA_TEST_MODEL=qwen3:30b-a3b \
//	OLLAMA_BASE_URL=http://localhost:11500/v1/chat/completions \
//	  go test -tags ollama_integration -v -run TestOllamaLiveSmoke ./llm/
//
// Defaults target a 36 GB-class machine. For 16 GB use qwen3:8b /
// granite4.1:8b / llama3.1:8b.
func TestOllamaLiveSmoke(t *testing.T) {
	if os.Getenv("OLLAMA_SKIP") != "" {
		t.Skip("OLLAMA_SKIP set; skipping live smoke")
	}
	model := os.Getenv("OLLAMA_TEST_MODEL")
	if model == "" {
		model = "qwen3:30b-a3b"
	}
	baseURL := os.Getenv("OLLAMA_BASE_URL") // empty → registry default

	cfg := core.ModelConfig{
		Provider:  "ollama",
		Model:     model,
		MaxTokens: 256,
		BaseURL:   baseURL,
	}
	p, err := NewProviderFromModelConfig(cfg)
	if err != nil {
		t.Fatalf("NewProviderFromModelConfig(ollama): %v", err)
	}

	resp, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "안녕? 한 줄로 자기소개 해줘."},
	})
	if err != nil {
		t.Fatalf("Generate: %v (is the ollama daemon running and the model pulled? `ollama pull %s`)", err, model)
	}
	if resp.Content == "" {
		t.Fatal("empty response content from Ollama")
	}
	t.Logf("Ollama model=%s base=%q response=%q", model, baseURL, resp.Content)
}
