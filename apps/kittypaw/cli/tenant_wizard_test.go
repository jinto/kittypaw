package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestPromptTenantSetup_AllFields walks the happy path: every prompt receives
// a non-empty answer and f gets all 4 (or 5) wizard fields populated. Pins the
// minimal interactive contract — a casual rewording of the prompts (or skipping
// a step) breaks this test.
func TestPromptTenantSetup_AllFields(t *testing.T) {
	stdin := strings.NewReader(strings.Join([]string{
		"123:fake-bot-token", // [1/4] telegram token
		"1",                  // [2/4] provider → anthropic
		"sk-fake-key",        // [3/4] api-key
		"claude-opus",        // [4/4] model (override default)
		"",
	}, "\n"))
	var stdout bytes.Buffer
	f := &tenantAddFlags{}

	if err := promptTenantSetup(stdin, &stdout, f); err != nil {
		t.Fatalf("promptTenantSetup: %v", err)
	}

	if f.telegramToken != "123:fake-bot-token" {
		t.Errorf("telegramToken = %q, want %q", f.telegramToken, "123:fake-bot-token")
	}
	if f.llmProvider != "anthropic" {
		t.Errorf("llmProvider = %q, want anthropic", f.llmProvider)
	}
	if f.llmAPIKey != "sk-fake-key" {
		t.Errorf("llmAPIKey = %q, want sk-fake-key", f.llmAPIKey)
	}
	if f.llmModel != "claude-opus" {
		t.Errorf("llmModel = %q, want claude-opus", f.llmModel)
	}
}

// TestPromptTenantSetup_LocalSkipsAPIKey covers the local-LLM branch: choice "3"
// → provider=local → api-key prompt skipped → llmAPIKey stays empty. A regression
// here would either prompt for a key the user can't supply (Ollama needs none)
// or set provider="3" literally.
func TestPromptTenantSetup_LocalSkipsAPIKey(t *testing.T) {
	stdin := strings.NewReader(strings.Join([]string{
		"123:fake-bot-token", // [1/4] telegram token
		"3",                  // [2/4] provider → local
		"",                   // [4/4] model — Enter accepts default (llama3)
	}, "\n"))
	var stdout bytes.Buffer
	f := &tenantAddFlags{}

	if err := promptTenantSetup(stdin, &stdout, f); err != nil {
		t.Fatalf("promptTenantSetup: %v", err)
	}

	if f.llmProvider != "local" {
		t.Errorf("llmProvider = %q, want local", f.llmProvider)
	}
	if f.llmAPIKey != "" {
		t.Errorf("llmAPIKey = %q, want empty (local skips api-key prompt)", f.llmAPIKey)
	}
	if f.llmModel != "llama3" {
		t.Errorf("llmModel = %q, want default llama3", f.llmModel)
	}
}

// TestPromptTenantSetup_EmptyTokenFails — first prompt empty must reject
// rather than silently create a tenant with no Telegram channel.
func TestPromptTenantSetup_EmptyTokenFails(t *testing.T) {
	stdin := strings.NewReader("\n")
	var stdout bytes.Buffer
	f := &tenantAddFlags{}

	err := promptTenantSetup(stdin, &stdout, f)
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "telegram token") {
		t.Errorf("error %q does not mention 'telegram token'", err.Error())
	}
}

// TestNeedsTenantPrompt covers the gating logic that decides when to launch
// the interactive prompt. The contract: launch only when no token source AND
// no LLM key are available, AND the tenant isn't a family coordinator.
func TestNeedsTenantPrompt(t *testing.T) {
	cases := []struct {
		name string
		f    *tenantAddFlags
		want bool
	}{
		{"all empty", &tenantAddFlags{}, true},
		{"token via flag", &tenantAddFlags{telegramToken: "x"}, false},
		{"token via stdin flag", &tenantAddFlags{telegramTokenStdin: true}, false},
		{"llm key only", &tenantAddFlags{llmAPIKey: "sk-x"}, false},
		{"local provider", &tenantAddFlags{llmProvider: "local"}, false},
		{"family — no prompt regardless", &tenantAddFlags{isFamily: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := needsTenantPrompt(tc.f)
			if got != tc.want {
				t.Errorf("needsTenantPrompt(%+v) = %v, want %v", tc.f, got, tc.want)
			}
		})
	}
}

// TestMapProviderChoice — menu integers and free-form input both resolved.
func TestMapProviderChoice(t *testing.T) {
	cases := map[string]string{
		"":           "anthropic",
		"1":          "anthropic",
		"2":          "openai",
		"3":          "local",
		"openrouter": "openrouter", // free-form passes through
	}
	for in, want := range cases {
		if got := mapProviderChoice(in); got != want {
			t.Errorf("mapProviderChoice(%q) = %q, want %q", in, got, want)
		}
	}
}
