package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

// mkCodeCall builds a Code.exec call with the given JS source string.
func mkCodeCall(t *testing.T, code string) core.SkillCall {
	t.Helper()
	raw, err := json.Marshal(code)
	if err != nil {
		t.Fatalf("marshal code: %v", err)
	}
	return core.SkillCall{
		SkillName: "Code",
		Method:    "exec",
		Args:      []json.RawMessage{raw},
	}
}

// parseCodeResult unwraps the JSON envelope returned by executeCode.
type codeResultEnvelope struct {
	Result any      `json:"result"`
	Logs   []string `json:"logs"`
	Error  string   `json:"error"`
}

func parseCodeResult(t *testing.T, raw string) codeResultEnvelope {
	t.Helper()
	var env codeResultEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw=%q", err, raw)
	}
	return env
}

func TestExecuteCode_PureMath(t *testing.T) {
	// The user-vision use case: KRW-base reframe arithmetic.
	const code = `const usdKrw = 1477.04;
const usdEur = 0.85383;
const eurKrw = usdKrw / usdEur;
eurKrw.toFixed(2);`
	out, err := executeCode(context.Background(), mkCodeCall(t, code), nil)
	if err != nil {
		t.Fatalf("executeCode: %v", err)
	}
	env := parseCodeResult(t, out)
	if env.Error != "" {
		t.Fatalf("unexpected error: %s", env.Error)
	}
	if env.Result == nil {
		t.Fatal("expected numeric result, got nil")
	}
	got, _ := env.Result.(string)
	if got != "1729.90" {
		t.Errorf("expected 1729.90 (= 1477.04 / 0.85383), got %v", env.Result)
	}
}

func TestExecuteCode_ConsoleLogCapture(t *testing.T) {
	const code = `console.log("step 1"); console.log("step 2"); 42;`
	out, _ := executeCode(context.Background(), mkCodeCall(t, code), nil)
	env := parseCodeResult(t, out)
	if len(env.Logs) != 2 {
		t.Fatalf("expected 2 log lines, got %d (%v)", len(env.Logs), env.Logs)
	}
	if env.Logs[0] != "step 1" || env.Logs[1] != "step 2" {
		t.Errorf("logs mismatch: %v", env.Logs)
	}
}

func TestExecuteCode_SyntaxError(t *testing.T) {
	const code = `const x = ;` // syntactically broken
	out, err := executeCode(context.Background(), mkCodeCall(t, code), nil)
	if err != nil {
		t.Fatalf("executeCode returned hard error (should be soft envelope): %v", err)
	}
	env := parseCodeResult(t, out)
	if env.Error == "" {
		t.Fatal("expected error envelope, got success")
	}
	if !strings.Contains(strings.ToLower(env.Error), "unexpected") {
		t.Logf("error message (informational): %q", env.Error)
	}
}

func TestExecuteCode_RuntimeError(t *testing.T) {
	const code = `throw new Error("custom failure");`
	out, _ := executeCode(context.Background(), mkCodeCall(t, code), nil)
	env := parseCodeResult(t, out)
	if env.Error == "" {
		t.Fatal("expected error envelope")
	}
	if !strings.Contains(env.Error, "custom failure") {
		t.Errorf("error should carry thrown message, got %q", env.Error)
	}
}

func TestExecuteCode_NoIOAccess(t *testing.T) {
	// The lock-down promise: Http / Storage / Skill / Llm globals must
	// not be reachable from a Code.exec sandbox. Reference to any of
	// them must produce a ReferenceError, not silently succeed and not
	// somehow reach the network.
	for _, name := range []string{"Http", "Storage", "Skill", "Llm", "Moa", "Memory", "Fanout", "Share"} {
		t.Run(name, func(t *testing.T) {
			code := name + ".get('x');"
			out, _ := executeCode(context.Background(), mkCodeCall(t, code), nil)
			env := parseCodeResult(t, out)
			if env.Error == "" {
				t.Fatalf("%s.get() should be unreachable; got success: %v", name, env.Result)
			}
			if !strings.Contains(env.Error, "not defined") {
				t.Errorf("%s reachable in unexpected shape: %q", name, env.Error)
			}
		})
	}
}

func TestExecuteCode_EmptyCode(t *testing.T) {
	out, _ := executeCode(context.Background(), mkCodeCall(t, "   \n  "), nil)
	env := parseCodeResult(t, out)
	if env.Error == "" {
		t.Fatal("empty code must error")
	}
}

func TestExecuteCode_UnknownMethod(t *testing.T) {
	call := core.SkillCall{
		SkillName: "Code",
		Method:    "evaluate",
		Args:      []json.RawMessage{json.RawMessage(`"42"`)},
	}
	out, _ := executeCode(context.Background(), call, nil)
	env := parseCodeResult(t, out)
	if !strings.Contains(env.Error, "unknown") {
		t.Errorf("unknown method must error, got: %q", env.Error)
	}
}

func TestExecuteCode_TimeoutInterrupts(t *testing.T) {
	// Infinite loop must be killed by the 1s deadline. Acceptable
	// envelope: error mentions interrupt/timeout, OR a hard go error
	// surfaces — both prove the runtime did not run forever.
	const code = `while (true) {}`
	out, err := executeCode(context.Background(), mkCodeCall(t, code), nil)
	if err != nil {
		// Hard error surface is acceptable as long as it returned at all.
		return
	}
	env := parseCodeResult(t, out)
	if env.Error == "" {
		t.Fatal("infinite loop must produce an error envelope")
	}
}
