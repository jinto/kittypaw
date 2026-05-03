package engine

import (
	"context"
	"math"
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/llm"
	"github.com/jinto/kittypaw/store"
)

type usageMockProvider struct {
	resp *llm.Response
}

func (p *usageMockProvider) Generate(context.Context, []core.LlmMessage) (*llm.Response, error) {
	return p.resp, nil
}

func (p *usageMockProvider) GenerateWithTools(context.Context, []core.LlmMessage, []llm.Tool) (*llm.Response, error) {
	return p.resp, nil
}

func (p *usageMockProvider) ContextWindow() int { return 1000 }

func (p *usageMockProvider) MaxTokens() int { return 100 }

func TestEstimateLLMUsageCostKnownModels(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage *llm.TokenUsage
		want  float64
	}{
		{
			name: "openai gpt-4o-mini",
			usage: &llm.TokenUsage{
				Model:        "gpt-4o-mini",
				InputTokens:  1_000_000,
				OutputTokens: 1_000_000,
			},
			want: 0.75,
		},
		{
			name: "anthropic sonnet cache tokens",
			usage: &llm.TokenUsage{
				Model:                    "claude-3-5-sonnet-20241022",
				InputTokens:              1_000_000,
				OutputTokens:             1_000_000,
				CacheCreationInputTokens: 1_000_000,
				CacheReadInputTokens:     1_000_000,
			},
			want: 22.05,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateLLMUsageCost(tc.usage)
			if !got.Matched {
				t.Fatal("price should be matched")
			}
			if math.Abs(got.EstimatedCostUSD-tc.want) > 0.000001 {
				t.Fatalf("cost = %.6f, want %.6f", got.EstimatedCostUSD, tc.want)
			}
		})
	}
}

func TestUsageRecordingProviderRecordsGenerate(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	provider := NewUsageRecordingProvider(&usageMockProvider{resp: &llm.Response{
		Content: "ok",
		Usage: &llm.TokenUsage{
			Model:        "gpt-4o-mini",
			InputTokens:  1_000,
			OutputTokens: 2_000,
		},
	}}, st, "openai")

	_, err = provider.Generate(WithLLMCallKind(context.Background(), "chat"), []core.LlmMessage{
		{Role: core.RoleUser, Content: "hello"},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	stats, err := st.TodayStats()
	if err != nil {
		t.Fatalf("today stats: %v", err)
	}
	if stats.TotalTokens != 3_000 {
		t.Fatalf("total tokens = %d, want 3000", stats.TotalTokens)
	}
	if math.Abs(stats.EstimatedCostUSD-0.00135) > 0.0000001 {
		t.Fatalf("cost = %.8f, want 0.00135000", stats.EstimatedCostUSD)
	}

	byModel, err := st.TodayLLMUsageByModel()
	if err != nil {
		t.Fatalf("by model: %v", err)
	}
	if len(byModel) != 1 {
		t.Fatalf("models = %d, want 1", len(byModel))
	}
	if byModel[0].Provider != "openai" || byModel[0].Model != "gpt-4o-mini" {
		t.Fatalf("model row = (%q,%q), want (openai,gpt-4o-mini)", byModel[0].Provider, byModel[0].Model)
	}
}
