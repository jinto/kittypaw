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
	"strings"
	"time"

	"github.com/jinto/kittypaw/core"
)

const (
	openAIChatCompletionsURL = "https://api.openai.com/v1/chat/completions"
	openAIResponsesURL       = "https://api.openai.com/v1/responses"
	openAIDefaultWindow      = 128_000
	openAIMaxRetries         = 3
	openAIBaseDelay          = 1 * time.Second
)

type openAIAPIMode string

const (
	openAIAPIModeChat      openAIAPIMode = "chat"
	openAIAPIModeResponses openAIAPIMode = "responses"
)

// OpenAIProvider implements Provider for OpenAI's Responses API by default.
// It also supports OpenAI-compatible Chat Completions endpoints (OpenRouter,
// Ollama, LM Studio) via a configurable base URL.
type OpenAIProvider struct {
	apiKey        string
	model         string
	maxTokens     int
	contextWindow int
	baseURL       string
	apiMode       openAIAPIMode
	client        *http.Client
}

// OpenAIOption is a functional option for NewOpenAI.
type OpenAIOption func(*OpenAIProvider)

// WithBaseURL overrides the default OpenAI API endpoint.
// Custom endpoints are treated as Chat Completions-compatible.
func WithBaseURL(url string) OpenAIOption {
	return func(p *OpenAIProvider) {
		p.baseURL = url
		p.apiMode = openAIAPIModeChat
	}
}

// WithResponsesBaseURL overrides the default OpenAI Responses endpoint.
func WithResponsesBaseURL(url string) OpenAIOption {
	return func(p *OpenAIProvider) {
		p.baseURL = url
		p.apiMode = openAIAPIModeResponses
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
		baseURL:       openAIResponsesURL,
		apiMode:       openAIAPIModeResponses,
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
	if o.apiMode == openAIAPIModeResponses {
		return o.buildResponsesRequestBody(messages)
	}
	return o.buildChatRequestBody(messages)
}

func (o *OpenAIProvider) buildChatRequestBody(messages []core.LlmMessage) map[string]any {
	apiMsgs := make([]openAIMessage, len(messages))
	for i, m := range messages {
		apiMsgs[i] = openAIMessage{Role: string(m.Role), Content: textFromMessage(m)}
	}
	return map[string]any{
		"model":      o.model,
		"max_tokens": o.maxTokens,
		"messages":   apiMsgs,
	}
}

type openAIResponsesInput struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (o *OpenAIProvider) buildResponsesRequestBody(messages []core.LlmMessage) map[string]any {
	instructions, conversation := splitSystemMessages(messages)
	input := make([]openAIResponsesInput, 0, len(conversation))
	for _, m := range conversation {
		content := textFromMessage(m)
		if content == "" {
			continue
		}
		input = append(input, openAIResponsesInput{Role: string(m.Role), Content: content})
	}
	body := map[string]any{
		"model":             o.model,
		"max_output_tokens": o.maxTokens,
		"input":             input,
	}
	if instructions != "" {
		body["instructions"] = instructions
	}
	return body
}

func textFromMessage(m core.LlmMessage) string {
	if m.Content != "" {
		return m.Content
	}
	var parts []string
	for _, b := range m.ContentBlocks {
		switch b.Type {
		case core.BlockTypeText:
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case core.BlockTypeToolResult:
			if b.Content != "" {
				parts = append(parts, b.Content)
			}
		}
	}
	return strings.Join(parts, "\n\n")
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

type openAIResponsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage *struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

func (o *OpenAIProvider) parseJSONResponse(r io.Reader) (*Response, error) {
	if o.apiMode == openAIAPIModeResponses {
		return o.parseResponsesJSONResponse(r)
	}
	return o.parseChatJSONResponse(r)
}

func (o *OpenAIProvider) parseChatJSONResponse(r io.Reader) (*Response, error) {
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

func (o *OpenAIProvider) parseResponsesJSONResponse(r io.Reader) (*Response, error) {
	var resp openAIResponsesResponse
	if err := json.NewDecoder(io.LimitReader(r, llmMaxResponseBytes)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	content := resp.OutputText
	if content == "" {
		var parts []string
		for _, item := range resp.Output {
			for _, part := range item.Content {
				if part.Text != "" {
					parts = append(parts, part.Text)
				}
			}
		}
		content = strings.Join(parts, "")
	}

	result := &Response{Content: content}
	if resp.Usage != nil {
		result.Usage = &TokenUsage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			Model:        resp.Model,
		}
	}
	return result, nil
}
