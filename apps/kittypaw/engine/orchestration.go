package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/llm"
	"github.com/jinto/gopaw/store"
)

// OrchestrateRequest routes a user message through the PM (Project Manager) agent
// which decides whether to handle directly or delegate to specialized profiles.
// Returns (response, nil) if orchestrated, ("", nil) if should fall through to default loop.
func OrchestrateRequest(
	ctx context.Context,
	text string,
	provider llm.Provider,
	st *store.Store,
	config *core.OrchestrationConfig,
) (string, bool, error) {
	if !config.Enabled {
		return "", false, nil
	}

	profiles, err := st.ListActiveProfiles()
	if err != nil || len(profiles) == 0 {
		return "", false, nil
	}

	// Build PM prompt
	var profileList strings.Builder
	for _, p := range profiles {
		profileList.WriteString(fmt.Sprintf("- %s: %s\n", p.ID, p.Description))
	}

	pmPrompt := fmt.Sprintf(`You are a PM (Project Manager) agent. A user sent this message:

"%s"

Available specialist profiles:
%s

Decide how to handle this request:
1. If the request clearly matches a specialist profile, respond with: DELEGATE:<profile_id>:<task description>
2. If the request is simple and doesn't need a specialist, respond with: DIRECT
3. If the request needs multiple specialists, respond with: PARALLEL:<profile_id1>:<task1>|<profile_id2>:<task2>

Respond with ONLY one of the above formats, nothing else.`, text, profileList.String())

	messages := []core.LlmMessage{
		{Role: core.RoleUser, Content: pmPrompt},
	}

	resp, err := provider.Generate(ctx, messages)
	if err != nil {
		slog.Warn("orchestration: PM call failed", "error", err)
		return "", false, nil // Fall through to default
	}

	decision := strings.TrimSpace(resp.Content)

	switch {
	case decision == "DIRECT":
		return "", false, nil

	case strings.HasPrefix(decision, "DELEGATE:"):
		parts := strings.SplitN(decision, ":", 3)
		if len(parts) < 3 {
			return "", false, nil
		}
		profileID := parts[1]
		task := parts[2]
		slog.Info("orchestration: delegating", "profile", profileID, "task", task)

		// Execute as the delegated profile
		result, err := executeDelegateProfile(ctx, profileID, task, provider, st)
		if err != nil {
			return "", false, fmt.Errorf("delegate %s: %w", profileID, err)
		}
		return result, true, nil

	case strings.HasPrefix(decision, "PARALLEL:"):
		// Parse parallel tasks
		taskParts := strings.Split(strings.TrimPrefix(decision, "PARALLEL:"), "|")
		var results []string
		for _, tp := range taskParts {
			parts := strings.SplitN(tp, ":", 2)
			if len(parts) < 2 {
				continue
			}
			profileID := parts[0]
			task := parts[1]

			result, err := executeDelegateProfile(ctx, profileID, task, provider, st)
			if err != nil {
				results = append(results, fmt.Sprintf("[%s] error: %s", profileID, err))
			} else {
				results = append(results, fmt.Sprintf("[%s] %s", profileID, result))
			}
		}
		return strings.Join(results, "\n\n"), true, nil

	default:
		return "", false, nil
	}
}

func executeDelegateProfile(
	ctx context.Context,
	profileID, task string,
	provider llm.Provider,
	st *store.Store,
) (string, error) {
	meta, ok, err := st.GetProfileMeta(profileID)
	if err != nil || !ok {
		return "", fmt.Errorf("profile %q not found", profileID)
	}

	// Build a profile-specific prompt
	profilePrompt := fmt.Sprintf("You are the %q profile. %s\n\nTask: %s\n\nRespond directly with the result.",
		profileID, meta.Description, task)

	messages := []core.LlmMessage{
		{Role: core.RoleUser, Content: profilePrompt},
	}

	resp, err := provider.Generate(ctx, messages)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// DelegateCtx holds context for agent delegation within the skill executor.
type DelegateCtx struct {
	Provider llm.Provider
	Store    *store.Store
	Config   *core.Config
	Depth    int
	MaxDepth int
}

// TokenBudget tracks shared token consumption across delegated agents.
type TokenBudget struct {
	Limit uint64
	Used  uint64
}

// NewTokenBudget creates a budget with the given limit.
func NewTokenBudget(limit uint64) *TokenBudget {
	return &TokenBudget{Limit: limit}
}

// Spend deducts tokens. Returns false if budget exceeded.
func (b *TokenBudget) Spend(tokens uint64) bool {
	if b.Limit == 0 {
		return true // Unlimited
	}
	if b.Used+tokens > b.Limit {
		return false
	}
	b.Used += tokens
	return true
}

// SpendFromUsage deducts from a TokenUsage.
func (b *TokenBudget) SpendFromUsage(usage *llm.TokenUsage) bool {
	if usage == nil {
		return true
	}
	return b.Spend(uint64(usage.InputTokens + usage.OutputTokens))
}

// Used only to suppress "declared and not used" for json import
var _ = json.Marshal
