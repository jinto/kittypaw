package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/llm"
)

// initFlags holds the cobra flag values for `kittypaw init`.
type initFlags struct {
	provider       string
	apiKey         string
	localURL       string
	localModel     string
	telegramToken  string
	telegramChatID string
	workspace      string
	httpAccess     bool
	force          bool
}

// runWizard drives the 4-step interactive wizard or applies flags.
// Returns a WizardResult. Never writes files.
func runWizard(flags initFlags, existing *core.Config) (core.WizardResult, error) {
	var w core.WizardResult

	// Non-interactive: populate from flags.
	if flags.provider != "" {
		return runNonInteractive(flags)
	}

	// TTY check: if not a terminal and no --provider flag, bail.
	if !isTTY() {
		return w, fmt.Errorf("not a terminal — use flags for non-interactive setup\n" +
			"  example: kittypaw init --provider anthropic --api-key $ANTHROPIC_API_KEY\n" +
			"  run kittypaw init --help for all options")
	}

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("  KittyPaw — AI Automation Framework")
	fmt.Println()

	// [1/4] LLM
	if err := wizardLLM(scanner, existing, &w); err != nil {
		return w, err
	}

	// [2/4] Telegram
	wizardTelegram(scanner, existing, &w)

	// [3/4] KakaoTalk — auto-skip
	fmt.Println()
	fmt.Println("  [3/4] KakaoTalk")
	fmt.Println("  > KakaoTalk requires the relay server. Skipping.")
	fmt.Println("    Configure via web onboarding after `kittypaw serve`.")

	// [4/4] Workspace & HTTP
	wizardWorkspaceHTTP(scanner, existing, &w)

	return w, nil
}

func runNonInteractive(flags initFlags) (core.WizardResult, error) {
	var w core.WizardResult

	provider, model, baseURL := core.ResolveLLMConfig(flags.provider, flags.localURL, flags.localModel)
	if provider == "" {
		return w, fmt.Errorf("unknown provider: %s", flags.provider)
	}
	w.LLMProvider = provider
	w.LLMModel = model
	w.LLMBaseURL = baseURL

	switch strings.ToLower(flags.provider) {
	case "local", "ollama":
		if flags.localModel == "" {
			return w, fmt.Errorf("--local-model is required for provider %q", flags.provider)
		}
	default:
		if flags.apiKey == "" {
			return w, fmt.Errorf("--api-key is required for provider %q", flags.provider)
		}
		w.LLMAPIKey = flags.apiKey
	}

	if flags.telegramToken != "" {
		if !core.ValidateTelegramToken(flags.telegramToken) {
			return w, fmt.Errorf("invalid telegram bot token format")
		}
		w.TelegramBotToken = flags.telegramToken
		w.TelegramChatID = flags.telegramChatID
	}

	if flags.workspace != "" {
		abs, err := filepath.Abs(flags.workspace)
		if err != nil {
			return w, fmt.Errorf("invalid workspace path: %w", err)
		}
		w.WorkspacePath = abs
	}

	w.HTTPAccess = flags.httpAccess
	return w, nil
}

// ---------------------------------------------------------------------------
// Step [1/4]: LLM
// ---------------------------------------------------------------------------

func wizardLLM(scanner *bufio.Scanner, existing *core.Config, w *core.WizardResult) error {
	fmt.Println("  [1/4] LLM Selection")

	defaultIdx := 1
	if existing != nil {
		switch {
		case existing.LLM.Provider == "anthropic":
			defaultIdx = 1
		case existing.LLM.BaseURL == core.OpenRouterBaseURL:
			defaultIdx = 2
		case existing.LLM.BaseURL != "":
			defaultIdx = 3
		}
	}

	choice := promptChoice(scanner, "  > ", []string{"Claude API", "OpenRouter", "Local (Ollama)"}, defaultIdx)

	var provider, apiKey, localURL, localModel string
	switch choice {
	case 1:
		provider = "anthropic"
		var err error
		apiKey, err = promptPassword("  API Key: ")
		if err != nil {
			return fmt.Errorf("read API key: %w", err)
		}
		if apiKey == "" && existing != nil && existing.LLM.Provider == "anthropic" {
			apiKey = existing.LLM.APIKey
			fmt.Println("  (keeping existing key)")
		} else if apiKey != "" {
			fmt.Printf("  ✓ %s\n", maskKey(apiKey))
		}
	case 2:
		provider = "openrouter"
		var err error
		apiKey, err = promptPassword("  API Key: ")
		if err != nil {
			return fmt.Errorf("read API key: %w", err)
		}
		if apiKey == "" && existing != nil && existing.LLM.BaseURL == core.OpenRouterBaseURL {
			apiKey = existing.LLM.APIKey
			fmt.Println("  (keeping existing key)")
		} else if apiKey != "" {
			fmt.Printf("  ✓ %s\n", maskKey(apiKey))
		}
	case 3:
		provider = "local"
		defURL := core.OllamaDefaultBaseURL
		if existing != nil && existing.LLM.BaseURL != "" && existing.LLM.BaseURL != core.OpenRouterBaseURL {
			defURL = strings.TrimSuffix(existing.LLM.BaseURL, "/chat/completions")
		}
		localURL = promptLine(scanner, "  URL", defURL)

		defModel := ""
		if existing != nil && existing.LLM.BaseURL != "" {
			defModel = existing.LLM.Model
		}
		localModel = promptLine(scanner, "  Model", defModel)
	}

	resolvedProvider, model, baseURL := core.ResolveLLMConfig(provider, localURL, localModel)
	w.LLMProvider = resolvedProvider
	w.LLMAPIKey = apiKey
	w.LLMModel = model
	w.LLMBaseURL = baseURL

	// Connection test.
	testCfg := core.LLMConfig{
		Provider:  resolvedProvider,
		APIKey:    apiKey,
		Model:     model,
		BaseURL:   baseURL,
		MaxTokens: 128,
	}
	fmt.Print("  Connecting... ")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	p, err := llm.NewProviderFromConfig(testCfg)
	if err != nil {
		fmt.Printf("FAIL (%v)\n", err)
		if !promptYesNo(scanner, "  Save anyway?", true) {
			return fmt.Errorf("aborted by user")
		}
		return nil
	}

	_, err = p.Generate(ctx, []core.LlmMessage{{Role: core.RoleUser, Content: "hi"}})
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("FAIL (%v)\n", err)
		if !promptYesNo(scanner, "  Key may be invalid. Save anyway?", true) {
			return fmt.Errorf("aborted by user")
		}
	} else {
		fmt.Printf("%s %s OK (%dms)\n", resolvedProvider, model, elapsed.Milliseconds())
	}

	return nil
}

// ---------------------------------------------------------------------------
// Step [2/4]: Telegram
// ---------------------------------------------------------------------------

func wizardTelegram(scanner *bufio.Scanner, _ *core.Config, w *core.WizardResult) {
	fmt.Println()
	fmt.Println("  [2/4] Telegram (optional)")

	if !promptYesNo(scanner, "  > Connect?", false) {
		return
	}

	token, err := promptPassword("  Bot Token: ")
	if err != nil || token == "" {
		fmt.Println("  Skipping Telegram.")
		return
	}

	if !core.ValidateTelegramToken(token) {
		fmt.Println("  Invalid token format. Skipping.")
		return
	}

	fmt.Printf("  ✓ %s\n", maskKey(token))
	w.TelegramBotToken = token

	// Guide user to send /start before auto-detect.
	printTelegramGuide()
	fmt.Printf("  > ")
	scanner.Scan() // wait for Enter

	// Auto-detect chat ID.
	fmt.Print("  Chat ID auto-detect... ")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	chatID, err := core.FetchTelegramChatID(ctx, token)
	if err != nil {
		fmt.Println("failed.")
		printTelegramChatIDHelp()
		manual := promptLine(scanner, "  Chat ID", "")
		if manual != "" {
			w.TelegramChatID = manual
		}
	} else {
		fmt.Printf("%s ✓\n", chatID)
		w.TelegramChatID = chatID
	}
}

func printTelegramGuide() {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		fmt.Println()
		fmt.Println("  📱 텔레그램에서 봇에게 /start 를 보내세요.")
		fmt.Println("     보낸 뒤 Enter를 누르면 Chat ID를 자동으로 찾습니다.")
		fmt.Println()
	case strings.HasPrefix(lang, "ja"):
		fmt.Println()
		fmt.Println("  📱 Telegramでボットに /start を送信してください。")
		fmt.Println("     送信後、Enterを押すとChat IDを自動検出します。")
		fmt.Println()
	default:
		fmt.Println()
		fmt.Println("  📱 Send /start to your bot in Telegram.")
		fmt.Println("     Then press Enter to auto-detect your Chat ID.")
		fmt.Println()
	}
}

func printTelegramChatIDHelp() {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		fmt.Println("  봇에게 메시지를 보냈는지 확인하세요.")
		fmt.Println("  @userinfobot 에게 메시지를 보내면 Chat ID를 알 수 있습니다.")
	case strings.HasPrefix(lang, "ja"):
		fmt.Println("  ボットにメッセージを送信したか確認してください。")
		fmt.Println("  @userinfobot にメッセージを送るとChat IDを確認できます。")
	default:
		fmt.Println("  Make sure you sent a message to the bot.")
		fmt.Println("  You can also message @userinfobot to find your Chat ID.")
	}
}

func detectLang() string {
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}
	return strings.ToLower(lang)
}

// ---------------------------------------------------------------------------
// Step [4/4]: Workspace & HTTP
// ---------------------------------------------------------------------------

func wizardWorkspaceHTTP(scanner *bufio.Scanner, existing *core.Config, w *core.WizardResult) {
	fmt.Println()
	fmt.Println("  [4/4] Workspace & Permissions")

	defWS := ""
	if existing != nil && len(existing.Sandbox.AllowedPaths) > 0 {
		defWS = existing.Sandbox.AllowedPaths[0]
	}
	ws := promptLine(scanner, "  Workspace path (empty=skip)", defWS)
	if ws != "" {
		abs, err := filepath.Abs(ws)
		if err == nil {
			ws = abs
		}
		if info, err := os.Stat(ws); err != nil || !info.IsDir() {
			if promptYesNo(scanner, fmt.Sprintf("  %s does not exist. Create?", ws), true) {
				if err := os.MkdirAll(ws, 0o755); err != nil {
					fmt.Printf("  Failed to create: %v\n", err)
					ws = ""
				}
			} else {
				ws = ""
			}
		}
		w.WorkspacePath = ws
	}

	w.HTTPAccess = promptYesNo(scanner, "  Allow HTTP access?", true)
}

// ---------------------------------------------------------------------------
// Input helpers
// ---------------------------------------------------------------------------

func isTTY() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// promptLine reads a single line. Shows defaultVal in brackets; Enter returns it.
func promptLine(scanner *bufio.Scanner, prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	if !scanner.Scan() {
		return defaultVal
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return defaultVal
	}
	return line
}

// promptPassword reads input showing * for each character typed.
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	if !isTTY() {
		// Fallback for non-TTY (piped input).
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return "", nil
		}
		return strings.TrimSpace(scanner.Text()), nil
	}

	fd := int(syscall.Stdin)
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Fall back to hidden input if raw mode fails.
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	defer term.Restore(fd, oldState)

	var buf []byte
	var b [1]byte
	for {
		if _, err := os.Stdin.Read(b[:]); err != nil {
			break
		}
		switch b[0] {
		case '\r', '\n': // Enter
			fmt.Print("\r\n")
			return strings.TrimSpace(string(buf)), nil
		case 3: // Ctrl+C
			fmt.Print("\r\n")
			return "", nil
		case 127, 8: // DEL, Backspace
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Print("\b \b")
			}
		default:
			if b[0] >= 32 { // printable
				buf = append(buf, b[0])
				fmt.Print("*")
			}
		}
	}
	fmt.Print("\r\n")
	return strings.TrimSpace(string(buf)), nil
}

// maskKey returns a masked version of an API key, e.g. "sk-ant-...x2f4".
func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:6] + "..." + key[len(key)-4:]
}

// promptYesNo asks a yes/no question. defaultYes controls Enter behavior.
func promptYesNo(scanner *bufio.Scanner, prompt string, defaultYes bool) bool {
	hint := "(y/N)"
	if defaultYes {
		hint = "(Y/n)"
	}
	fmt.Printf("%s %s: ", prompt, hint)
	if !scanner.Scan() {
		return defaultYes
	}
	ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
	switch ans {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultYes
	}
}

// promptChoice presents numbered options and returns a 1-indexed selection.
func promptChoice(scanner *bufio.Scanner, prompt string, options []string, defaultIdx int) int {
	for i, opt := range options {
		marker := "  "
		if i+1 == defaultIdx {
			marker = "> "
		}
		fmt.Printf("  %s(%d) %s\n", marker, i+1, opt)
	}
	fmt.Printf("%sSelect [%d]: ", prompt, defaultIdx)
	if !scanner.Scan() {
		return defaultIdx
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return defaultIdx
	}
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err == nil && idx >= 1 && idx <= len(options) {
		return idx
	}
	return defaultIdx
}
