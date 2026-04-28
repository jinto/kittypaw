package llm

import (
	"context"

	"github.com/jinto/kittypaw/core"
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
//
// Content carries the concatenated text blocks for the historical
// plain-text consumers. ContentBlocks carries the full structured
// response (text + tool_use + …) for callers that need the wire-shape
// — e.g. a tool-use loop has to inspect tool_use blocks rather than
// the flattened text. StopReason mirrors Anthropic's field
// ("end_turn", "tool_use", "max_tokens", …) so a tool-use loop can
// decide whether to keep iterating. Both new fields are zero-valued
// for providers that don't surface them — pre-existing callers that
// only read .Content are unaffected.
type Response struct {
	Content       string              `json:"content"`
	ContentBlocks []core.ContentBlock `json:"content_blocks,omitempty"`
	StopReason    string              `json:"stop_reason,omitempty"`
	Usage         *TokenUsage         `json:"usage,omitempty"`
}

// Tool describes a callable surface the model may invoke during a
// generation. The schema follows Anthropic's tool definition shape
// (name + description + JSON-schema input). For non-Claude providers
// this struct is opaque — they ignore tools and degrade to plain
// generation.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
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

	// GenerateWithTools sends messages along with a tool definition
	// list and returns the next response. When tools is non-empty the
	// provider is expected to surface tool_use ContentBlocks +
	// StopReason on the Response so the caller can drive an iteration
	// loop. Providers without native tool support ignore tools and
	// degrade to Generate semantics.
	GenerateWithTools(ctx context.Context, messages []core.LlmMessage, tools []Tool) (*Response, error)

	// ContextWindow returns the model's context window size in tokens.
	ContextWindow() int

	// MaxTokens returns the maximum output tokens.
	MaxTokens() int
}
