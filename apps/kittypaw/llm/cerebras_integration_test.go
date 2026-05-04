//go:build cerebras_integration

package llm

import (
	"context"
	"os"
	"testing"

	"github.com/jinto/kittypaw/core"
)

// TestCerebrasLiveSmoke validates the wire round-trip against the real
// Cerebras Cloud endpoint when CEREBRAS_API_KEY is set. Build-tagged so the
// default `make test` skips it (no key, no network); run with
//
//	CEREBRAS_API_KEY=... go test -tags cerebras_integration -v -run TestCerebrasLiveSmoke ./llm/
//
// Pins AC-8 (plan: openai-tool-calling.md): Phase 1 wire + Phase 2 registry
// case both surface a Korean reply through the free tier (Qwen3-235B).
func TestCerebrasLiveSmoke(t *testing.T) {
	apiKey := os.Getenv("CEREBRAS_API_KEY")
	if apiKey == "" {
		t.Skip("CEREBRAS_API_KEY not set; skipping live smoke")
	}

	p, err := NewProvider("cerebras", apiKey, "qwen-3-235b-a22b-instruct-2507", 256)
	if err != nil {
		t.Fatalf("NewProvider(cerebras): %v", err)
	}

	resp, err := p.Generate(context.Background(), []core.LlmMessage{
		{Role: core.RoleUser, Content: "안녕? 한 줄로 자기소개 해줘."},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("empty response content from Cerebras")
	}
	t.Logf("Cerebras model=%s response=%q", "qwen-3-235b-a22b-instruct-2507", resp.Content)
}
