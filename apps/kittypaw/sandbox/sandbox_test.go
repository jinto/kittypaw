package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/jinto/gopaw/core"
)

func TestExecuteSimpleReturn(t *testing.T) {
	sb := New(core.SandboxConfig{TimeoutSecs: 5})
	result, err := sb.Execute(context.Background(), `return 1 + 2;`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Output != "3" {
		t.Errorf("expected output %q, got %q", "3", result.Output)
	}
}

func TestExecuteConsoleLog(t *testing.T) {


	sb := New(core.SandboxConfig{TimeoutSecs: 5})
	result, err := sb.Execute(context.Background(), `console.log("hello world");`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "hello world") {
		t.Errorf("expected output to contain %q, got %q", "hello world", result.Output)
	}
}

func TestExecuteSkillCall(t *testing.T) {


	sb := New(core.SandboxConfig{TimeoutSecs: 5})
	code := `
		Http.get("https://example.com");
		Storage.set("key", "value");
		return "done";
	`
	result, err := sb.Execute(context.Background(), code, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(result.SkillCalls) != 2 {
		t.Fatalf("expected 2 skill calls, got %d", len(result.SkillCalls))
	}

	sc0 := result.SkillCalls[0]
	if sc0.SkillName != "Http" || sc0.Method != "get" {
		t.Errorf("call 0: expected Http.get, got %s.%s", sc0.SkillName, sc0.Method)
	}
	if len(sc0.Args) != 1 || string(sc0.Args[0]) != `"https://example.com"` {
		t.Errorf("call 0: unexpected args %v", sc0.Args)
	}

	sc1 := result.SkillCalls[1]
	if sc1.SkillName != "Storage" || sc1.Method != "set" {
		t.Errorf("call 1: expected Storage.set, got %s.%s", sc1.SkillName, sc1.Method)
	}
}

func TestExecuteWithContext(t *testing.T) {


	sb := New(core.SandboxConfig{TimeoutSecs: 5})
	jsCtx := map[string]any{"user": "alice", "count": 42}
	code := `return context.user + ":" + context.count;`
	result, err := sb.Execute(context.Background(), code, jsCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Output != "alice:42" {
		t.Errorf("expected %q, got %q", "alice:42", result.Output)
	}
}

func TestExecuteError(t *testing.T) {


	sb := New(core.SandboxConfig{TimeoutSecs: 5})
	result, err := sb.Execute(context.Background(), `throw new Error("boom");`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(result.Error, "boom") {
		t.Errorf("expected error to contain %q, got %q", "boom", result.Error)
	}
}

func TestExecuteWithResolver(t *testing.T) {


	sb := New(core.SandboxConfig{TimeoutSecs: 5})
	var resolved []core.SkillCall

	resolver := func(ctx context.Context, call core.SkillCall) (string, error) {
		resolved = append(resolved, call)
		return `{"ok":true}`, nil
	}

	code := `
		Telegram.send("chat123", "hello");
		Shell.exec("ls -la");
	`
	result, err := sb.ExecuteWithResolver(context.Background(), code, nil, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved calls, got %d", len(resolved))
	}
	if resolved[0].SkillName != "Telegram" || resolved[0].Method != "send" {
		t.Errorf("resolved[0]: expected Telegram.send, got %s.%s", resolved[0].SkillName, resolved[0].Method)
	}
	if resolved[1].SkillName != "Shell" || resolved[1].Method != "exec" {
		t.Errorf("resolved[1]: expected Shell.exec, got %s.%s", resolved[1].SkillName, resolved[1].Method)
	}
}

func TestSynchronousResolver(t *testing.T) {


	sb := New(core.SandboxConfig{TimeoutSecs: 5})

	resolver := func(_ context.Context, call core.SkillCall) (string, error) {
		if call.SkillName == "Env" && call.Method == "get" {
			return `{"value":"test-path"}`, nil
		}
		return `null`, nil
	}

	code := `
		const result = Env.get("PATH");
		return result.value;
	`
	result, err := sb.ExecuteWithResolver(context.Background(), code, nil, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Output != "test-path" {
		t.Errorf("expected %q, got %q", "test-path", result.Output)
	}
}

func TestAutoReturn(t *testing.T) {


	sb := New(core.SandboxConfig{TimeoutSecs: 5})
	// Code without return — autoReturn should add it.
	result, err := sb.Execute(context.Background(), `"hello from auto-return"`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Output != "hello from auto-return" {
		t.Errorf("expected %q, got %q", "hello from auto-return", result.Output)
	}
}
