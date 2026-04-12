package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
)

const (
	claudeBaseURL       = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion    = "2023-06-01"
	claudeDefaultWindow = 200_000
	claudeFallbackWindow = 8192
	claudeMaxRetries    = 3
	claudeBaseDelay     = 1 * time.Second
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
			systemParts = append(systemParts, m.Content)
		} else {
			conversation = append(conversation, m)
		}
	}
	return strings.Join(systemParts, "\n\n"), conversation
}

// claudeMessage is the wire format for a single message in the API request.
type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *ClaudeProvider) buildRequestBody(system string, msgs []core.LlmMessage, stream bool) map[string]any {
	apiMsgs := make([]claudeMessage, len(msgs))
	for i, m := range msgs {
		apiMsgs[i] = claudeMessage{Role: string(m.Role), Content: m.Content}
	}

	body := map[string]any{
		"model":      c.model,
		"max_tokens": c.maxTokens,
		"messages":   apiMsgs,
	}
	if system != "" {
		body["system"] = system
	}
	if stream {
		body["stream"] = true
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
			delay := time.Duration(float64(claudeBaseDelay) * math.Pow(2, float64(attempt-1)))
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

type claudeResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

func (c *ClaudeProvider) parseJSONResponse(r io.Reader) (*Response, error) {
	var resp claudeResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("claude: decode response: %w", err)
	}

	var content strings.Builder
	for _, block := range resp.Content {
		content.WriteString(block.Text)
	}

	return &Response{
		Content: content.String(),
		Usage: &TokenUsage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			Model:        resp.Model,
		},
	}, nil
}

// --- SSE streaming response parsing ---

// SSE event types we care about:
//   event: content_block_delta   -> extract delta.text
//   event: message_delta         -> extract usage
//   event: message_start         -> extract model + initial usage

type sseContentDelta struct {
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
}

type sseMessageStart struct {
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens int64 `json:"input_tokens"`
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

	var (
		content      strings.Builder
		eventType    string
		model        string
		inputTokens  int64
		outputTokens int64
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
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("claude: read SSE stream: %w", err)
	}

	return &Response{
		Content: content.String(),
		Usage: &TokenUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			Model:        model,
		},
	}, nil
}

// isLargeContextModel returns true for Claude models with a 200k context window.
func isLargeContextModel(model string) bool {
	return strings.Contains(model, "claude")
}
