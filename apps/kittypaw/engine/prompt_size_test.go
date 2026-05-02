package engine

import (
	"strings"
	"testing"
)

// TestPromptSize_Breakdown is an ad-hoc measurement test (not load-bearing).
// Run: `go test ./engine -run TestPromptSize_Breakdown -v`
// Used during the prompt-refactor plan to identify which sub-block dominates
// the budget. Delete once the refactor lands.
func TestPromptSize_Breakdown(t *testing.T) {
	type block struct {
		name string
		text string
	}
	blocks := []block{
		{"Identity", IdentityBlock},
		{"Execution", ExecutionBlock},
		{"Quality (Decision+Evidence+Capability+Browse)", QualityBlock},
		{"SkillCreation", SkillCreationBlock},
		{"Memory", MemoryBlock},
	}

	t.Logf("=== Prompt block token breakdown ===")
	total := 0
	for _, b := range blocks {
		tokens := EstimateTokens(b.text)
		chars := len(b.text)
		lines := strings.Count(b.text, "\n") + 1
		total += tokens
		t.Logf("%-50s | %5d tokens | %6d chars | %4d lines", b.name, tokens, chars, lines)
	}
	t.Logf("%-50s | %5d tokens", "TOTAL", total)

	// Within QualityBlock — split on H2 headings to see Decision/Evidence/Capability/Browse.
	t.Logf("=== Quality sub-sections ===")
	parts := strings.Split(QualityBlock, "## ")
	for i, p := range parts {
		if i == 0 && strings.TrimSpace(p) == "" {
			continue
		}
		head := p
		if idx := strings.Index(p, "\n"); idx > 0 {
			head = p[:idx]
		}
		tokens := EstimateTokens(p)
		chars := len(p)
		t.Logf("%-50s | %5d tokens | %6d chars", head, tokens, chars)
	}
}
