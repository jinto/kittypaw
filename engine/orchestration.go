package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/llm"
	"github.com/jinto/gopaw/store"
	"golang.org/x/sync/errgroup"
)

// maxDelegateTaskLen caps task description size to prevent prompt explosion.
const maxDelegateTaskLen = 4096

// backgroundTokenCap is the hard cap for background delegate tasks.
const backgroundTokenCap = 2048

// ---------------------------------------------------------------------------
// PM decision types (JSON format)
// ---------------------------------------------------------------------------

// PMDecision is the JSON response from the PM agent.
type PMDecision struct {
	Kind   string       `json:"kind"`   // "direct" or "delegate"
	Reason string       `json:"reason"` // why this routing was chosen
	Tasks  []PMTaskSpec `json:"tasks"`  // non-empty when kind=="delegate"
}

// PMTaskSpec describes a single delegation target.
type PMTaskSpec struct {
	ProfileID  string `json:"profile_id"`
	Task       string `json:"task"`
	Background bool   `json:"background,omitempty"`
}

// DelegateCtx holds context for agent delegation within the skill executor.
type DelegateCtx struct {
	Provider llm.Provider
	Store    *store.Store
	Config   *core.Config
	Budget   *SharedTokenBudget
	Depth    int
	MaxDepth int
}

// DelegateResult holds the outcome of a single delegation.
type DelegateResult struct {
	ProfileID  string `json:"profile_id"`
	Task       string `json:"task"`
	Result     string `json:"result"`
	Success    bool   `json:"success"`
	TokenUsage int64  `json:"token_usage"`
}

// ---------------------------------------------------------------------------
// OrchestrateRequest
// ---------------------------------------------------------------------------

// OrchestrateRequest routes a user message through the PM (Project Manager)
// agent which decides whether to handle directly or delegate to profiles.
// Returns (response, handled, error). When handled is false, the caller
// should fall through to the default agent loop.
func OrchestrateRequest(
	ctx context.Context,
	text string,
	provider llm.Provider,
	st *store.Store,
	config *core.OrchestrationConfig,
	budget *SharedTokenBudget,
	baseDir string,
) (string, bool, error) {
	if !config.Enabled {
		return "", false, nil
	}

	profiles, err := st.ListActiveProfiles()
	if err != nil || len(profiles) == 0 {
		return "", false, nil
	}

	// PM decision.
	decision, err := pmDecide(ctx, text, profiles, provider)
	if err != nil {
		slog.Warn("orchestration: PM decision failed", "error", err)
		return "", false, nil
	}

	if decision.Kind == "direct" {
		return "", false, nil
	}

	if len(decision.Tasks) == 0 {
		return "", false, nil
	}

	maxDepth := int(config.MaxDepth)
	if maxDepth == 0 {
		maxDepth = 3 // default
	}

	// Execute delegations in parallel.
	results, err := fanOutDelegations(ctx, decision.Tasks, provider, st, budget, maxDepth, config, baseDir)
	if err != nil {
		return "", false, fmt.Errorf("delegation fan-out: %w", err)
	}

	// Synthesize results.
	response, err := pmSynthesize(ctx, decision.Tasks, results, provider)
	if err != nil {
		return "", false, fmt.Errorf("synthesis: %w", err)
	}

	return response, true, nil
}

// ---------------------------------------------------------------------------
// PM Decision (JSON)
// ---------------------------------------------------------------------------

func pmDecide(
	ctx context.Context,
	text string,
	profiles []store.ProfileMeta,
	provider llm.Provider,
) (*PMDecision, error) {
	var profileList strings.Builder
	for _, p := range profiles {
		profileList.WriteString(fmt.Sprintf("- %s: %s\n", p.ID, p.Description))
	}

	pmPrompt := fmt.Sprintf(`You are a PM (Project Manager) agent. A user sent this message:

"%s"

Available specialist profiles:
%s
Respond with a JSON object (no markdown fences):
- If the request is simple or doesn't need a specialist:
  {"kind":"direct","reason":"..."}
- If one or more specialists should handle it:
  {"kind":"delegate","reason":"...","tasks":[{"profile_id":"...","task":"..."}]}

Output ONLY valid JSON.`, text, profileList.String())

	messages := []core.LlmMessage{
		{Role: core.RoleUser, Content: pmPrompt},
	}

	resp, err := provider.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(resp.Content)
	raw = stripFences(raw)

	var decision PMDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		slog.Warn("orchestration: JSON parse failed, falling through", "raw", raw, "error", err)
		return &PMDecision{Kind: "direct", Reason: "JSON parse failure"}, nil
	}

	return &decision, nil
}

// ---------------------------------------------------------------------------
// Fan-Out Delegations
// ---------------------------------------------------------------------------

func fanOutDelegations(
	ctx context.Context,
	tasks []PMTaskSpec,
	provider llm.Provider,
	st *store.Store,
	budget *SharedTokenBudget,
	maxDepth int,
	config *core.OrchestrationConfig,
	baseDir string,
) ([]DelegateResult, error) {
	maxDelegates := int(config.MaxDelegates)
	if maxDelegates == 0 {
		maxDelegates = 5
	}
	if len(tasks) > maxDelegates {
		tasks = tasks[:maxDelegates]
	}

	results := make([]DelegateResult, len(tasks))

	// Wrap context so we can cancel all siblings when budget is exhausted.
	allCtx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	g, gCtx := errgroup.WithContext(allCtx)

	for i, task := range tasks {
		g.Go(func() error {
			// Per-child timeout.
			childCtx, cancel := context.WithTimeout(gCtx, 60*time.Second)
			defer cancel()

			result := executeDelegateTask(childCtx, task, provider, st, budget, 1, maxDepth, baseDir)
			results[i] = result

			// If budget exhausted, cancel all remaining siblings.
			if budget != nil && budget.Remaining() == 0 {
				slog.Warn("orchestration: budget exhausted, cancelling remaining", "profile", task.ProfileID)
				cancelAll()
			}

			return nil // never fail the group — we collect results
		})
	}

	_ = g.Wait()
	return results, nil
}

// executeDelegateTask runs a single delegation against a profile.
func executeDelegateTask(
	ctx context.Context,
	task PMTaskSpec,
	provider llm.Provider,
	st *store.Store,
	budget *SharedTokenBudget,
	depth, maxDepth int,
	baseDir string,
) DelegateResult {
	result := DelegateResult{
		ProfileID: task.ProfileID,
		Task:      task.Task,
	}

	// Validate inputs.
	if err := core.ValidateProfileID(task.ProfileID); err != nil {
		result.Result = fmt.Sprintf("invalid profile ID: %s", err)
		return result
	}
	if len(task.Task) > maxDelegateTaskLen {
		result.Result = fmt.Sprintf("task too long (%d > %d chars)", len(task.Task), maxDelegateTaskLen)
		return result
	}
	if depth >= maxDepth {
		result.Result = fmt.Sprintf("max delegation depth reached (%d)", maxDepth)
		return result
	}

	// Load profile.
	meta, ok, err := st.GetProfileMeta(task.ProfileID)
	if err != nil || !ok {
		result.Result = fmt.Sprintf("profile %q not found", task.ProfileID)
		return result
	}

	// Build system prompt: try SOUL.md, fallback to description.
	systemPrompt := loadSOUL(baseDir, task.ProfileID)
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are the %q profile. %s", meta.ID, meta.Description)
	}

	if provider == nil {
		result.Result = "no LLM provider available"
		return result
	}

	messages := []core.LlmMessage{
		{Role: core.RoleSystem, Content: systemPrompt + "\n\nRespond directly with the result."},
		{Role: core.RoleUser, Content: task.Task},
	}

	// Determine token cap for background tasks.
	var maxTokens int
	if task.Background {
		maxTokens = backgroundTokenCap
	}
	_ = maxTokens // TODO: pass to provider when token limit per-call is supported

	resp, err := provider.Generate(ctx, messages)
	if err != nil {
		result.Result = fmt.Sprintf("LLM error: %s", err)
		return result
	}

	// Budget check.
	if budget != nil && resp.Usage != nil {
		if !budget.TrySpendFromUsage(resp.Usage) {
			result.Result = "token budget exhausted"
			return result
		}
		result.TokenUsage = resp.Usage.InputTokens + resp.Usage.OutputTokens
	}

	result.Result = resp.Content
	result.Success = true
	return result
}

// loadSOUL reads ~/.gopaw/profiles/{id}/SOUL.md via core.LoadProfile.
// Returns "" on any failure.
func loadSOUL(baseDir, profileID string) string {
	base, err := core.ResolveBaseDir(baseDir)
	if err != nil {
		return ""
	}
	p, err := core.LoadProfile(base, profileID)
	if err != nil {
		return ""
	}
	return p.Soul
}

// ---------------------------------------------------------------------------
// PM Synthesize
// ---------------------------------------------------------------------------

// pmSynthesize combines delegation results into a single response.
func pmSynthesize(
	ctx context.Context,
	tasks []PMTaskSpec,
	results []DelegateResult,
	provider llm.Provider,
) (string, error) {
	// Count successes and failures.
	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}

	// All failed: return error directly, no LLM call.
	if successCount == 0 {
		var errs strings.Builder
		for _, r := range results {
			errs.WriteString(fmt.Sprintf("[%s] %s\n", r.ProfileID, r.Result))
		}
		return fmt.Sprintf("All delegations failed:\n%s", errs.String()), nil
	}

	// Single task: return directly without synthesis.
	if len(results) == 1 && results[0].Success {
		return results[0].Result, nil
	}

	// Build synthesis prompt.
	var sections strings.Builder
	for _, r := range results {
		marker := ""
		if !r.Success {
			marker = " [FAILED]"
		}
		sections.WriteString(fmt.Sprintf("--- %s (%s)%s ---\n%s\n\n",
			r.ProfileID, r.Task, marker, r.Result))
	}

	synthPrompt := fmt.Sprintf(`You are synthesizing results from multiple specialists.
Combine these results into a single coherent response for the user.
If any section is marked [FAILED], acknowledge the failure briefly.

%s
Provide a unified, natural response.`, sections.String())

	messages := []core.LlmMessage{
		{Role: core.RoleUser, Content: synthPrompt},
	}

	resp, err := provider.Generate(ctx, messages)
	if err != nil {
		// Fallback: return raw sections.
		return sections.String(), nil
	}

	return resp.Content, nil
}
