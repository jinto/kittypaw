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
	mcpreg "github.com/jinto/gopaw/mcp"
	"github.com/jinto/gopaw/sandbox"
	"github.com/jinto/gopaw/store"
)

const maxRetries = 3

// PermissionCallback is called when the agent needs user permission for an action.
type PermissionCallback func(ctx context.Context, description, resource string) (bool, error)

// RunOptions holds per-call options for Session.Run. Callbacks are scoped to
// a single Run invocation, avoiding shared mutable state across concurrent calls.
type RunOptions struct {
	OnToken      llm.TokenCallback
	OnPermission PermissionCallback
}

// Session holds the injected dependencies for processing events.
// Create once, call Run() for each event. Session is safe for concurrent use;
// per-call state is passed via RunOptions.
type Session struct {
	Provider         llm.Provider
	FallbackProvider llm.Provider
	Sandbox          *sandbox.Sandbox
	Store            *store.Store
	Config           *core.Config
	McpRegistry      *mcpreg.Registry   // nil when no MCP servers configured
	Budget           *SharedTokenBudget // shared across auto-fix, delegation, reflection
}

// Run processes a single event through the agent loop.
func (s *Session) Run(ctx context.Context, event core.Event, opts *RunOptions) (string, error) {
	// Fast path: slash commands
	eventText := FormatEvent(&event)
	if response, handled := tryHandleCommand(ctx, eventText, s); handled {
		return response, nil
	}

	return s.runAgentLoop(ctx, event, eventText, opts)
}

func (s *Session) runAgentLoop(ctx context.Context, event core.Event, rawEventText string, opts *RunOptions) (string, error) {
	loopStart := time.Now()
	channelName := event.Type.ChannelName()
	channelUserID := sessionIDFromEvent(&event)

	// Extract callbacks from options.
	var onToken llm.TokenCallback
	var onPermission PermissionCallback
	if opts != nil {
		onToken = opts.OnToken
		onPermission = opts.OnPermission
	}

	// Resolve cross-channel identity
	agentID := func() string {
		globalID, ok, err := s.Store.ResolveUser(channelName, channelUserID)
		if err == nil && ok {
			slog.Info("resolved cross-channel identity",
				"channel", channelName,
				"channel_user_id", channelUserID,
				"global_user_id", globalID,
			)
			return "user-" + globalID
		}
		return channelName + "-" + channelUserID
	}()

	// Load or create agent state
	state, err := s.Store.LoadState(agentID)
	if err != nil {
		return "", fmt.Errorf("load state: %w", err)
	}
	if state == nil {
		state = &core.AgentState{
			AgentID:      agentID,
			SystemPrompt: SystemPrompt,
		}
		if err := s.Store.SaveState(state); err != nil {
			return "", fmt.Errorf("save initial state: %w", err)
		}
	}

	slog.Info("agent state ready", "phase", core.PhaseInit, "agent_id", agentID)

	// Parse @mention routing
	profileOverride, eventText := "", rawEventText
	if pid, remaining, matched := ParseAtMention(rawEventText); matched {
		meta, ok, _ := s.Store.GetProfileMeta(pid)
		if ok && meta.Active {
			slog.Info("@mention routing", "profile_id", pid)
			profileOverride = pid
			eventText = remaining
		}
	}

	// Add user turn
	userTurn := core.ConversationTurn{
		Role:      core.RoleUser,
		Content:   eventText,
		Timestamp: core.NowTimestamp(),
	}
	state.Turns = append(state.Turns, userTurn)
	if err := s.Store.AddTurn(agentID, &userTurn); err != nil {
		return "", fmt.Errorf("add turn: %w", err)
	}

	// Check daily token budget
	if s.Config.Features.DailyTokenLimit > 0 {
		stats, err := s.Store.TodayStats()
		if err == nil && stats.TotalTokens >= int64(s.Config.Features.DailyTokenLimit) {
			return "", fmt.Errorf("daily token limit reached (%d/%d)",
				stats.TotalTokens, s.Config.Features.DailyTokenLimit)
		}
	}

	// Orchestration gate: PM agent may delegate to profiles.
	if response, handled, orchErr := OrchestrateRequest(
		ctx, eventText, s.Provider, s.Store, &s.Config.Orchestration, s.Budget,
	); orchErr != nil {
		slog.Warn("orchestration error, falling through", "error", orchErr)
	} else if handled {
		assistantTurn := core.ConversationTurn{
			Role:      core.RoleAssistant,
			Content:   response,
			Timestamp: core.NowTimestamp(),
		}
		state.Turns = append(state.Turns, assistantTurn)
		_ = s.Store.AddTurn(agentID, &assistantTurn)
		_ = s.Store.SaveState(state)
		return response, nil
	}

	// Load memory context once before retry loop.
	memoryContext := ""
	if lines, err := s.Store.MemoryContextLines(); err != nil {
		slog.Warn("failed to load memory context", "error", err)
	} else if len(lines) > 0 {
		memoryContext = strings.Join(lines, "\n\n")
	}

	// Build MCP tools section once (AllTools returns cached data).
	var mcpToolsSection string
	if s.McpRegistry != nil {
		mcpToolsSection = BuildMCPToolsSection(s.McpRegistry.AllTools())
	}

	// Main retry loop
	var lastError string
	activeProvider := s.Provider
	fallbackUsed := false

	for attempt := range maxRetries {
		if attempt > 0 {
			slog.Info("retry attempt", "attempt", attempt, "max", maxRetries)
		}

		// Build compaction config based on attempt and feature flags
		compaction := s.compactionForAttempt(attempt)

		// Build prompt
		messages := BuildPrompt(state, eventText, compaction, s.Config, channelName, profileOverride, memoryContext, mcpToolsSection)

		slog.Info("prompt built",
			"phase", core.PhasePrompt,
			"attempt", attempt,
			"message_count", len(messages),
			"recent_window", compaction.RecentWindow,
		)

		// Proactive token budget check
		estTokens := 0
		for _, m := range messages {
			estTokens += EstimateTokens(m.Content)
		}
		tokenBudget := activeProvider.ContextWindow() - activeProvider.MaxTokens()
		if estTokens > tokenBudget && attempt < maxRetries-1 {
			slog.Warn("prompt exceeds token budget, tightening compaction",
				"est_tokens", estTokens, "budget", tokenBudget, "attempt", attempt)
			lastError = fmt.Sprintf("estimated %d tokens exceeds budget %d", estTokens, tokenBudget)
			continue
		}

		// Append error feedback if retrying
		if lastError != "" {
			messages = append(messages, core.LlmMessage{
				Role:    core.RoleUser,
				Content: fmt.Sprintf("Your previous code had an error:\n%s\n\nPlease fix the code and try again.", lastError),
			})
		}

		// Call LLM
		var resp *llm.Response
		if onToken != nil {
			resp, err = activeProvider.GenerateStream(ctx, messages, onToken)
		} else {
			resp, err = activeProvider.Generate(ctx, messages)
		}

		if err != nil {
			// Handle retryable errors
			if attempt < maxRetries-1 {
				slog.Warn("LLM error, retrying", "attempt", attempt, "error", err)
				lastError = err.Error()
				time.Sleep(2 * time.Second)
				continue
			}
			// Try fallback on last attempt
			if !fallbackUsed && s.FallbackProvider != nil {
				slog.Warn("switching to fallback provider", "error", err)
				activeProvider = s.FallbackProvider
				fallbackUsed = true
				lastError = err.Error()
				continue
			}
			return "", fmt.Errorf("LLM error after %d retries: %w", maxRetries, err)
		}

		code := resp.Content
		slog.Info("code generated",
			"phase", core.PhaseGenerate,
			"agent_id", agentID,
			"attempt", attempt,
			"code_len", len(code),
		)

		// Build sandbox context
		jsContext := map[string]any{
			"event":      json.RawMessage(event.Payload),
			"event_type": string(event.Type),
			"agent_id":   agentID,
		}

		// Execute in sandbox with skill resolver
		var resolver sandbox.SkillResolver
		if s.Config.AutonomyLevel != core.AutonomyReadonly {
			resolver = func(ctx context.Context, call core.SkillCall) (string, error) {
				return resolveSkillCall(ctx, call, s, onPermission)
			}
		}

		execResult, err := s.Sandbox.ExecuteWithResolver(ctx, code, jsContext, resolver)
		if err != nil {
			return "", fmt.Errorf("sandbox execute: %w", err)
		}

		if execResult.Success {
			output := execResult.Output
			if output == "" {
				output = "(no output)"
			}

			slog.Info("execution success",
				"phase", core.PhaseFinish,
				"agent_id", agentID,
				"output_len", len(output),
				"skill_calls", len(execResult.SkillCalls),
			)

			// Save assistant turn
			assistantTurn := core.ConversationTurn{
				Role:      core.RoleAssistant,
				Content:   output,
				Code:      code,
				Result:    FormatExecResult(execResult),
				Timestamp: core.NowTimestamp(),
			}
			state.Turns = append(state.Turns, assistantTurn)
			if err := s.Store.AddTurn(agentID, &assistantTurn); err != nil {
				slog.Warn("failed to save assistant turn", "agent_id", agentID, "error", err)
			}
			if err := s.Store.SaveState(state); err != nil {
				slog.Warn("failed to save agent state", "agent_id", agentID, "error", err)
			}

			// Record execution metrics.
			s.recordExecution(agentID, eventText, output, resp, loopStart, attempt, true)

			return output, nil
		}

		// Execution failed — retry
		errMsg := execResult.Error
		if errMsg == "" {
			errMsg = "unknown error"
		}
		slog.Warn("execution failed",
			"phase", core.PhaseRetry,
			"agent_id", agentID,
			"attempt", attempt,
			"error", errMsg,
		)
		lastError = errMsg
	}

	// All retries exhausted
	errMsg := lastError
	if errMsg == "" {
		errMsg = "unknown error"
	}
	slog.Info("retries exhausted", "phase", core.PhaseFinish, "agent_id", agentID)

	assistantTurn := core.ConversationTurn{
		Role:      core.RoleAssistant,
		Content:   fmt.Sprintf("Error after %d retries: %s", maxRetries, errMsg),
		Timestamp: core.NowTimestamp(),
	}
	state.Turns = append(state.Turns, assistantTurn)
	if err := s.Store.AddTurn(agentID, &assistantTurn); err != nil {
		slog.Warn("failed to save error turn", "agent_id", agentID, "error", err)
	}
	if err := s.Store.SaveState(state); err != nil {
		slog.Warn("failed to save agent state after failure", "agent_id", agentID, "error", err)
	}

	s.recordExecution(agentID, eventText, errMsg, nil, loopStart, maxRetries, false)

	return "", fmt.Errorf("code execution failed after %d retries: %s", maxRetries, errMsg)
}

func (s *Session) recordExecution(agentID, input, output string, resp *llm.Response, start time.Time, retries int, success bool) {
	summary := output
	if len(summary) > 200 {
		summary = summary[:200]
	}
	usageJSON := ""
	if resp != nil && resp.Usage != nil {
		if data, err := json.Marshal(resp.Usage); err == nil {
			usageJSON = string(data)
		}
	}
	if err := s.Store.RecordExecution(&store.ExecutionRecord{
		SkillID:       agentID,
		SkillName:     "chat",
		StartedAt:     start.UTC().Format("2006-01-02T15:04:05Z"),
		FinishedAt:    time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		DurationMs:    time.Since(start).Milliseconds(),
		InputParams:   input,
		ResultSummary: summary,
		Success:       success,
		RetryCount:    retries,
		UsageJSON:     usageJSON,
	}); err != nil {
		slog.Warn("failed to record execution", "agent_id", agentID, "error", err)
	}
}

func (s *Session) compactionForAttempt(attempt int) CompactionConfig {
	if !s.Config.Features.ContextCompaction {
		return CompactionConfig{RecentWindow: 20, MiddleWindow: 0, TruncateLen: 100}
	}
	if !s.Config.Features.ProgressiveRetry {
		return DefaultCompaction()
	}
	return CompactionForAttempt(attempt)
}

func sessionIDFromEvent(event *core.Event) string {
	payload, err := event.ParsePayload()
	if err != nil {
		return "unknown"
	}
	if payload.SessionID != "" {
		return payload.SessionID
	}
	return payload.ChatID
}
