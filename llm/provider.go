package llm

import (
	"context"

	"github.com/jinto/gopaw/core"
)

// TokenUsage tracks token consumption for a single LLM call.
type TokenUsage struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens,omitempty"`
	Model                    string `json:"model"`
}

// Response is the result of an LLM generation call.
type Response struct {
	Content string      `json:"content"`
	Usage   *TokenUsage `json:"usage,omitempty"`
}

// TokenCallback receives streaming tokens as they arrive.
type TokenCallback func(token string)

// Provider is the interface all LLM backends must implement.
type Provider interface {
	// Generate sends messages and returns a complete response.
	Generate(ctx context.Context, messages []core.LlmMessage) (*Response, error)

	// GenerateStream sends messages and streams tokens via the callback.
	// Returns the complete response when done.
	GenerateStream(ctx context.Context, messages []core.LlmMessage, onToken TokenCallback) (*Response, error)

	// ContextWindow returns the model's context window size in tokens.
	ContextWindow() int

	// MaxTokens returns the maximum output tokens.
	MaxTokens() int
}
