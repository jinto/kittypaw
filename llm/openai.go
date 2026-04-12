package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
)

const (
	openAIDefaultBaseURL = "https://api.openai.com/v1/chat/completions"
	openAIDefaultWindow  = 128_000
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

// Generate sends messages and returns a complete response.
func (o *OpenAIProvider) Generate(ctx context.Context, messages []core.LlmMessage) (*Response, error) {
	return o.GenerateStream(ctx, messages, nil)
}

// GenerateStream sends messages and streams tokens via the callback.
func (o *OpenAIProvider) GenerateStream(ctx context.Context, messages []core.LlmMessage, onToken TokenCallback) (*Response, error) {
	streaming := onToken != nil

	body := o.buildRequestBody(messages, streaming)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai: API error %d: %s", resp.StatusCode, string(body))
	}

	if streaming {
		return o.parseSSEStream(resp.Body, onToken)
	}
	return o.parseJSONResponse(resp.Body)
}

// openAIMessage is the wire format for the Chat Completions API.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (o *OpenAIProvider) buildRequestBody(messages []core.LlmMessage, stream bool) map[string]any {
	apiMsgs := make([]openAIMessage, len(messages))
	for i, m := range messages {
		apiMsgs[i] = openAIMessage{Role: string(m.Role), Content: m.Content}
	}

	body := map[string]any{
		"model":      o.model,
		"max_tokens": o.maxTokens,
		"messages":   apiMsgs,
	}
	if stream {
		body["stream"] = true
	}
	return body
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
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
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

// --- SSE streaming response parsing ---
//
// The OpenAI streaming format sends lines like:
//   data: {"choices":[{"delta":{"content":"token"}}]}
//   data: [DONE]

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

func (o *OpenAIProvider) parseSSEStream(r io.Reader, onToken TokenCallback) (*Response, error) {
	scanner := bufio.NewScanner(r)

	var (
		content      strings.Builder
		model        string
		inputTokens  int64
		outputTokens int64
	)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Model != "" {
			model = chunk.Model
		}

		if len(chunk.Choices) > 0 {
			text := chunk.Choices[0].Delta.Content
			if text != "" {
				content.WriteString(text)
				onToken(text)
			}
		}

		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("openai: read SSE stream: %w", err)
	}

	result := &Response{Content: content.String()}
	if inputTokens > 0 || outputTokens > 0 {
		result.Usage = &TokenUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			Model:        model,
		}
	}
	return result, nil
}
