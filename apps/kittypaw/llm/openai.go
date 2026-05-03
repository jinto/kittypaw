package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
// It also supports OpenAI-compatible Chat Completions endpoints (Cerebras,
// Groq, DeepSeek, OpenRouter, Ollama, LM Studio) via a configurable base URL.
// Chat-mode endpoints unlock function calling: Anthropic-style ContentBlocks
// (tool_use / tool_result) round-trip through OpenAI's tool_calls + role:"tool".
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

// Generate sends messages and returns a complete response. Wire is plain
// JSON (`stream: false`) — see Provider docs for why streaming was removed
// in Phase 13.3.
func (o *OpenAIProvider) Generate(ctx context.Context, messages []core.LlmMessage) (*Response, error) {
	return o.generate(ctx, messages, nil)
}

// GenerateWithTools sends messages along with a tool definition list. When
// tools is non-empty AND the provider is in Chat Completions mode, the
// response carries ContentBlocks (text + tool_use) and StopReason so the
// caller can drive a tool-use loop.
//
// Falls back to Generate semantics when tools is nil/empty, or when the
// provider is in Responses mode (Phase 1 supports Chat-mode tool calling
// only — Cerebras / Groq / DeepSeek / OpenRouter / Ollama all run via the
// Chat Completions wire).
func (o *OpenAIProvider) GenerateWithTools(ctx context.Context, messages []core.LlmMessage, tools []Tool) (*Response, error) {
	if len(tools) == 0 || o.apiMode != openAIAPIModeChat {
		return o.Generate(ctx, messages)
	}
	return o.generate(ctx, messages, tools)
}

func (o *OpenAIProvider) generate(ctx context.Context, messages []core.LlmMessage, tools []Tool) (*Response, error) {
	body := o.buildRequestBodyWithTools(messages, tools)
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

// --- Request body builders ---

// openAIMessage is the wire format for one Chat Completions message.
//
// Content is `any` because the API accepts string, null, or absent depending
// on the role (assistant with tool_calls may have content==null). ToolCalls
// is the assistant's tool_use shape; ToolCallID points the role:"tool"
// reply at its originating tool_call.
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (o *OpenAIProvider) buildRequestBody(messages []core.LlmMessage) map[string]any {
	return o.buildRequestBodyWithTools(messages, nil)
}

func (o *OpenAIProvider) buildRequestBodyWithTools(messages []core.LlmMessage, tools []Tool) map[string]any {
	if o.apiMode == openAIAPIModeResponses {
		// Responses API is not wired for tool calling in this phase. Cerebras /
		// Groq / Ollama all use Chat Completions, which is the path that
		// surfaces tool_calls to the caller.
		return o.buildResponsesRequestBody(messages)
	}
	return o.buildChatRequestBodyWithTools(messages, tools)
}

// buildChatRequestBodyWithTools assembles the Chat Completions wire body and
// emits an OpenAI `tools` array when tools is non-empty.
func (o *OpenAIProvider) buildChatRequestBodyWithTools(messages []core.LlmMessage, tools []Tool) map[string]any {
	body := map[string]any{
		"model":      o.model,
		"max_tokens": o.maxTokens,
		"messages":   convertMessagesToOpenAIChat(messages),
	}
	if len(tools) > 0 {
		body["tools"] = convertToolsToOpenAI(tools)
	}
	return body
}

// convertToolsToOpenAI maps Anthropic-style Tool definitions to OpenAI's
// `{type:"function", function:{name, description, parameters}}` shape. A nil
// InputSchema is normalized to an empty-object schema — Anthropic accepts
// nil but OpenAI requires a parameters object.
func convertToolsToOpenAI(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  schema,
			},
		})
	}
	return out
}

// convertMessagesToOpenAIChat converts internal LlmMessage values into the
// Chat Completions message array. Tool-related ContentBlocks are unfolded:
//
//   - assistant + tool_use blocks → assistant message + `tool_calls` array
//     (text blocks alongside become the assistant's `content` string)
//   - user + tool_result blocks   → role:"tool" message per tool_result,
//     order preserved (AC-12)
//   - mixed text + tool_result on the same user message → text becomes a
//     standalone user message and the tool_results split out (with a
//     slog.Warn — caller contract violation signal: tool_results normally
//     arrive on their own user turn).
func convertMessagesToOpenAIChat(messages []core.LlmMessage) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages))
	for _, m := range messages {
		if len(m.ContentBlocks) == 0 {
			out = append(out, openAIMessage{Role: string(m.Role), Content: m.Content})
			continue
		}

		var (
			textParts   []string
			toolUses    []openAIToolCall
			toolResults []core.ContentBlock
		)
		for _, b := range m.ContentBlocks {
			switch b.Type {
			case core.BlockTypeText:
				if b.Text != "" {
					textParts = append(textParts, b.Text)
				}
			case core.BlockTypeToolUse:
				// nil Input → "{}" (mirrors Anthropic ContentBlock.MarshalJSON
				// which always emits an input field). A genuine Marshal error
				// means the caller put an unmarshalable value (cyclic ref,
				// chan/func) into Input — caller-code bug, not a runtime
				// payload issue. Fail loud rather than silently rewriting
				// Arguments to "{}", which would let the model observe
				// Arguments different from what it sent in the prior turn and
				// drive incoherent reasoning or a tool-loop.
				var args []byte
				if b.Input == nil {
					args = []byte("{}")
				} else {
					var err error
					args, err = json.Marshal(b.Input)
					if err != nil {
						panic(fmt.Sprintf("openai: tool_use Input marshal failed for id=%q name=%q: %v", b.ID, b.Name, err))
					}
				}
				toolUses = append(toolUses, openAIToolCall{
					ID:   b.ID,
					Type: "function",
					Function: openAIToolCallFunction{
						Name:      b.Name,
						Arguments: string(args),
					},
				})
			case core.BlockTypeToolResult:
				toolResults = append(toolResults, b)
			}
		}

		if len(toolResults) > 0 && (len(textParts) > 0 || len(toolUses) > 0) {
			// Counts only — never log raw Text / Input / tr.Content from this
			// site. Those fields can carry user prompts, tool arguments
			// (potentially API tokens / passwords), or tool output and must
			// not leak into structured logs.
			slog.Warn("openai: mixed text+tool_result message; emitting in order",
				"role", string(m.Role),
				"text_parts", len(textParts),
				"tool_uses", len(toolUses),
				"tool_results", len(toolResults))
		}

		// Emit text + tool_use under the original role.
		if len(textParts) > 0 || len(toolUses) > 0 {
			msg := openAIMessage{Role: string(m.Role)}
			switch {
			case len(textParts) > 0:
				msg.Content = strings.Join(textParts, "\n\n")
			case len(toolUses) > 0:
				// Empty string keeps the field present without committing to null
				// — every observed compatible endpoint accepts "".
				msg.Content = ""
			}
			if len(toolUses) > 0 {
				msg.ToolCalls = toolUses
			}
			out = append(out, msg)
		}

		// Each tool_result becomes its own role:"tool" message, preserving the
		// order of the source ContentBlocks (AC-12).
		for _, tr := range toolResults {
			out = append(out, openAIMessage{
				Role:       "tool",
				ToolCallID: tr.ToolUseID,
				Content:    tr.Content,
			})
		}
	}
	return out
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

type openAIChoiceMessage struct {
	Content   string                   `json:"content"`
	ToolCalls []openAIResponseToolCall `json:"tool_calls,omitempty"`
}

type openAIResponseToolCall struct {
	ID       string                         `json:"id"`
	Type     string                         `json:"type"`
	Function openAIResponseToolCallFunction `json:"function"`
}

type openAIResponseToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type openAIResponse struct {
	Choices []struct {
		Message      openAIChoiceMessage `json:"message"`
		FinishReason string              `json:"finish_reason"`
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

// mapStopReason translates OpenAI Chat finish_reason into the Anthropic-
// flavored stop-reason vocabulary the agent loop already recognizes.
// Unknown values pass through verbatim — forward-compat: a new finish_reason
// reaches the caller as raw evidence, not as a guessed translation. Caller
// contract: anything outside {"end_turn", "tool_use", "max_tokens"} should
// be treated as a conservative termination.
func mapStopReason(finish string) string {
	switch finish {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return finish
	}
}

// decodeArguments handles the two on-the-wire shapes `arguments` arrives in:
//
//   - JSON string ("{...}") — OpenAI standard
//   - JSON object ({...})   — emitted by some Ollama models (qwen2.5:7b,
//     llama3.1) despite the docs
//
// An empty string is normalized to an empty map. A genuine parse failure
// surfaces as an error so the caller never silently feeds a malformed Input
// into a tool executor.
func decodeArguments(raw json.RawMessage) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return map[string]any{}, nil
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, fmt.Errorf("openai: arguments string decode: %w", err)
		}
		if s == "" {
			return map[string]any{}, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("openai: arguments JSON parse: %w", err)
		}
		if out == nil {
			return map[string]any{}, nil
		}
		return out, nil
	case '{':
		var out map[string]any
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return nil, fmt.Errorf("openai: arguments object decode: %w", err)
		}
		if out == nil {
			return map[string]any{}, nil
		}
		return out, nil
	default:
		return nil, fmt.Errorf("openai: unrecognized arguments shape: %s", string(trimmed))
	}
}

func (o *OpenAIProvider) parseChatJSONResponse(r io.Reader) (*Response, error) {
	var resp openAIResponse
	if err := json.NewDecoder(io.LimitReader(r, llmMaxResponseBytes)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	var (
		contentText  string
		blocks       []core.ContentBlock
		finishReason string
	)
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		contentText = choice.Message.Content
		if contentText != "" {
			blocks = append(blocks, core.ContentBlock{
				Type: core.BlockTypeText,
				Text: contentText,
			})
		}
		for _, tc := range choice.Message.ToolCalls {
			// An empty tool_call.id is a wire violation — the caller has to
			// echo it on the next turn as tool_call_id, and OpenAI then 400s
			// ("messages with role 'tool' must be a response to a preceding
			// message with 'tool_calls'"). Surface immediately so the caller
			// sees the malformed response instead of looping with empty IDs.
			// (Some Ollama models drop the id field on single-tool calls.)
			if tc.ID == "" {
				return nil, fmt.Errorf("openai: tool_call missing id (function=%q)", tc.Function.Name)
			}
			input, err := decodeArguments(tc.Function.Arguments)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, core.ContentBlock{
				Type:  core.BlockTypeToolUse,
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
		finishReason = choice.FinishReason
	}

	result := &Response{
		Content:       contentText,
		ContentBlocks: blocks,
		StopReason:    mapStopReason(finishReason),
	}
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
