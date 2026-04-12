package sandbox

import (
	"bufio"
	"bytes"
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

// run executes JS code in a subprocess using pipe-based I/O.
// Skill calls are resolved synchronously via stdin/stdout protocol:
// the JS stub writes a tagged request to stdout and blocks reading stdin;
// the host reads the request, resolves it through the resolver, and writes
// the JSON response back to stdin.
func run(ctx context.Context, cfg core.SandboxConfig, code string, jsContext map[string]any, resolver SkillResolver) (*core.ExecutionResult, error) {
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

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Capture stderr separately (JS error stack traces, runtime warnings).
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start subprocess: %w", err)
	}

	result := &core.ExecutionResult{Success: true}
	var otherLines []string

	// Process stdout line-by-line, resolving skill calls inline.
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024) // up to 1MB lines

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, tagSkillReq):
			payload := strings.TrimPrefix(line, tagSkillReq)
			var sc wireSkillCall
			if err := json.Unmarshal([]byte(payload), &sc); err != nil {
				stdinPipe.Write([]byte("null\n"))
				continue
			}
			call := sc.toCore()
			result.SkillCalls = append(result.SkillCalls, call)

			// Resolve the skill call and send the result back.
			resp := "null"
			if resolver != nil {
				if out, err := resolver(ctx, call); err == nil && out != "" {
					resp = out
				} else if err != nil {
					slog.Debug("skill resolver error", "skill", call.SkillName, "method", call.Method, "error", err)
				}
			}
			stdinPipe.Write([]byte(resp + "\n"))

		case strings.HasPrefix(line, tagResult):
			result.Output = strings.TrimPrefix(line, tagResult)

		case strings.HasPrefix(line, tagError):
			result.Success = false
			result.Error = strings.TrimPrefix(line, tagError)

		default:
			if t := strings.TrimSpace(line); t != "" {
				otherLines = append(otherLines, t)
			}
		}
	}

	stdinPipe.Close()
	execErr := cmd.Wait()

	// Append untagged console.log output.
	if len(otherLines) > 0 {
		extra := strings.Join(otherLines, "\n")
		if result.Output == "" {
			result.Output = extra
		} else {
			result.Output = extra + "\n" + result.Output
		}
	}

	// Stderr may contain stack traces from uncaught errors.
	if stderr := strings.TrimSpace(stderrBuf.String()); stderr != "" && result.Error == "" {
		result.Success = false
		result.Error = stderr
	}

	// Process-level failure (timeout, signal, non-zero exit).
	if execErr != nil && result.Error == "" {
		result.Success = false
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "execution timed out"
		} else {
			result.Error = execErr.Error()
		}
	}

	return result, nil
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
