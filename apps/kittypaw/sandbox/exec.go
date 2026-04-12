package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
)

// run executes JS code in a subprocess and parses the tagged stdout protocol.
func run(ctx context.Context, cfg core.SandboxConfig, code string, jsContext map[string]any) (*core.ExecutionResult, error) {
	if jsContext == nil {
		jsContext = map[string]any{}
	}

	wrapper, err := buildWrapper(code, jsContext)
	if err != nil {
		return nil, fmt.Errorf("build wrapper: %w", err)
	}

	// Write the wrapper to a temp file.
	tmpDir, err := os.MkdirTemp("", "gopaw-sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "run.js")
	if err := os.WriteFile(scriptPath, []byte(wrapper), 0o600); err != nil {
		return nil, fmt.Errorf("write script: %w", err)
	}

	// Choose runtime: deno first, then node.
	runtime, args := pickRuntime(scriptPath)
	if runtime == "" {
		return nil, fmt.Errorf("no JavaScript runtime found: install deno or node")
	}

	// Apply timeout from config (default 30s).
	timeout := time.Duration(cfg.TimeoutSecs) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, runtime, args...)
	cmd.Dir = tmpDir

	out, execErr := cmd.CombinedOutput()

	return parseOutput(string(out), execErr)
}

// pickRuntime returns the binary name and arguments for running a JS file.
// Prefers deno for its built-in permission model; falls back to node.
func pickRuntime(scriptPath string) (string, []string) {
	if p, err := exec.LookPath("deno"); err == nil {
		return p, []string{"run", "--no-prompt", scriptPath}
	}
	if p, err := exec.LookPath("node"); err == nil {
		slog.Warn("sandbox: using node (no process isolation) — install deno for secure sandboxing")
		return p, []string{scriptPath}
	}
	return "", nil
}

// parseOutput reads tagged lines from subprocess output and builds an ExecutionResult.
func parseOutput(raw string, execErr error) (*core.ExecutionResult, error) {
	result := &core.ExecutionResult{Success: true}

	var otherLines []string

	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, tagSkillCall):
			payload := strings.TrimPrefix(line, tagSkillCall)
			var sc wireSkillCall
			if err := json.Unmarshal([]byte(payload), &sc); err == nil {
				result.SkillCalls = append(result.SkillCalls, sc.toCore())
			}

		case strings.HasPrefix(line, tagResult):
			payload := strings.TrimPrefix(line, tagResult)
			result.Output = payload

		case strings.HasPrefix(line, tagError):
			payload := strings.TrimPrefix(line, tagError)
			result.Success = false
			result.Error = payload

		default:
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				otherLines = append(otherLines, trimmed)
			}
		}
	}

	// Append any untagged console.log output.
	if len(otherLines) > 0 {
		extra := strings.Join(otherLines, "\n")
		if result.Output == "" {
			result.Output = extra
		} else {
			result.Output = extra + "\n" + result.Output
		}
	}

	// If the process itself failed (non-zero exit, timeout), mark as error.
	if execErr != nil && result.Error == "" {
		result.Success = false
		if ctx := context.DeadlineExceeded; execErr == ctx {
			result.Error = "execution timed out"
		} else {
			result.Error = execErr.Error()
		}
	}

	return result, nil
}

// wireSkillCall matches the JSON shape emitted by the JS stubs.
type wireSkillCall struct {
	Skill  string        `json:"skill"`
	Method string        `json:"method"`
	Args   []interface{} `json:"args"`
}

func (w wireSkillCall) toCore() core.SkillCall {
	rawArgs := make([]json.RawMessage, 0, len(w.Args))
	for _, a := range w.Args {
		if b, err := json.Marshal(a); err == nil {
			rawArgs = append(rawArgs, b)
		}
	}
	return core.SkillCall{
		SkillName: w.Skill,
		Method:    w.Method,
		Args:      rawArgs,
	}
}
