// Package sandbox executes JavaScript skill code in an isolated subprocess.
//
// Instead of embedding a JS engine, it shells out to deno (preferred) or node,
// wrapping user code in a harness that injects skill-primitive stubs. Each stub
// serialises the call as a tagged JSON line to stdout. The host parses these
// lines to build the list of SkillCalls returned in ExecutionResult.
//
// This is a two-phase design: the sandbox captures calls (stubs return null),
// and the caller resolves them afterward -- matching the Rust/Seatbelt version.
package sandbox

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jinto/gopaw/core"
)

// SkillResolver is called for each skill invocation after sandbox execution.
// It receives a SkillCall and returns the JSON-serialised result string.
type SkillResolver func(ctx context.Context, call core.SkillCall) (string, error)

// Sandbox executes JavaScript code in an isolated subprocess.
type Sandbox struct {
	config core.SandboxConfig
}

// New creates a Sandbox with the given configuration.
func New(config core.SandboxConfig) *Sandbox {
	return &Sandbox{config: config}
}

// Execute runs JavaScript code without skill resolution.
func (s *Sandbox) Execute(ctx context.Context, code string, jsContext map[string]any) (*core.ExecutionResult, error) {
	return s.ExecuteWithResolver(ctx, code, jsContext, nil)
}

// ExecuteWithResolver runs JavaScript code, captures skill calls, and
// optionally resolves them through the provided callback.
func (s *Sandbox) ExecuteWithResolver(
	ctx context.Context,
	code string,
	jsContext map[string]any,
	resolver SkillResolver,
) (*core.ExecutionResult, error) {
	result, err := run(ctx, s.config, code, jsContext)
	if err != nil {
		return nil, err
	}

	// Phase 2: resolve captured skill calls if a resolver was provided.
	if resolver != nil && len(result.SkillCalls) > 0 {
		var resolvedOutputs []string
		for _, call := range result.SkillCalls {
			out, err := resolver(ctx, call)
			if err != nil {
				slog.Warn("skill call resolver error",
					"skill", call.SkillName, "method", call.Method, "error", err)
				continue
			}
			if out != "" {
				resolvedOutputs = append(resolvedOutputs, out)
			}
		}
		if len(resolvedOutputs) > 0 && result.Output == "" {
			result.Output = strings.Join(resolvedOutputs, "\n")
		}
	}

	return result, nil
}
