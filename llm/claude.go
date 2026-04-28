package llm

import (
	"bufio"
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
	claudeBaseURL        = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion     = "2023-06-01"
	claudeDefaultWindow  = 200_000
	claudeFallbackWindow = 8192
	claudeMaxRetries     = 3
	claudeBaseDelay      = 1 * time.Second
)

// ClaudeProvider implements Provider for the Anthropic Messages API.
type ClaudeProvider struct {
	apiKey        string
	model         string
	maxTokens     int
	contextWindow int
	baseURL       string
	client        *http.Client
}

// ClaudeOption is a functional option for NewClaude.
type ClaudeOption func(*ClaudeProvider)

// WithClaudeHTTPClient overrides the default HTTP client.
func WithClaudeHTTPClient(c *http.Client) ClaudeOption {
	return func(p *ClaudeProvider) {
		p.client = c
	}
}

// WithClaudeBaseURL overrides the default Anthropic API endpoint.
func WithClaudeBaseURL(url string) ClaudeOption {
	return func(p *ClaudeProvider) {
		p.baseURL = url
	}
}

// NewClaude creates a ClaudeProvider for the given model.
func NewClaude(apiKey, model string, maxTokens int, opts ...ClaudeOption) *ClaudeProvider {
	window := claudeFallbackWindow
	if isLargeContextModel(model) {
		window = claudeDefaultWindow
	}
	p := &ClaudeProvider{
		apiKey:        apiKey,
		model:         model,
		maxTokens:     maxTokens,
		contextWindow: window,
		baseURL:       claudeBaseURL,
		client:        &http.Client{Timeout: 5 * time.Minute},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ContextWindow returns the model's context window size in tokens.
func (c *ClaudeProvider) ContextWindow() int { return c.contextWindow }

// MaxTokens returns the maximum output tokens.
func (c *ClaudeProvider) MaxTokens() int { return c.maxTokens }

// Generate sends messages and returns a complete response.
func (c *ClaudeProvider) Generate(ctx context.Context, messages []core.LlmMessage) (*Response, error) {
	return c.GenerateStream(ctx, messages, nil)
}

// GenerateWithTools sends messages along with a tool definition list.
// When tools is non-empty the response carries ContentBlocks (text +
// tool_use) and StopReason so a caller can drive a tool-use loop. Falls
// back to plain Generate semantics when tools is nil/empty.
func (c *ClaudeProvider) GenerateWithTools(ctx context.Context, messages []core.LlmMessage, tools []Tool) (*Response, error) {
	if len(tools) == 0 {
		return c.Generate(ctx, messages)
	}
	system, msgs := splitSystemMessages(messages)
	body := c.buildRequestBodyWithTools(system, msgs, false, tools)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("claude: marshal request: %w", err)
	}
	resp, err := c.doWithRetry(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return c.parseJSONResponse(resp.Body)
}

// GenerateStream sends messages and streams tokens via the callback.
func (c *ClaudeProvider) GenerateStream(ctx context.Context, messages []core.LlmMessage, onToken TokenCallback) (*Response, error) {
	system, msgs := splitSystemMessages(messages)
	streaming := onToken != nil

	body := c.buildRequestBody(system, msgs, streaming)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("claude: marshal request: %w", err)
	}

	resp, err := c.doWithRetry(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if streaming {
		return c.parseSSEStream(resp.Body, onToken)
	}
	return c.parseJSONResponse(resp.Body)
}

// splitSystemMessages separates system messages from the conversation.
// The Anthropic API expects system content as a top-level field, not in the
// messages array.
func splitSystemMessages(messages []core.LlmMessage) (string, []core.LlmMessage) {
	var systemParts []string
	var conversation []core.LlmMessage

	for _, m := range messages {
		if m.Role == core.RoleSystem {
			systemParts = append(systemParts, systemTextFrom(m))
		} else {
			conversation = append(conversation, m)
		}
	}
	return strings.Join(systemParts, "\n\n"), conversation
}

// systemTextFrom flattens a system message into a plain string. Callers that
// used the new ContentBlocks shape for a system message (text blocks only) get
// their text concatenated — anything else is dropped, since system role does
// not accept tool_use / tool_result on the wire.
func systemTextFrom(m core.LlmMessage) string {
	if m.Content != "" {
		return m.Content
	}
	var parts []string
	for _, b := range m.ContentBlocks {
		if b.Type == core.BlockTypeText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// claudeMessage is the wire format for a single message in the API request.
//
// Content is typed `any` because Anthropic accepts either a string or a
// content-block array. buildRequestBody picks the shape per LlmMessage.
type claudeMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func (c *ClaudeProvider) buildRequestBody(system string, msgs []core.LlmMessage, stream bool) map[string]any {
	return c.buildRequestBodyWithTools(system, msgs, stream, nil)
}

// buildRequestBodyWithTools is the same as buildRequestBody but emits
// the Anthropic-required `tools` field when tools is non-empty. The
// loop in mediateSkillOutputWithTools relies on the model returning
// stop_reason="tool_use" when it picks a tool — that only happens
// when `tools` is on the wire.
func (c *ClaudeProvider) buildRequestBodyWithTools(system string, msgs []core.LlmMessage, stream bool, tools []Tool) map[string]any {
	apiMsgs := make([]claudeMessage, len(msgs))
	for i, m := range msgs {
		// ContentBlocks wins when present so callers that set both (e.g. a
		// stale Content="" placeholder) still get the structured shape on the
		// wire. This is the only path for tool_use / tool_result blocks.
		if len(m.ContentBlocks) > 0 {
			apiMsgs[i] = claudeMessage{Role: string(m.Role), Content: m.ContentBlocks}
		} else {
			apiMsgs[i] = claudeMessage{Role: string(m.Role), Content: m.Content}
		}
	}

	body := map[string]any{
		"model":      c.model,
		"max_tokens": c.maxTokens,
		"messages":   apiMsgs,
	}
	if system != "" {
		body["system"] = []map[string]any{{
			"type":          "text",
			"text":          system,
			"cache_control": map[string]string{"type": "ephemeral"},
		}}
	}
	if stream {
		body["stream"] = true
	}
	if len(tools) > 0 {
		wireTools := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			schema := t.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			wireTools = append(wireTools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": schema,
			})
		}
		body["tools"] = wireTools
	}
	return body
}

func (c *ClaudeProvider) newRequest(ctx context.Context, payload []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)
	return req, nil
}

// doWithRetry executes the HTTP request with exponential backoff on
// 429 (rate limit) and 529 (overloaded) responses.
func (c *ClaudeProvider) doWithRetry(ctx context.Context, payload []byte) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= claudeMaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(float64(claudeBaseDelay) * math.Pow(2, float64(attempt-1)) * (0.5 + rand.Float64()))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := c.newRequest(ctx, payload)
		if err != nil {
			return nil, fmt.Errorf("claude: build request: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("claude: http request: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			resp.Body.Close()
			lastErr = fmt.Errorf("claude: server returned %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("claude: API error %d: %s", resp.StatusCode, string(body))
		}

		return resp, nil
	}

	return nil, fmt.Errorf("claude: retries exhausted: %w", lastErr)
}

// --- JSON (non-streaming) response parsing ---

// claudeResponse mirrors the shapes Anthropic returns for a single
// completion. Content is heterogeneous — text blocks carry .text,
// tool_use blocks carry id/name/input. We decode into a flat struct
// covering both so the parser can route per Type without a second
// round of JSON.
type claudeResponse struct {
	Content []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text,omitempty"`
		ID    string         `json:"id,omitempty"`
		Name  string         `json:"name,omitempty"`
		Input map[string]any `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

func (c *ClaudeProvider) parseJSONResponse(r io.Reader) (*Response, error) {
	var resp claudeResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("claude: decode response: %w", err)
	}

	var content strings.Builder
	blocks := make([]core.ContentBlock, 0, len(resp.Content))
	for _, b := range resp.Content {
		switch b.Type {
		case core.BlockTypeText, "":
			// "" handles Anthropic responses that elide type for text-only.
			content.WriteString(b.Text)
			blocks = append(blocks, core.ContentBlock{
				Type: core.BlockTypeText,
				Text: b.Text,
			})
		case core.BlockTypeToolUse:
			blocks = append(blocks, core.ContentBlock{
				Type:  core.BlockTypeToolUse,
				ID:    b.ID,
				Name:  b.Name,
				Input: b.Input,
			})
		}
	}

	return &Response{
		Content:       content.String(),
		ContentBlocks: blocks,
		StopReason:    resp.StopReason,
		Usage: &TokenUsage{
			InputTokens:              resp.Usage.InputTokens,
			OutputTokens:             resp.Usage.OutputTokens,
			CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
			Model:                    resp.Model,
		},
	}, nil
}

// --- SSE streaming response parsing ---

// SSE event types we care about:
//   event: content_block_delta   -> extract delta.text
//   event: message_delta         -> extract usage
//   event: message_start         -> extract model + initial usage

type sseError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type sseContentDelta struct {
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
}

type sseMessageStart struct {
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens              int64 `json:"input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type sseMessageDelta struct {
	Usage struct {
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

func (c *ClaudeProvider) parseSSEStream(r io.Reader, onToken TokenCallback) (*Response, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		content                  strings.Builder
		eventType                string
		model                    string
		inputTokens              int64
		outputTokens             int64
		cacheCreationInputTokens int64
		cacheReadInputTokens     int64
	)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		switch eventType {
		case "message_start":
			var msg sseMessageStart
			if err := json.Unmarshal([]byte(data), &msg); err == nil {
				model = msg.Message.Model
				inputTokens = msg.Message.Usage.InputTokens
				cacheCreationInputTokens = msg.Message.Usage.CacheCreationInputTokens
				cacheReadInputTokens = msg.Message.Usage.CacheReadInputTokens
			}

		case "content_block_delta":
			var delta sseContentDelta
			if err := json.Unmarshal([]byte(data), &delta); err == nil {
				text := delta.Delta.Text
				content.WriteString(text)
				if text != "" && onToken != nil {
					onToken(text)
				}
			}

		case "message_delta":
			var md sseMessageDelta
			if err := json.Unmarshal([]byte(data), &md); err == nil {
				outputTokens = md.Usage.OutputTokens
			}

		case "error":
			var se sseError
			if err := json.Unmarshal([]byte(data), &se); err == nil {
				if n := content.Len(); n > 0 {
					return nil, fmt.Errorf("claude: stream error (%s): %s [%d bytes received]", se.Error.Type, se.Error.Message, n)
				}
				return nil, fmt.Errorf("claude: stream error (%s): %s", se.Error.Type, se.Error.Message)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("claude: read SSE stream: %w", err)
	}

	return &Response{
		Content: content.String(),
		Usage: &TokenUsage{
			InputTokens:              inputTokens,
			OutputTokens:             outputTokens,
			CacheCreationInputTokens: cacheCreationInputTokens,
			CacheReadInputTokens:     cacheReadInputTokens,
			Model:                    model,
		},
	}, nil
}

// isLargeContextModel returns true for Claude models with a 200k context window.
func isLargeContextModel(model string) bool {
	return strings.Contains(model, "claude")
}
