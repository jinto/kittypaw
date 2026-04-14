package llm

import (
	"fmt"
	"os"
	"strings"

	"github.com/jinto/kittypaw/core"
)

const ollamaDefaultBaseURL = "http://localhost:11434/v1/chat/completions"

// Option is a functional option for NewProvider.
type Option func(*providerOpts)

type providerOpts struct {
	baseURL       string
	contextWindow int
}

// WithProviderBaseURL sets a custom base URL for the provider.
func WithProviderBaseURL(url string) Option {
	return func(o *providerOpts) {
		o.baseURL = url
	}
}

// WithProviderContextWindow sets a custom context window size.
func WithProviderContextWindow(size int) Option {
	return func(o *providerOpts) {
		o.contextWindow = size
	}
}

// NewProvider creates a Provider from config parameters.
// If apiKey is empty, falls back to the standard environment variable
// for the given provider (ANTHROPIC_API_KEY, OPENAI_API_KEY).
func NewProvider(provider, apiKey, model string, maxTokens int, opts ...Option) (Provider, error) {
	if apiKey == "" {
		apiKey = envAPIKey(provider)
	}

	var o providerOpts
	for _, opt := range opts {
		opt(&o)
	}

	switch strings.ToLower(provider) {
	case "anthropic", "claude":
		return NewClaude(apiKey, model, maxTokens), nil

	case "openai", "gpt":
		var openaiOpts []OpenAIOption
		if o.baseURL != "" {
			openaiOpts = append(openaiOpts, WithBaseURL(o.baseURL))
		}
		if o.contextWindow > 0 {
			openaiOpts = append(openaiOpts, WithContextWindow(o.contextWindow))
		}
		return NewOpenAI(apiKey, model, maxTokens, openaiOpts...), nil

	case "ollama":
		baseURL := ollamaDefaultBaseURL
		if o.baseURL != "" {
			baseURL = o.baseURL
		}
		return NewOpenAI(apiKey, model, maxTokens,
			WithBaseURL(baseURL),
		), nil

	default:
		return nil, fmt.Errorf("llm: unknown provider %q", provider)
	}
}

// NewProviderFromConfig creates a Provider from an LLMConfig.
func NewProviderFromConfig(cfg core.LLMConfig) (Provider, error) {
	var opts []Option
	if cfg.BaseURL != "" {
		opts = append(opts, WithProviderBaseURL(cfg.BaseURL))
	}
	return NewProvider(cfg.Provider, cfg.APIKey, cfg.Model, int(cfg.MaxTokens), opts...)
}

// NewProviderFromModelConfig creates a Provider from a ModelConfig.
func NewProviderFromModelConfig(cfg core.ModelConfig) (Provider, error) {
	var opts []Option
	if cfg.BaseURL != "" {
		opts = append(opts, WithProviderBaseURL(cfg.BaseURL))
	}
	if cfg.ContextWindow > 0 {
		opts = append(opts, WithProviderContextWindow(int(cfg.ContextWindow)))
	}
	return NewProvider(cfg.Provider, cfg.APIKey, cfg.Model, int(cfg.MaxTokens), opts...)
}

// envAPIKey returns the standard API key environment variable for a provider.
func envAPIKey(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic", "claude":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "openai", "gpt":
		return os.Getenv("OPENAI_API_KEY")
	default:
		return ""
	}
}
