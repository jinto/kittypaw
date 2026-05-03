package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

// TestPromptAccountSetup_AllFields walks the happy path: every prompt receives
// a non-empty answer and f gets all 4 (or 5) wizard fields populated. Pins the
// minimal interactive contract — a casual rewording of the prompts (or skipping
// a step) breaks this test.
func TestPromptAccountSetup_AllFields(t *testing.T) {
	stdin := strings.NewReader(strings.Join([]string{
		"1",                  // [1/5] channel → telegram
		"123:fake-bot-token", // [2/5] telegram token
		"1",                  // [3/5] provider → anthropic
		"sk-fake-key",        // [4/5] api-key
		"claude-opus",        // [5/5] model (override default)
		"",
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup(stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
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

// TestPromptAccountSetup_LocalSkipsAPIKey covers the local-LLM branch: choice "3"
// → provider=local → api-key prompt skipped → llmAPIKey stays empty. A regression
// here would either prompt for a key the user can't supply (Ollama needs none)
// or set provider="3" literally.
func TestPromptAccountSetup_LocalSkipsAPIKey(t *testing.T) {
	stdin := strings.NewReader(strings.Join([]string{
		"1",                  // [1/5] channel → telegram
		"123:fake-bot-token", // [2/5] telegram token
		"3",                  // [3/5] provider → local
		"",                   // [5/5] model — Enter accepts default (llama3)
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup(stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
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

func TestPromptAccountSetupProviderAcceptsArrowSequence(t *testing.T) {
	stdin := strings.NewReader(strings.Join([]string{
		"1",                  // [1/5] channel → telegram
		"123:fake-bot-token", // [2/5] telegram token
		"\x1b[B",             // [3/5] down arrow → openai
		"sk-fake-key",        // [4/5] api-key
		"gpt-test",           // [5/5] model
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup(stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
	}

	if f.llmProvider != "openai" {
		t.Errorf("llmProvider = %q, want openai", f.llmProvider)
	}
	if f.llmAPIKey != "sk-fake-key" {
		t.Errorf("llmAPIKey = %q, want sk-fake-key", f.llmAPIKey)
	}
	if f.llmModel != "gpt-test" {
		t.Errorf("llmModel = %q, want gpt-test", f.llmModel)
	}
}

func TestPromptAccountSetupKakaoOnly(t *testing.T) {
	restore := stubAccountKakaoPairing(t, core.WizardResult{
		KakaoEnabled:    true,
		KakaoRelayWSURL: "wss://relay.example.com/ws/kakao-token",
		APIServerURL:    core.DefaultAPIServerURL,
	})
	defer restore()

	stdin := strings.NewReader(strings.Join([]string{
		"\x1b[B",      // [1/5] down arrow → KakaoTalk
		"1",           // [3/5] provider → anthropic
		"sk-fake-key", // [4/5] api-key
		"claude-test", // [5/5] model
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup(stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
	}

	if f.telegramToken != "" {
		t.Fatalf("telegramToken = %q, want empty for Kakao-only", f.telegramToken)
	}
	if !f.kakaoEnabled || f.kakaoRelayWSURL != "wss://relay.example.com/ws/kakao-token" {
		t.Fatalf("kakao = (%v, %q), want true ws URL", f.kakaoEnabled, f.kakaoRelayWSURL)
	}
}

func TestPromptAccountSetupBothChannels(t *testing.T) {
	restore := stubAccountKakaoPairing(t, core.WizardResult{
		KakaoEnabled:    true,
		KakaoRelayWSURL: "wss://relay.example.com/ws/kakao-token",
		APIServerURL:    core.DefaultAPIServerURL,
	})
	defer restore()

	stdin := strings.NewReader(strings.Join([]string{
		"3",                  // [1/5] channel → both
		"123:fake-bot-token", // [2/5] telegram token
		"3",                  // [3/5] provider → local
		"",                   // [5/5] model — Enter accepts default
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup(stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
	}

	if f.telegramToken != "123:fake-bot-token" {
		t.Fatalf("telegramToken = %q, want token", f.telegramToken)
	}
	if !f.kakaoEnabled || f.kakaoRelayWSURL == "" {
		t.Fatalf("expected Kakao enabled with ws URL, got (%v, %q)", f.kakaoEnabled, f.kakaoRelayWSURL)
	}
}

func stubAccountKakaoPairing(t *testing.T, result core.WizardResult) func() {
	t.Helper()
	old := runKakaoPairingWizard
	runKakaoPairingWizard = func(io.Writer) (core.WizardResult, error) {
		return result, nil
	}
	return func() { runKakaoPairingWizard = old }
}

// TestPromptAccountSetup_EmptyTokenFails — first prompt empty must reject
// rather than silently create an account with no Telegram channel.
func TestPromptAccountSetup_EmptyTokenFails(t *testing.T) {
	stdin := strings.NewReader("1\n\n")
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	err := promptAccountSetup(stdin, &stdout, f)
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "telegram token") {
		t.Errorf("error %q does not mention 'telegram token'", err.Error())
	}
}

func TestReadSecretMaskedLoopPrintsStarsAndHandlesBackspace(t *testing.T) {
	input := []byte{'a', 'b', 'c', 127, 'd', '\n'}
	pos := 0
	var stdout bytes.Buffer

	got, err := readSecretMaskedLoop(func(p []byte) (int, error) {
		if pos >= len(input) {
			return 0, nil
		}
		p[0] = input[pos]
		pos++
		return 1, nil
	}, &stdout)
	if err != nil {
		t.Fatalf("readSecretMaskedLoop: %v", err)
	}

	if got != "abd" {
		t.Fatalf("secret = %q, want abd", got)
	}
	if want := "***\b \b*\r\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// TestNeedsAccountPrompt covers the gating logic that decides when to launch
// the interactive prompt. The contract: launch only when no token source AND
// no LLM key are available, AND the account isn't a shared coordinator.
func TestNeedsAccountPrompt(t *testing.T) {
	cases := []struct {
		name string
		f    *accountAddFlags
		want bool
	}{
		{"all empty", &accountAddFlags{}, true},
		{"token via flag", &accountAddFlags{telegramToken: "x"}, false},
		{"token via stdin flag", &accountAddFlags{telegramTokenStdin: true}, false},
		{"llm key only", &accountAddFlags{llmAPIKey: "sk-x"}, false},
		{"local provider", &accountAddFlags{llmProvider: "local"}, false},
		{"kakao configured", &accountAddFlags{kakaoEnabled: true}, false},
		{"shared — no prompt regardless", &accountAddFlags{isShared: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := needsAccountPrompt(tc.f)
			if got != tc.want {
				t.Errorf("needsAccountPrompt(%+v) = %v, want %v", tc.f, got, tc.want)
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
