package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/llm"
)

// setupFlags holds the cobra flag values for `kittypaw setup`.
type setupFlags struct {
	provider       string
	apiKey         string
	localURL       string
	localModel     string
	telegramToken  string
	telegramChatID string
	firecrawlKey   string
	workspace      string
	httpAccess     bool
	force          bool
	noChat         bool
	noService      bool
	web            bool
}

// runWizard drives the 6-step interactive wizard or applies flags.
// Returns a WizardResult. Never writes files.
func runWizard(flags setupFlags, existing *core.Config) (core.WizardResult, error) {
	var w core.WizardResult

	// Non-interactive: populate from flags.
	if flags.provider != "" {
		return runNonInteractive(flags)
	}

	// TTY check: if not a terminal and no --provider flag, bail.
	if !isTTY() {
		return w, fmt.Errorf("not a terminal — use flags for non-interactive setup\n" +
			"  example: kittypaw setup --provider anthropic --api-key $ANTHROPIC_API_KEY\n" +
			"  run kittypaw setup --help for all options")
	}

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("  KittyPaw — AI Automation Framework")
	fmt.Println()

	// [1/6] LLM
	if err := wizardLLM(scanner, existing, &w); err != nil {
		return w, err
	}

	// [2/6] Telegram
	wizardTelegram(scanner, existing, &w)

	// [3/6] KakaoTalk
	wizardKakao(scanner, existing, &w)

	// [4/6] Web Search
	wizardWebSearch(scanner, existing, &w)

	// [5/6] Workspace & HTTP
	wizardWorkspaceHTTP(scanner, existing, &w)

	// [6/6] KittyPaw API Server
	wizardAPIServer(scanner, &w)

	return w, nil
}

func runNonInteractive(flags setupFlags) (core.WizardResult, error) {
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

	w.FirecrawlKey = flags.firecrawlKey
	w.HTTPAccess = flags.httpAccess
	return w, nil
}

// ---------------------------------------------------------------------------
// Step [1/5]: LLM
// ---------------------------------------------------------------------------

func wizardLLM(scanner *bufio.Scanner, existing *core.Config, w *core.WizardResult) error {
	fmt.Println("  [1/6] LLM Selection")

	// Detect existing LLM config.
	if existing != nil && existing.LLM.Provider != "" {
		name := existing.LLM.Provider
		if existing.LLM.Model != "" {
			name += " (" + existing.LLM.Model + ")"
		}
		fmt.Printf("  ✓ Already configured: %s\n", name)
		if !promptYesNo(scanner, "  > Reconfigure?", false) {
			fmt.Println("  (keeping existing LLM)")
			return nil
		}
	}

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
		claudeModels := []string{"claude-sonnet-4-20250514", "claude-opus-4-20250514", "claude-haiku-4-20250514"}
		modelIdx := promptChoice(scanner, "  Model > ", claudeModels, 1)
		localModel = claudeModels[modelIdx-1]
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
// Step [2/5]: Telegram
// ---------------------------------------------------------------------------

func wizardTelegram(scanner *bufio.Scanner, existing *core.Config, w *core.WizardResult) {
	fmt.Println()
	fmt.Println("  [2/6] Telegram (optional)")

	// Detect existing Telegram config.
	var existingToken string
	var existingChatID string
	if existing != nil {
		for _, ch := range existing.Channels {
			if ch.ChannelType == core.ChannelTelegram && ch.Token != "" {
				existingToken = ch.Token
				break
			}
		}
		if len(existing.AdminChatIDs) > 0 {
			existingChatID = existing.AdminChatIDs[0]
		}
	}

	if existingToken != "" {
		msg := "  ✓ Already connected"
		if existingChatID != "" {
			msg += fmt.Sprintf(" (Chat ID: %s)", maskKey(existingChatID))
		}
		fmt.Println(msg)
		if !promptYesNo(scanner, "  > Reconfigure?", false) {
			fmt.Println("  (keeping existing connection)")
			return
		}
	} else {
		if !promptYesNo(scanner, "  > Connect?", false) {
			return
		}
	}

	printBotFatherGuide()

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

	// Auto-detect chat ID with retries.
	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Print("  Chat ID auto-detect... ")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		chatID, err := core.FetchTelegramChatID(ctx, token)
		cancel()
		if err == nil {
			fmt.Printf("%s ✓\n", chatID)
			w.TelegramChatID = chatID
			return
		}

		if attempt < maxRetries {
			printTelegramRetryHint(attempt)
			fmt.Printf("  > ")
			scanner.Scan()
		} else {
			fmt.Println()
			printTelegramChatIDHelp()
			manual := promptLine(scanner, "  Chat ID", "")
			if manual != "" {
				w.TelegramChatID = manual
			}
		}
	}
}

func printBotFatherGuide() {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		fmt.Println("  1. 텔레그램에서 @BotFather 를 검색하세요")
		fmt.Println("  2. /newbot 을 보내고 안내에 따라 봇을 만드세요")
		fmt.Println("  3. BotFather가 발급한 토큰을 아래에 붙여넣으세요")
		fmt.Println()
	case strings.HasPrefix(lang, "ja"):
		fmt.Println("  1. Telegramで @BotFather を検索してください")
		fmt.Println("  2. /newbot を送信し、案内に従ってボットを作成してください")
		fmt.Println("  3. BotFatherが発行したトークンを下に貼り付けてください")
		fmt.Println()
	default:
		fmt.Println("  1. Search for @BotFather in Telegram")
		fmt.Println("  2. Send /newbot and follow the prompts to create a bot")
		fmt.Println("  3. Paste the token BotFather gives you below")
		fmt.Println()
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

func printTelegramRetryHint(attempt int) {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		if attempt == 1 {
			fmt.Println("아직 감지되지 않았습니다.")
			fmt.Println("  📱 텔레그램에서 봇에게 아무 메시지나 보내고 Enter를 눌러주세요.")
		} else {
			fmt.Println("아직 감지되지 않았습니다.")
			fmt.Println("  📱 봇 이름을 검색해서 대화를 열고, /start 를 보내세요.")
			fmt.Println("     보낸 뒤 Enter를 눌러주세요.")
		}
	case strings.HasPrefix(lang, "ja"):
		if attempt == 1 {
			fmt.Println("まだ検出されませんでした。")
			fmt.Println("  📱 Telegramでボットにメッセージを送ってからEnterを押してください。")
		} else {
			fmt.Println("まだ検出されませんでした。")
			fmt.Println("  📱 ボット名を検索して会話を開き、/start を送信してください。")
			fmt.Println("     送信後にEnterを押してください。")
		}
	default:
		if attempt == 1 {
			fmt.Println("not found yet.")
			fmt.Println("  📱 Send any message to the bot in Telegram, then press Enter.")
		} else {
			fmt.Println("not found yet.")
			fmt.Println("  📱 Search for the bot by name, open the chat, and send /start.")
			fmt.Println("     Then press Enter.")
		}
	}
	fmt.Println()
}

func printTelegramChatIDHelp() {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		fmt.Println("  자동 감지에 실패했습니다. Chat ID를 직접 입력해주세요.")
		fmt.Println("  💡 텔레그램에서 @userinfobot 에게 메시지를 보내면 Chat ID를 알 수 있습니다.")
	case strings.HasPrefix(lang, "ja"):
		fmt.Println("  自動検出に失敗しました。Chat IDを手動で入力してください。")
		fmt.Println("  💡 Telegramで @userinfobot にメッセージを送るとChat IDを確認できます。")
	default:
		fmt.Println("  Auto-detect failed. Please enter your Chat ID manually.")
		fmt.Println("  💡 Message @userinfobot in Telegram to find your Chat ID.")
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
// Step [3/6]: KakaoTalk
// ---------------------------------------------------------------------------

func wizardKakao(scanner *bufio.Scanner, existing *core.Config, w *core.WizardResult) {
	fmt.Println()
	fmt.Println("  [3/6] KakaoTalk (optional)")

	hasExisting := false
	if existing != nil {
		for _, ch := range existing.Channels {
			if ch.ChannelType == core.ChannelKakaoTalk {
				hasExisting = true
				break
			}
		}
	}

	if hasExisting {
		fmt.Println("  ✓ Already enabled")
		return
	}

	if !promptYesNo(scanner, "  > Enable?", false) {
		return
	}

	// KakaoTalk doesn't need an authenticated API session — it only needs the
	// relay URL, which GET /discovery exposes anonymously.
	apiURL := core.DefaultAPIServerURL

	d, err := core.FetchDiscovery(apiURL)
	if err != nil {
		fmt.Printf("  Discovery 실패: %v\n", err)
		fmt.Println("  KakaoTalk 활성화를 건너뜁니다.")
		return
	}
	if d.RelayURL == "" {
		fmt.Println("  Discovery 응답에 relay_url이 없어 페어링을 건너뜁니다.")
		return
	}

	secrets, err := core.LoadAccountSecrets(core.DefaultAccountID)
	if err != nil {
		fmt.Printf("  secrets 로드 실패: %v\n", err)
		return
	}
	mgr := core.NewAPITokenManager("", secrets)

	reg, err := core.RegisterRelaySession(d.RelayURL)
	if err != nil {
		fmt.Printf("  릴레이 등록 실패: %v\n", err)
		fmt.Println("  KakaoTalk 활성화를 건너뜁니다. 네트워크 확인 후 재실행하세요.")
		return
	}

	wsURL := core.WSURLFromRelay(d.RelayURL, reg.Token)
	if err := mgr.SaveKakaoRelayURL(apiURL, wsURL); err != nil {
		fmt.Printf("  WS URL 저장 실패: %v\n", err)
		return
	}

	// Persist apiURL so runSetup writes it to secrets under the bare
	// "kittypaw-api" namespace that InjectKakaoWSURL reads at serve time.
	w.APIServerURL = apiURL
	w.KakaoEnabled = true
	wizardKakaoPairing(scanner, d.RelayURL, reg)
}

func wizardKakaoPairing(_ *bufio.Scanner, relayBase string, reg *core.RelayRegistration) {
	fmt.Println()

	if err := copyToClipboard(reg.PairCode); err == nil {
		fmt.Printf("  인증코드 %s 이 클립보드에 복사되었습니다.\n", reg.PairCode)
	} else {
		fmt.Printf("  인증코드: %s\n", reg.PairCode)
	}

	fmt.Printf("  인증코드 %s 을 채널에 전송하세요.\n", reg.PairCode)
	fmt.Println()
	if err := core.OpenBrowser(reg.ChannelURL); err != nil {
		fmt.Printf("  채널 URL: %s\n", reg.ChannelURL)
	}

	fmt.Print("  페어링 대기 중")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			fmt.Println(" 시간 초과")
			fmt.Println("  ✓ KakaoTalk 활성화 완료 (나중에 채널에서 인증코드를 전송하세요)")
			return
		case <-time.After(3 * time.Second):
			fmt.Print(".")
			if core.CheckRelayPairStatus(relayBase, reg.Token) {
				fmt.Println(" OK")
				fmt.Println("  ✓ KakaoTalk 페어링 완료!")
				return
			}
		}
	}
}

func copyToClipboard(text string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	case "linux":
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	default:
		return fmt.Errorf("unsupported")
	}
}

// ---------------------------------------------------------------------------
// Step [4/5]: Web Search
// ---------------------------------------------------------------------------

func wizardWebSearch(scanner *bufio.Scanner, existing *core.Config, w *core.WizardResult) {
	fmt.Println()
	fmt.Println("  [4/6] Web Search (optional)")

	hasExisting := existing != nil && existing.Web.FirecrawlKey != ""
	if hasExisting {
		fmt.Printf("  ✓ Firecrawl configured (%s)\n", maskKey(existing.Web.FirecrawlKey))
		if !promptYesNo(scanner, "  > Reconfigure?", false) {
			fmt.Println("  (keeping existing Firecrawl key)")
			return
		}
	} else {
		fmt.Println("  Default: DuckDuckGo (free, no API key)")
		fmt.Println("  Upgrade: Firecrawl (higher quality search results)")
		if !promptYesNo(scanner, "  > Configure Firecrawl?", false) {
			return
		}
	}

	key, err := promptPassword("  Firecrawl API Key: ")
	if err != nil || key == "" {
		if hasExisting {
			fmt.Println("  (keeping existing key)")
		} else {
			fmt.Println("  Skipping — using DuckDuckGo.")
		}
		return
	}

	fmt.Printf("  ✓ %s\n", maskKey(key))
	w.FirecrawlKey = key
}

// ---------------------------------------------------------------------------
// Step [5/5]: Workspace & HTTP
// ---------------------------------------------------------------------------

func wizardWorkspaceHTTP(scanner *bufio.Scanner, existing *core.Config, w *core.WizardResult) {
	fmt.Println()
	fmt.Println("  [5/6] Workspace & Permissions")

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
// Step [6/6]: KittyPaw API Server
// ---------------------------------------------------------------------------

func wizardAPIServer(scanner *bufio.Scanner, w *core.WizardResult) {
	fmt.Println()
	fmt.Println("  [6/6] KittyPaw API Server (optional)")
	fmt.Println("  KittyPaw 는 사용자 편의를 위해 몇 가지 API 를 제공하고 있습니다.")
	fmt.Println("  대기질·날씨 등 인증이 필요한 스킬을 사용하시려면 로그인 해주세요.")

	secrets, err := core.LoadAccountSecrets(core.DefaultAccountID)
	if err != nil {
		fmt.Printf("  secrets 로드 실패: %v\n", err)
		return
	}
	mgr := core.NewAPITokenManager("", secrets)
	apiURL := core.DefaultAPIServerURL

	// Already logged in? LoadAccessToken auto-refreshes an expired token using
	// the stored refresh_token, so a live refresh session counts as "logged in".
	if tok, err := mgr.LoadAccessToken(apiURL); err == nil && tok != "" {
		fmt.Printf("  ✓ Logged in (%s)\n", apiURL)
		w.APIServerURL = apiURL
		return
	}

	if !promptYesNo(scanner, "  > Login?", false) {
		return
	}

	if isTTY() {
		err = loginHTTP(apiURL, mgr)
	} else {
		err = loginCode(apiURL, mgr)
	}
	if err != nil {
		fmt.Printf("  로그인 실패: %v\n", err)
		return
	}

	w.APIServerURL = apiURL
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
	defer term.Restore(fd, oldState) //nolint:errcheck

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
