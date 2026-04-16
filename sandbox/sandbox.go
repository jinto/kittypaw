// Package sandbox executes JavaScript skill code in an isolated subprocess.
//
// Skill calls are resolved synchronously: each JS stub writes a tagged JSON
// request to stdout and blocks reading stdin. The host reads the request,
// resolves it through the provided SkillResolver, and writes the result
// back to stdin. This lets JS code use skill return values directly:
//
//	const path = Env.get("PATH");   // returns the actual value
//	const data = Http.get(url);     // returns {status, body}
package sandbox

import (
	"context"

	"github.com/jinto/kittypaw/core"
)

// SkillResolver is called for each skill invocation during sandbox execution.
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

// ExecuteWithResolver runs JavaScript code and resolves skill calls
// synchronously through the provided callback. Resolver results are
// auto-parsed from JSON into JS objects (for LLM-generated code).
func (s *Sandbox) ExecuteWithResolver(
	ctx context.Context,
	code string,
	jsContext map[string]any,
	resolver SkillResolver,
) (*core.ExecutionResult, error) {
	return run(ctx, s.config, code, jsContext, resolver, execOpts{})
}

// ExecutePackage runs package code with raw resolver results. Skill stubs
// return JSON strings instead of parsed objects, matching the convention that
// packages call JSON.parse() on skill results themselves.
func (s *Sandbox) ExecutePackage(
	ctx context.Context,
	code string,
	jsContext map[string]any,
	resolver SkillResolver,
) (*core.ExecutionResult, error) {
	return run(ctx, s.config, code, jsContext, resolver, execOpts{rawResolverResults: true})
}
