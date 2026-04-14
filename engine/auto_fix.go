package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jinto/gopaw/core"
)

const maxFixAttempts = 2

// AttemptAutoFix generates a code fix for a failing skill using the LLM.
// It calls generateCode directly (not HandleTeach) to preserve the original
// skill's name, trigger, and permissions.
//
// Returns the TeachResult if fix generation succeeds, or nil if the budget
// is exhausted. Does not increment fix_attempts on LLM failure — only the
// caller (schedule.go) does that on success.
func AttemptAutoFix(
	ctx context.Context,
	skillName, errorMsg string,
	s *Session,
	budget *SharedTokenBudget,
) (*TeachResult, error) {
	// Load current skill metadata and code from disk.
	skill, currentCode, err := core.LoadSkillFrom(s.BaseDir, skillName)
	if err != nil {
		return nil, fmt.Errorf("load skill %q: %w", skillName, err)
	}
	if skill == nil {
		return nil, fmt.Errorf("skill %q not found on disk", skillName)
	}

	// Build fix prompt with error context + current code.
	fixPrompt := buildFixPrompt(errorMsg, currentCode)

	// Generate fix code via LLM.
	raw, err := generateCode(ctx, fixPrompt, "scheduler", s.Provider)
	if err != nil {
		return nil, fmt.Errorf("LLM fix generation failed: %w", err)
	}

	// Budget check after generation.
	if budget != nil && !budget.TrySpend(estimateFixTokens(currentCode, raw)) {
		slog.Warn("auto-fix: budget exhausted, discarding fix", "skill", skillName)
		return nil, fmt.Errorf("token budget exhausted")
	}

	// Strip fences and syntax check.
	code := stripFences(raw)
	ok, syntaxErr := SyntaxCheck(ctx, code, nil)

	// Construct TeachResult preserving original skill metadata.
	result := &TeachResult{
		SkillName:   skill.Name,
		Code:        code,
		SyntaxOK:    ok,
		SyntaxError: syntaxErr,
		Description: skill.Description,
		Trigger:     skill.Trigger,
		Permissions: skill.Permissions.Primitives,
	}

	return result, nil
}

// ApplyAutoFix applies a generated fix based on autonomy level.
// Full autonomy: saves to disk immediately and records as applied.
// Supervised: records in DB only for user review.
func ApplyAutoFix(
	s *Session,
	skillName string,
	result *TeachResult,
	errorMsg, oldCode string,
) error {
	if !result.SyntaxOK {
		slog.Warn("auto-fix: syntax error in generated fix, discarding",
			"skill", skillName, "error", result.SyntaxError)
		return fmt.Errorf("generated fix has syntax error: %s", result.SyntaxError)
	}

	switch s.Config.AutonomyLevel {
	case core.AutonomyFull:
		// Apply immediately: save to disk.
		if err := ApproveSkill(s.BaseDir, result); err != nil {
			return fmt.Errorf("approve fix: %w", err)
		}
		// Record as already applied.
		if err := s.Store.RecordFix(skillName, errorMsg, oldCode, result.Code, true); err != nil {
			slog.Warn("auto-fix: failed to record applied fix", "skill", skillName, "error", err)
		}
		_ = s.Store.ResetFailureCount(skillName)
		slog.Info("auto-fix: applied fix", "skill", skillName, "autonomy", "full")

	case core.AutonomySupervised:
		// Store for user review only.
		if err := s.Store.RecordFix(skillName, errorMsg, oldCode, result.Code, false); err != nil {
			return fmt.Errorf("record fix: %w", err)
		}
		slog.Info("auto-fix: fix stored for review", "skill", skillName, "autonomy", "supervised")

	default:
		return fmt.Errorf("auto-fix not available in %s mode", s.Config.AutonomyLevel)
	}

	return nil
}

// buildFixPrompt creates the LLM prompt for generating a code fix.
func buildFixPrompt(errorMsg, currentCode string) string {
	return fmt.Sprintf(`The following JavaScript skill code failed with this error:

Error: %s

Current code:
%s

Fix the code to resolve the error. Output ONLY the corrected JavaScript code.
Do NOT change the overall logic or add new features — only fix the error.
Follow the same style and conventions as the original code.`, errorMsg, currentCode)
}

// estimateFixTokens provides a rough token estimate for budget accounting
// when the LLM response doesn't include usage data. Uses ~4 chars per token.
func estimateFixTokens(input, output string) uint64 {
	return uint64((len(input) + len(output)) / 4)
}
