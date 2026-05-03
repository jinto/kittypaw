package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

// TestPromptAccountSetup_AllFields walks the happy path: every prompt receives
// a non-empty answer and f gets all 4 (or 5) wizard fields populated. Pins the
// minimal interactive contract — a casual rewording of the prompts (or skipping
// a step) breaks this test.
func TestPromptAccountSetup_AllFields(t *testing.T) {
	restoreTelegram := stubAccountTelegramPairing(t, "111")
	defer restoreTelegram()

	stdin := strings.NewReader(strings.Join([]string{
		"1",                  // [1/5] channel → telegram
		"123:fake-bot-token", // [2/5] telegram token
		"1",                  // [3/5] provider → anthropic
		"sk-fake-key",        // [4/5] api-key
		"2",                  // [5/5] model → second Claude choice
		"",
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup("new", stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
	}

	if f.telegramToken != "123:fake-bot-token" {
		t.Errorf("telegramToken = %q, want %q", f.telegramToken, "123:fake-bot-token")
	}
	if !f.telegramTokenFromPrompt {
		t.Error("telegramTokenFromPrompt = false, want true")
	}
	if f.adminChatID != "111" {
		t.Errorf("adminChatID = %q, want 111", f.adminChatID)
	}
	if f.llmProvider != "anthropic" {
		t.Errorf("llmProvider = %q, want anthropic", f.llmProvider)
	}
	if f.llmAPIKey != "sk-fake-key" {
		t.Errorf("llmAPIKey = %q, want sk-fake-key", f.llmAPIKey)
	}
	if want := core.ClaudeModelChoices()[1]; f.llmModel != want {
		t.Errorf("llmModel = %q, want %q", f.llmModel, want)
	}
}

// TestPromptAccountSetup_LocalSkipsAPIKey covers the local-LLM branch: choice "3"
// → provider=local → api-key prompt skipped → llmAPIKey stays empty. A regression
// here would either prompt for a key the user can't supply (Ollama needs none)
// or set provider="3" literally.
func TestPromptAccountSetup_LocalSkipsAPIKey(t *testing.T) {
	restoreTelegram := stubAccountTelegramPairing(t, "111")
	defer restoreTelegram()

	stdin := strings.NewReader(strings.Join([]string{
		"1",                  // [1/5] channel → telegram
		"123:fake-bot-token", // [2/5] telegram token
		"5",                  // [3/5] provider → local
		"",                   // local URL — Enter accepts default
		"",                   // local model — Enter accepts default (llama3)
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup("new", stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
	}

	if f.llmProvider != "openai" {
		t.Errorf("llmProvider = %q, want openai-compatible local", f.llmProvider)
	}
	if f.llmAPIKey != "" {
		t.Errorf("llmAPIKey = %q, want empty (local skips api-key prompt)", f.llmAPIKey)
	}
	if f.llmModel != "llama3" {
		t.Errorf("llmModel = %q, want default llama3", f.llmModel)
	}
	if f.llmBaseURL != core.OllamaDefaultBaseURL+"/chat/completions" {
		t.Errorf("llmBaseURL = %q", f.llmBaseURL)
	}
}

func TestPromptAccountSetupProviderAcceptsArrowSequence(t *testing.T) {
	restoreTelegram := stubAccountTelegramPairing(t, "111")
	defer restoreTelegram()

	stdin := strings.NewReader(strings.Join([]string{
		"1",                  // [1/5] channel → telegram
		"123:fake-bot-token", // [2/5] telegram token
		"\x1b[B",             // [3/5] down arrow → openai
		"sk-fake-key",        // [4/5] api-key
		"\x1b[B",             // [5/5] down arrow → second OpenAI model
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup("new", stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
	}

	if f.llmProvider != "openai" {
		t.Errorf("llmProvider = %q, want openai", f.llmProvider)
	}
	if f.llmAPIKey != "sk-fake-key" {
		t.Errorf("llmAPIKey = %q, want sk-fake-key", f.llmAPIKey)
	}
	if want := core.OpenAIModelChoices()[1]; f.llmModel != want {
		t.Errorf("llmModel = %q, want %q", f.llmModel, want)
	}
}

func TestPromptAccountSetupGeminiProvider(t *testing.T) {
	restoreTelegram := stubAccountTelegramPairing(t, "111")
	defer restoreTelegram()

	stdin := strings.NewReader(strings.Join([]string{
		"1",                  // channel → telegram
		"123:fake-bot-token", // telegram token
		"3",                  // provider → gemini
		"gemini-key",         // api-key
		"1",                  // default Gemini model
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup("new", stdin, &stdout, f); err != nil {
		t.Fatalf("promptAccountSetup: %v", err)
	}

	if f.llmProvider != "gemini" {
		t.Fatalf("llmProvider = %q, want gemini", f.llmProvider)
	}
	if f.llmModel != core.GeminiModelChoices()[0] {
		t.Fatalf("llmModel = %q, want %q", f.llmModel, core.GeminiModelChoices()[0])
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
		"1",           // [5/5] model
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup("new", stdin, &stdout, f); err != nil {
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
	restoreTelegram := stubAccountTelegramPairing(t, "111")
	defer restoreTelegram()

	restore := stubAccountKakaoPairing(t, core.WizardResult{
		KakaoEnabled:    true,
		KakaoRelayWSURL: "wss://relay.example.com/ws/kakao-token",
		APIServerURL:    core.DefaultAPIServerURL,
	})
	defer restore()

	stdin := strings.NewReader(strings.Join([]string{
		"3",                  // [1/5] channel → both
		"123:fake-bot-token", // [2/5] telegram token
		"5",                  // [3/5] provider → local
		"",                   // local URL
		"",                   // local model — Enter accepts default
	}, "\n"))
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	if err := promptAccountSetup("new", stdin, &stdout, f); err != nil {
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

func stubAccountTelegramPairing(t *testing.T, chatID string) func() {
	t.Helper()
	old := runTelegramChatIDWizard
	runTelegramChatIDWizard = func(*bufio.Scanner, io.Writer, string, string) string {
		return chatID
	}
	return func() { runTelegramChatIDWizard = old }
}

func TestPromptTelegramChatIDWaitsForStartAndDetects(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("\n"))
	var stdout bytes.Buffer
	called := false

	chatID := promptTelegramChatID(scanner, &stdout, "123:token", telegramPairingDeps{
		fetchChatID: func(context.Context, string) (string, error) {
			called = true
			return "222", nil
		},
	})

	if !called {
		t.Fatal("fetchChatID was not called")
	}
	if chatID != "222" {
		t.Fatalf("chatID = %q, want 222", chatID)
	}
	if out := stdout.String(); !strings.Contains(out, "/start") || !strings.Contains(out, "Chat ID auto-detect") {
		t.Fatalf("stdout should guide Telegram /start and auto-detect, got %q", out)
	}
}

func TestPromptTelegramChatIDUsesServerPairingWithoutStartPrompt(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	var stdout bytes.Buffer
	directCalled := false

	chatID := promptTelegramChatID(scanner, &stdout, "123:token", telegramPairingDeps{
		serverChatID: func(context.Context, string) (telegramPairingStatus, error) {
			return telegramPairingStatus{Status: "paired", ChatID: "333", Source: "active_channel"}, nil
		},
		fetchChatID: func(context.Context, string) (string, error) {
			directCalled = true
			return "", nil
		},
	})

	if chatID != "333" {
		t.Fatalf("chatID = %q, want 333", chatID)
	}
	if directCalled {
		t.Fatal("direct FetchTelegramChatID must not run when server pairing succeeds")
	}
	if out := stdout.String(); strings.Contains(out, "/start") {
		t.Fatalf("stdout must not ask for /start when server pairing is already complete, got %q", out)
	}
}

func TestPromptTelegramChatIDServerUnavailableFallsBackToDirect(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("\n"))
	var stdout bytes.Buffer
	directCalled := false

	chatID := promptTelegramChatID(scanner, &stdout, "123:token", telegramPairingDeps{
		serverChatID: func(context.Context, string) (telegramPairingStatus, error) {
			return telegramPairingStatus{}, errTelegramPairingServerUnavailable
		},
		fetchChatID: func(context.Context, string) (string, error) {
			directCalled = true
			return "444", nil
		},
	})

	if chatID != "444" {
		t.Fatalf("chatID = %q, want 444", chatID)
	}
	if !directCalled {
		t.Fatal("direct FetchTelegramChatID should run when local server is unavailable")
	}
	if out := stdout.String(); !strings.Contains(out, "/start") {
		t.Fatalf("stdout should keep the server-off /start guide, got %q", out)
	}
}

func TestPromptTelegramChatIDServerWaitingPollsWithoutDirectFetch(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	var stdout bytes.Buffer
	polls := 0
	directCalled := false

	chatID := promptTelegramChatID(scanner, &stdout, "123:token", telegramPairingDeps{
		serverChatID: func(context.Context, string) (telegramPairingStatus, error) {
			polls++
			if polls == 1 {
				return telegramPairingStatus{Status: "waiting"}, nil
			}
			return telegramPairingStatus{Status: "paired", ChatID: "555", Source: "active_channel"}, nil
		},
		fetchChatID: func(context.Context, string) (string, error) {
			directCalled = true
			return "", nil
		},
		serverPollInterval: 0,
		maxServerPolls:     2,
	})

	if chatID != "555" {
		t.Fatalf("chatID = %q, want 555", chatID)
	}
	if polls != 2 {
		t.Fatalf("server polls = %d, want 2", polls)
	}
	if directCalled {
		t.Fatal("direct FetchTelegramChatID must not run while server pairing is polling")
	}
	if out := stdout.String(); strings.Contains(out, "/start") {
		t.Fatalf("stdout must not ask for /start during server-coordinated pairing, got %q", out)
	}
}

// TestPromptAccountSetup_EmptyTokenFails — first prompt empty must reject
// rather than silently create an account with no Telegram channel.
func TestPromptAccountSetup_EmptyTokenFails(t *testing.T) {
	stdin := strings.NewReader("1\n\n")
	var stdout bytes.Buffer
	f := &accountAddFlags{}

	err := promptAccountSetup("new", stdin, &stdout, f)
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "telegram token") {
		t.Errorf("error %q does not mention 'telegram token'", err.Error())
	}
}

func TestPromptAccountSetupDuplicateTelegramTokenStopsBeforeChatIDWizard(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KITTYPAW_CONFIG_DIR", root)
	token := "12345:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	if _, err := core.InitAccount(filepath.Join(root, "accounts"), "alice", core.AccountOpts{
		TelegramToken: token,
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	old := runTelegramChatIDWizard
	runTelegramChatIDWizard = func(*bufio.Scanner, io.Writer, string, string) string {
		t.Fatal("telegram chat-id wizard should not run for a token already owned by another account")
		return ""
	}
	defer func() { runTelegramChatIDWizard = old }()

	stdin := strings.NewReader(strings.Join([]string{
		"1",
		token,
	}, "\n"))
	var stdout bytes.Buffer
	err := promptAccountSetup("new", stdin, &stdout, &accountAddFlags{})
	if err == nil {
		t.Fatal("expected duplicate token error, got nil")
	}
	if !strings.Contains(err.Error(), "already used by account \"alice\"") {
		t.Fatalf("error = %q, want existing account name", err.Error())
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
// no LLM key are available, AND the account isn't a team-space coordinator.
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
func TestResolveAccountProviderChoice(t *testing.T) {
	cases := map[string]struct {
		provider string
		baseURL  string
	}{
		"":           {provider: "anthropic"},
		"1":          {provider: "anthropic"},
		"2":          {provider: "openai"},
		"3":          {provider: "gemini"},
		"4":          {provider: "openrouter", baseURL: core.OpenRouterBaseURL},
		"openrouter": {provider: "openrouter", baseURL: core.OpenRouterBaseURL},
	}
	for in, want := range cases {
		gotProvider, gotBaseURL := resolveAccountProviderChoice(in)
		if gotProvider != want.provider || gotBaseURL != want.baseURL {
			t.Errorf("resolveAccountProviderChoice(%q) = (%q, %q), want (%q, %q)",
				in, gotProvider, gotBaseURL, want.provider, want.baseURL)
		}
	}
}
