package llm

import (
	"os"
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

func TestNewProviderCerebras(t *testing.T) {
	// Cerebras Cloud is OpenAI-compatible — provider name resolves to an
	// OpenAIProvider in chat mode pointed at api.cerebras.ai with the free
	// tier's 8K context cap baked in.
	p, err := NewProvider("cerebras", "test-key", "qwen-3-235b", 1024)
	if err != nil {
		t.Fatalf("NewProvider(cerebras) error: %v", err)
	}
	op, ok := p.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider for cerebras, got %T", p)
	}
	if op.apiMode != openAIAPIModeChat {
		t.Errorf("apiMode = %q, want chat", op.apiMode)
	}
	if op.baseURL != cerebrasDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", op.baseURL, cerebrasDefaultBaseURL)
	}
	if op.contextWindow != cerebrasFreeContextWindow {
		t.Errorf("contextWindow = %d, want %d (free-tier cap)", op.contextWindow, cerebrasFreeContextWindow)
	}
}

func TestNewProviderCerebrasBaseURLOverride(t *testing.T) {
	// Custom base_url (paid tier, regional endpoint, mock) wins over the
	// default — pin via NewProviderFromModelConfig path.
	p, err := NewProviderFromModelConfig(core.ModelConfig{
		Provider:  "cerebras",
		APIKey:    "k",
		Model:     "qwen-3-235b",
		MaxTokens: 1024,
		BaseURL:   "http://localhost:9999/v1/chat/completions",
	})
	if err != nil {
		t.Fatalf("NewProviderFromModelConfig(cerebras) error: %v", err)
	}
	op := p.(*OpenAIProvider)
	if op.baseURL != "http://localhost:9999/v1/chat/completions" {
		t.Errorf("baseURL = %q, want override", op.baseURL)
	}
}

func TestNewProviderCerebrasContextWindowOverride(t *testing.T) {
	// Paid-tier callers can lift the 8K cap via ModelConfig.ContextWindow.
	p, err := NewProviderFromModelConfig(core.ModelConfig{
		Provider:      "cerebras",
		APIKey:        "k",
		Model:         "qwen-3-235b",
		MaxTokens:     1024,
		ContextWindow: 65536,
	})
	if err != nil {
		t.Fatalf("NewProviderFromModelConfig(cerebras) error: %v", err)
	}
	op := p.(*OpenAIProvider)
	if op.contextWindow != 65536 {
		t.Errorf("contextWindow = %d, want 65536 (override)", op.contextWindow)
	}
}

func TestNewProviderCerebrasAPIKeyFromEnv(t *testing.T) {
	t.Setenv("CEREBRAS_API_KEY", "env-key")
	p, err := NewProvider("cerebras", "", "qwen-3-235b", 1024)
	if err != nil {
		t.Fatalf("NewProvider(cerebras) error: %v", err)
	}
	op := p.(*OpenAIProvider)
	if op.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want %q (from CEREBRAS_API_KEY)", op.apiKey, "env-key")
	}
}

func TestEnvAPIKeyCerebras(t *testing.T) {
	// Direct envAPIKey lookup — protects the table from silent typos in the
	// env var name.
	t.Setenv("CEREBRAS_API_KEY", "v")
	if got := envAPIKey("cerebras"); got != "v" {
		t.Errorf("envAPIKey(cerebras) = %q, want v", got)
	}
	// Case-insensitive: provider name normalisation matches the switch.
	t.Setenv("CEREBRAS_API_KEY", "")
	_ = os.Unsetenv("CEREBRAS_API_KEY")
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
