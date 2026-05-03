package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/llm"
	"github.com/jinto/kittypaw/store"
)

const llmPricingSource = "builtin:2026-05-03"

type llmCallKindContextKey struct{}

// WithLLMCallKind annotates ctx so UsageRecordingProvider can classify a call.
func WithLLMCallKind(ctx context.Context, kind string) context.Context {
	if kind == "" {
		return ctx
	}
	return context.WithValue(ctx, llmCallKindContextKey{}, kind)
}

func llmCallKind(ctx context.Context) string {
	if ctx == nil {
		return "llm"
	}
	if kind, ok := ctx.Value(llmCallKindContextKey{}).(string); ok && kind != "" {
		return kind
	}
	return "llm"
}

// LLMUsageCostEstimate is the local cost estimate for one usage payload.
type LLMUsageCostEstimate struct {
	EstimatedCostUSD float64
	PricingSource    string
	Matched          bool
}

type llmTokenPrice struct {
	InputPerMTok      float64
	OutputPerMTok     float64
	CacheWritePerMTok float64
	CacheReadPerMTok  float64
}

// EstimateLLMUsageCost estimates USD cost from known per-1M-token prices.
// Unknown models still get token accounting; their estimate remains unmatched.
func EstimateLLMUsageCost(usage *llm.TokenUsage) LLMUsageCostEstimate {
	if usage == nil {
		return LLMUsageCostEstimate{PricingSource: llmPricingSource}
	}
	price, ok := priceForLLMUsage(usage)
	if !ok {
		return LLMUsageCostEstimate{PricingSource: llmPricingSource}
	}
	cost := perMTokCost(usage.InputTokens, price.InputPerMTok) +
		perMTokCost(usage.OutputTokens, price.OutputPerMTok) +
		perMTokCost(usage.CacheCreationInputTokens, price.CacheWritePerMTok) +
		perMTokCost(usage.CacheReadInputTokens, price.CacheReadPerMTok)
	return LLMUsageCostEstimate{
		EstimatedCostUSD: cost,
		PricingSource:    llmPricingSource,
		Matched:          true,
	}
}

func perMTokCost(tokens int64, rate float64) float64 {
	if tokens <= 0 || rate <= 0 {
		return 0
	}
	return float64(tokens) * rate / 1_000_000
}

func priceForLLMUsage(usage *llm.TokenUsage) (llmTokenPrice, bool) {
	model := strings.ToLower(usage.Model)
	model = strings.TrimSpace(model)
	model = strings.TrimPrefix(model, "models/")
	if model == "" {
		return llmTokenPrice{}, false
	}

	switch {
	case strings.Contains(model, "gpt-5.5-pro"):
		return llmTokenPrice{InputPerMTok: 30, OutputPerMTok: 180}, true
	case strings.Contains(model, "gpt-5.5"):
		return llmTokenPrice{InputPerMTok: 5, OutputPerMTok: 30, CacheWritePerMTok: 5, CacheReadPerMTok: 0.50}, true
	case strings.Contains(model, "gpt-5.4-pro"):
		return llmTokenPrice{InputPerMTok: 30, OutputPerMTok: 180}, true
	case strings.Contains(model, "gpt-5.4-mini"):
		return llmTokenPrice{InputPerMTok: 0.75, OutputPerMTok: 4.50, CacheWritePerMTok: 0.75, CacheReadPerMTok: 0.075}, true
	case strings.Contains(model, "gpt-5.4-nano"):
		return llmTokenPrice{InputPerMTok: 0.20, OutputPerMTok: 1.25, CacheWritePerMTok: 0.20, CacheReadPerMTok: 0.02}, true
	case strings.Contains(model, "gpt-5.4"):
		return llmTokenPrice{InputPerMTok: 2.50, OutputPerMTok: 15, CacheWritePerMTok: 2.50, CacheReadPerMTok: 0.25}, true
	case strings.Contains(model, "gpt-5.3-codex") || strings.Contains(model, "gpt-5.3-chat"):
		return llmTokenPrice{InputPerMTok: 1.75, OutputPerMTok: 14, CacheWritePerMTok: 1.75, CacheReadPerMTok: 0.175}, true
	case strings.Contains(model, "gpt-4o-mini"):
		return llmTokenPrice{InputPerMTok: 0.15, OutputPerMTok: 0.60, CacheWritePerMTok: 0.15, CacheReadPerMTok: 0.075}, true
	case strings.Contains(model, "gpt-4o"):
		return llmTokenPrice{InputPerMTok: 2.50, OutputPerMTok: 10, CacheWritePerMTok: 2.50, CacheReadPerMTok: 1.25}, true
	case strings.Contains(model, "gpt-4.1-mini"):
		return llmTokenPrice{InputPerMTok: 0.40, OutputPerMTok: 1.60, CacheWritePerMTok: 0.40, CacheReadPerMTok: 0.10}, true
	case strings.Contains(model, "gpt-4.1-nano"):
		return llmTokenPrice{InputPerMTok: 0.10, OutputPerMTok: 0.40, CacheWritePerMTok: 0.10, CacheReadPerMTok: 0.025}, true
	case strings.Contains(model, "gpt-4.1"):
		return llmTokenPrice{InputPerMTok: 2, OutputPerMTok: 8, CacheWritePerMTok: 2, CacheReadPerMTok: 0.50}, true
	case strings.Contains(model, "opus-4-7") || strings.Contains(model, "opus-4.7") ||
		strings.Contains(model, "4-7-opus") || strings.Contains(model, "4.7-opus") ||
		strings.Contains(model, "opus-4-6") || strings.Contains(model, "opus-4.6") ||
		strings.Contains(model, "4-6-opus") || strings.Contains(model, "4.6-opus") ||
		strings.Contains(model, "opus-4-5") || strings.Contains(model, "opus-4.5") ||
		strings.Contains(model, "4-5-opus") || strings.Contains(model, "4.5-opus"):
		return llmTokenPrice{InputPerMTok: 5, OutputPerMTok: 25, CacheWritePerMTok: 6.25, CacheReadPerMTok: 0.50}, true
	case strings.Contains(model, "opus"):
		return llmTokenPrice{InputPerMTok: 15, OutputPerMTok: 75, CacheWritePerMTok: 18.75, CacheReadPerMTok: 1.50}, true
	case strings.Contains(model, "haiku-4-5") || strings.Contains(model, "haiku-4.5") ||
		strings.Contains(model, "4-5-haiku") || strings.Contains(model, "4.5-haiku"):
		return llmTokenPrice{InputPerMTok: 1, OutputPerMTok: 5, CacheWritePerMTok: 1.25, CacheReadPerMTok: 0.10}, true
	case strings.Contains(model, "haiku-3-5") || strings.Contains(model, "haiku-3.5") ||
		strings.Contains(model, "3-5-haiku") || strings.Contains(model, "3.5-haiku"):
		return llmTokenPrice{InputPerMTok: 0.80, OutputPerMTok: 4, CacheWritePerMTok: 1, CacheReadPerMTok: 0.08}, true
	case strings.Contains(model, "haiku"):
		return llmTokenPrice{InputPerMTok: 0.25, OutputPerMTok: 1.25, CacheWritePerMTok: 0.30, CacheReadPerMTok: 0.03}, true
	case strings.Contains(model, "sonnet"):
		return llmTokenPrice{InputPerMTok: 3, OutputPerMTok: 15, CacheWritePerMTok: 3.75, CacheReadPerMTok: 0.30}, true
	case strings.Contains(model, "gemini-3.1-pro"):
		return geminiProPrice(usage, 2, 12, 4, 18, 0.20, 0.40), true
	case strings.Contains(model, "gemini-3.1-flash-lite"):
		return llmTokenPrice{InputPerMTok: 0.25, OutputPerMTok: 1.50, CacheWritePerMTok: 0.25, CacheReadPerMTok: 0.025}, true
	case strings.Contains(model, "gemini-3-flash"):
		return llmTokenPrice{InputPerMTok: 0.50, OutputPerMTok: 3, CacheWritePerMTok: 0.50, CacheReadPerMTok: 0.05}, true
	case strings.Contains(model, "gemini-2.5-pro"):
		return geminiProPrice(usage, 1.25, 10, 2.50, 15, 0.125, 0.25), true
	case strings.Contains(model, "gemini-2.5-flash-lite"):
		return llmTokenPrice{InputPerMTok: 0.10, OutputPerMTok: 0.40, CacheWritePerMTok: 0.10, CacheReadPerMTok: 0.01}, true
	case strings.Contains(model, "gemini-2.5-flash"):
		return llmTokenPrice{InputPerMTok: 0.30, OutputPerMTok: 2.50, CacheWritePerMTok: 0.30, CacheReadPerMTok: 0.03}, true
	case strings.Contains(model, "gemini-2.0-flash-lite"):
		return llmTokenPrice{InputPerMTok: 0.075, OutputPerMTok: 0.30}, true
	case strings.Contains(model, "gemini-2.0-flash"):
		return llmTokenPrice{InputPerMTok: 0.10, OutputPerMTok: 0.40, CacheWritePerMTok: 0.10, CacheReadPerMTok: 0.025}, true
	case strings.Contains(model, "gemini-1.5-pro"):
		return geminiProPrice(usage, 1.25, 5, 2.50, 10, 0.3125, 0.625), true
	case strings.Contains(model, "gemini-1.5-flash-8b"):
		return geminiProPrice(usage, 0.0375, 0.15, 0.075, 0.30, 0.01, 0.02), true
	case strings.Contains(model, "gemini-1.5-flash"):
		return geminiProPrice(usage, 0.075, 0.30, 0.15, 0.60, 0.01875, 0.0375), true
	default:
		return llmTokenPrice{}, false
	}
}

func geminiProPrice(usage *llm.TokenUsage, input, output, longInput, longOutput, cacheRead, longCacheRead float64) llmTokenPrice {
	if usage.InputTokens > 200_000 {
		return llmTokenPrice{
			InputPerMTok:      longInput,
			OutputPerMTok:     longOutput,
			CacheWritePerMTok: longInput,
			CacheReadPerMTok:  longCacheRead,
		}
	}
	return llmTokenPrice{
		InputPerMTok:      input,
		OutputPerMTok:     output,
		CacheWritePerMTok: input,
		CacheReadPerMTok:  cacheRead,
	}
}

type usageRecordingProvider struct {
	inner    llm.Provider
	store    *store.Store
	provider string
}

// NewUsageRecordingProvider wraps provider calls and persists successful usage.
func NewUsageRecordingProvider(inner llm.Provider, st *store.Store, provider string) llm.Provider {
	if inner == nil || st == nil {
		return inner
	}
	if _, ok := inner.(*usageRecordingProvider); ok {
		return inner
	}
	return &usageRecordingProvider{inner: inner, store: st, provider: provider}
}

func (p *usageRecordingProvider) Generate(ctx context.Context, messages []core.LlmMessage) (*llm.Response, error) {
	start := time.Now()
	resp, err := p.inner.Generate(ctx, messages)
	p.record(ctx, start, resp, err)
	return resp, err
}

func (p *usageRecordingProvider) GenerateWithTools(ctx context.Context, messages []core.LlmMessage, tools []llm.Tool) (*llm.Response, error) {
	start := time.Now()
	resp, err := p.inner.GenerateWithTools(ctx, messages, tools)
	p.record(ctx, start, resp, err)
	return resp, err
}

func (p *usageRecordingProvider) ContextWindow() int { return p.inner.ContextWindow() }

func (p *usageRecordingProvider) MaxTokens() int { return p.inner.MaxTokens() }

func (p *usageRecordingProvider) record(ctx context.Context, start time.Time, resp *llm.Response, callErr error) {
	if callErr != nil || resp == nil || resp.Usage == nil || p.store == nil {
		return
	}
	finished := time.Now()
	estimate := EstimateLLMUsageCost(resp.Usage)
	usageJSON := ""
	if data, err := json.Marshal(resp.Usage); err == nil {
		usageJSON = string(data)
	}
	if err := p.store.RecordLLMCallUsage(&store.LLMCallUsageRecord{
		CallKind:                 llmCallKind(ctx),
		Provider:                 p.provider,
		Model:                    resp.Usage.Model,
		StartedAt:                start.UTC().Format("2006-01-02T15:04:05Z"),
		FinishedAt:               finished.UTC().Format("2006-01-02T15:04:05Z"),
		DurationMs:               finished.Sub(start).Milliseconds(),
		InputTokens:              resp.Usage.InputTokens,
		OutputTokens:             resp.Usage.OutputTokens,
		CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
		EstimatedCost:            estimate.EstimatedCostUSD,
		PricingSource:            estimate.PricingSource,
		PricingMatched:           estimate.Matched,
		UsageJSON:                usageJSON,
	}); err != nil {
		slog.Warn("failed to record llm usage", "call_kind", llmCallKind(ctx), "provider", p.provider, "error", err)
	}
}
