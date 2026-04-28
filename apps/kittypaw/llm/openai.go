package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/jinto/kittypaw/core"
)

const (
	openAIDefaultBaseURL = "https://api.openai.com/v1/chat/completions"
	openAIDefaultWindow  = 128_000
	openAIMaxRetries     = 3
	openAIBaseDelay      = 1 * time.Second
)

// OpenAIProvider implements Provider for the OpenAI Chat Completions API.
// It also supports any OpenAI-compatible endpoint (e.g. Ollama) via a
// configurable base URL.
type OpenAIProvider struct {
	apiKey        string
	model         string
	maxTokens     int
	contextWindow int
	baseURL       string
	client        *http.Client
}

// OpenAIOption is a functional option for NewOpenAI.
type OpenAIOption func(*OpenAIProvider)

// WithBaseURL overrides the default OpenAI API endpoint.
func WithBaseURL(url string) OpenAIOption {
	return func(p *OpenAIProvider) {
		p.baseURL = url
	}
}

// WithContextWindow overrides the default context window size.
func WithContextWindow(size int) OpenAIOption {
	return func(p *OpenAIProvider) {
		p.contextWindow = size
	}
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(c *http.Client) OpenAIOption {
	return func(p *OpenAIProvider) {
		p.client = c
	}
}

// NewOpenAI creates an OpenAIProvider for the given model.
func NewOpenAI(apiKey, model string, maxTokens int, opts ...OpenAIOption) *OpenAIProvider {
	p := &OpenAIProvider{
		apiKey:        apiKey,
		model:         model,
		maxTokens:     maxTokens,
		contextWindow: openAIDefaultWindow,
		baseURL:       openAIDefaultBaseURL,
		client:        &http.Client{Timeout: 5 * time.Minute},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ContextWindow returns the model's context window size in tokens.
func (o *OpenAIProvider) ContextWindow() int { return o.contextWindow }

// MaxTokens returns the maximum output tokens.
func (o *OpenAIProvider) MaxTokens() int { return o.maxTokens }

// Generate sends messages and returns a complete response. Wire is
// plain JSON (`stream: false`) — see Provider docs for why streaming
// was removed in Phase 13.3.
func (o *OpenAIProvider) Generate(ctx context.Context, messages []core.LlmMessage) (*Response, error) {
	body := o.buildRequestBody(messages)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}
	resp, err := o.doWithRetry(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return o.parseJSONResponse(resp.Body)
}

// GenerateWithTools degrades to Generate — the current OpenAI wire
// builder does not emit native tool definitions. Callers can still
// invoke this method; the tools argument is ignored. A future commit
// can wire OpenAI's function-calling shape if cross-provider tool use
// becomes load-bearing. For now Anthropic is the only path that
// surfaces tool_use blocks back through the iteration loop.
func (o *OpenAIProvider) GenerateWithTools(ctx context.Context, messages []core.LlmMessage, _ []Tool) (*Response, error) {
	return o.Generate(ctx, messages)
}

func (o *OpenAIProvider) newRequest(ctx context.Context, payload []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	return req, nil
}

// doWithRetry executes the HTTP request with exponential backoff + jitter on
// 429 (rate limit) and 503 (service unavailable) responses.
func (o *OpenAIProvider) doWithRetry(ctx context.Context, payload []byte) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= openAIMaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(float64(openAIBaseDelay) * math.Pow(2, float64(attempt-1)) * (0.5 + rand.Float64()))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := o.newRequest(ctx, payload)
		if err != nil {
			return nil, fmt.Errorf("openai: build request: %w", err)
		}

		resp, err := o.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("openai: http request: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			resp.Body.Close()
			lastErr = fmt.Errorf("openai: server returned %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("openai: API error %d: %s", resp.StatusCode, string(body))
		}

		return resp, nil
	}

	return nil, fmt.Errorf("openai: retries exhausted: %w", lastErr)
}

// openAIMessage is the wire format for the Chat Completions API.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (o *OpenAIProvider) buildRequestBody(messages []core.LlmMessage) map[string]any {
	apiMsgs := make([]openAIMessage, len(messages))
	for i, m := range messages {
		apiMsgs[i] = openAIMessage{Role: string(m.Role), Content: m.Content}
	}
	return map[string]any{
		"model":      o.model,
		"max_tokens": o.maxTokens,
		"messages":   apiMsgs,
	}
}

// --- JSON (non-streaming) response parsing ---

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

func (o *OpenAIProvider) parseJSONResponse(r io.Reader) (*Response, error) {
	var resp openAIResponse
	// Cap response body — see llmMaxResponseBytes rationale.
	if err := json.NewDecoder(io.LimitReader(r, llmMaxResponseBytes)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	var content string
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}

	result := &Response{Content: content}
	if resp.Usage != nil {
		result.Usage = &TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			Model:        resp.Model,
		}
	}
	return result, nil
}
