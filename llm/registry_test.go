package llm

import (
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestNewProviderClaude(t *testing.T) {
	p, err := NewProvider("anthropic", "test-key", "claude-3-opus-20240229", 1024)
	if err != nil {
		t.Fatalf("NewProvider(anthropic) error: %v", err)
	}
	if _, ok := p.(*ClaudeProvider); !ok {
		t.Errorf("expected *ClaudeProvider, got %T", p)
	}
}

func TestNewProviderOpenAI(t *testing.T) {
	p, err := NewProvider("openai", "test-key", "gpt-4", 1024)
	if err != nil {
		t.Fatalf("NewProvider(openai) error: %v", err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Errorf("expected *OpenAIProvider, got %T", p)
	}
}

func TestNewProviderOllama(t *testing.T) {
	p, err := NewProvider("ollama", "", "llama3", 1024)
	if err != nil {
		t.Fatalf("NewProvider(ollama) error: %v", err)
	}
	op, ok := p.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider for ollama, got %T", p)
	}
	if op.baseURL != ollamaDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", op.baseURL, ollamaDefaultBaseURL)
	}
}

func TestNewProviderGemini(t *testing.T) {
	p, err := NewProvider("gemini", "test-key", "gemini-3.1-pro-preview", 1024)
	if err != nil {
		t.Fatalf("NewProvider(gemini) error: %v", err)
	}
	if _, ok := p.(*GeminiProvider); !ok {
		t.Errorf("expected *GeminiProvider, got %T", p)
	}
}

func TestNewProviderUnknown(t *testing.T) {
	_, err := NewProvider("unknown", "key", "model", 1024)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewProviderAliases(t *testing.T) {
	// "claude" alias → ClaudeProvider
	p, err := NewProvider("claude", "key", "model", 1024)
	if err != nil {
		t.Fatalf("NewProvider(claude) error: %v", err)
	}
	if _, ok := p.(*ClaudeProvider); !ok {
		t.Errorf("expected *ClaudeProvider for alias 'claude', got %T", p)
	}

	// "gpt" alias → OpenAIProvider
	p, err = NewProvider("gpt", "key", "model", 1024)
	if err != nil {
		t.Fatalf("NewProvider(gpt) error: %v", err)
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Errorf("expected *OpenAIProvider for alias 'gpt', got %T", p)
	}
}

func TestNewProviderFromConfig(t *testing.T) {
	p, err := NewProviderFromConfig(core.LLMConfig{
		Provider:  "anthropic",
		APIKey:    "key",
		Model:     "claude-3-opus-20240229",
		MaxTokens: 2048,
	})
	if err != nil {
		t.Fatalf("NewProviderFromConfig() error: %v", err)
	}
	cp, ok := p.(*ClaudeProvider)
	if !ok {
		t.Fatalf("expected *ClaudeProvider, got %T", p)
	}
	if cp.maxTokens != 2048 {
		t.Errorf("maxTokens = %d, want 2048", cp.maxTokens)
	}
}

func TestNewProviderFromModelConfig(t *testing.T) {
	p, err := NewProviderFromModelConfig(core.ModelConfig{
		Provider:      "openai",
		APIKey:        "key",
		Model:         "gpt-4",
		MaxTokens:     512,
		BaseURL:       "http://custom:8080/v1",
		ContextWindow: 32000,
	})
	if err != nil {
		t.Fatalf("NewProviderFromModelConfig() error: %v", err)
	}
	op, ok := p.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", p)
	}
	if op.baseURL != "http://custom:8080/v1" {
		t.Errorf("baseURL = %q, want %q", op.baseURL, "http://custom:8080/v1")
	}
	if op.contextWindow != 32000 {
		t.Errorf("contextWindow = %d, want 32000", op.contextWindow)
	}
}
