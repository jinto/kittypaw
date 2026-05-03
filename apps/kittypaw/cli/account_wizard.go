package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jinto/kittypaw/core"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// needsAccountPrompt returns true when `account add` was invoked without enough
// info to proceed unattended: no channel source AND no LLM key (and the account
// is not a shared coordinator). In that state we'd otherwise reject the command
// — the interactive prompt is a friendlier path.
func needsAccountPrompt(f *accountAddFlags) bool {
	if f.isShared {
		return false
	}
	hasChannelSource := f.telegramToken != "" || f.telegramTokenStdin || os.Getenv(accountEnvBotToken) != "" || f.kakaoEnabled
	hasLLM := f.llmAPIKey != "" || f.llmProvider == "local"
	return !hasChannelSource && !hasLLM
}

// promptAccountSetup walks the user through 5 minimal questions and writes
// the answers back into f. Mutates f directly so the existing runAccountAdd
// flow can pick up the values without further branching.
//
// Steps: channel → channel credentials → LLM provider → LLM api-key (skipped for local) → LLM model.
// Admin chat ID is left for runAccountAdd's auto-detect (FetchTelegramChatID).
//
// Secrets (telegram token, api-key) are read with masked terminal input when
// stdin is a TTY — keeps shoulder-surfers and scrollback buffers from picking
// up the value. Tests use a non-TTY io.Reader and exercise the scanner path.
func promptAccountSetup(accountID string, stdin io.Reader, stdout io.Writer, f *accountAddFlags) error {
	scanner := bufio.NewScanner(stdin)

	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprintln(stdout, "  새 account 설정 — 5 단계 질문")
	_, _ = fmt.Fprintln(stdout)

	// [1/5] Channel
	_, _ = fmt.Fprintln(stdout, "[1/5] Messaging channel")
	channelChoice, err := promptAccountChannelChoice(scanner, stdin, stdout)
	if err != nil {
		return fmt.Errorf("read channel: %w", err)
	}
	channels := mapAccountChannelChoice(channelChoice)

	// [2/5] Channel credentials
	if channels.telegram {
		_, _ = fmt.Fprintln(stdout, "\n[2/5] Telegram 봇 토큰 (BotFather 의 /newbot 결과)")
		_, _ = fmt.Fprint(stdout, "  토큰: ")
		token, err := readSecret(scanner, stdin, stdout)
		if err != nil {
			return fmt.Errorf("read telegram token: %w", err)
		}
		f.telegramToken = strings.TrimSpace(token)
		f.telegramTokenFromPrompt = true
		if f.telegramToken == "" {
			return fmt.Errorf("telegram token required")
		}
		if owner, ok, err := accountTelegramTokenOwner(f.telegramToken); err != nil {
			return fmt.Errorf("check telegram token: %w", err)
		} else if ok {
			return fmt.Errorf("telegram bot_token already used by account %q", owner)
		}
		f.adminChatID = runTelegramChatIDWizard(scanner, stdout, accountID, f.telegramToken)
	}
	if channels.kakao {
		_, _ = fmt.Fprintln(stdout, "\n[2/5] KakaoTalk")
		result, err := runKakaoPairingWizard(stdout)
		if err != nil {
			return fmt.Errorf("configure kakao: %w", err)
		}
		f.kakaoEnabled = result.KakaoEnabled
		f.kakaoRelayWSURL = strings.TrimSpace(result.KakaoRelayWSURL)
		if f.kakaoEnabled && f.kakaoRelayWSURL == "" {
			return fmt.Errorf("kakao relay URL required")
		}
	}
	if !channels.telegram && !channels.kakao {
		return fmt.Errorf("at least one channel is required")
	}

	if err := promptAccountLLM(scanner, stdin, stdout, f); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(stdout)
	return nil
}

type accountChannelSelection struct {
	telegram bool
	kakao    bool
}

// readLine returns the next scanner line (without trailing newline). EOF or
// scan error returns an empty string with the scanner error (or nil for
// clean EOF) — callers decide whether the empty string is acceptable.
func readLine(scanner *bufio.Scanner) (string, error) {
	if !scanner.Scan() {
		return "", scanner.Err()
	}
	return scanner.Text(), nil
}

// readSecret reads a secret with masked echo when stdin is a real
// TTY (*os.File whose fd is a terminal). Falls back to readLine for any other
// reader — primarily tests with strings.NewReader. The fallback intentionally
// echoes; production never hits it because os.Stdin is a *os.File.
func readSecret(scanner *bufio.Scanner, stdin io.Reader, stdout io.Writer) (string, error) {
	f, ok := stdin.(*os.File)
	if !ok || !isatty.IsTerminal(f.Fd()) {
		return readLine(scanner)
	}
	fd := int(f.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		b, readErr := term.ReadPassword(fd)
		// term.ReadPassword swallows the user's Enter without printing a newline,
		// so the next prompt would otherwise sit on the same line as the hidden
		// input. Restore the cursor explicitly.
		_, _ = fmt.Fprintln(stdout)
		if readErr != nil {
			return "", readErr
		}
		return string(b), nil
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	return readSecretMaskedLoop(f.Read, stdout)
}

func promptAccountChannelChoice(scanner *bufio.Scanner, stdin io.Reader, stdout io.Writer) (string, error) {
	options := []string{"Telegram", "KakaoTalk", "Telegram + KakaoTalk"}
	if f, ok := stdin.(*os.File); ok && f == os.Stdin && isatty.IsTerminal(f.Fd()) {
		if out, ok := stdout.(*os.File); ok && out == os.Stdout && isatty.IsTerminal(out.Fd()) {
			if idx, ok := promptChoiceInteractive("  ", options, 1); ok {
				return fmt.Sprintf("%d", idx), nil
			}
		}
	}

	_, _ = fmt.Fprintln(stdout, "  1) Telegram   2) KakaoTalk   3) Telegram + KakaoTalk")
	_, _ = fmt.Fprint(stdout, "  선택 [1]: ")
	return readLine(scanner)
}

func readSecretMaskedLoop(read func([]byte) (int, error), stdout io.Writer) (string, error) {
	var buf []byte
	var b [1]byte
	for {
		n, err := read(b[:])
		if err != nil {
			if err == io.EOF {
				return string(buf), nil
			}
			return "", err
		}
		if n == 0 {
			return string(buf), nil
		}
		switch b[0] {
		case '\r', '\n':
			_, _ = fmt.Fprint(stdout, "\r\n")
			return string(buf), nil
		case 3: // Ctrl+C
			_, _ = fmt.Fprint(stdout, "\r\n")
			return "", nil
		case 127, 8: // DEL, Backspace
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				_, _ = fmt.Fprint(stdout, "\b \b")
			}
		default:
			if b[0] >= 32 {
				buf = append(buf, b[0])
				_, _ = fmt.Fprint(stdout, "*")
			}
		}
	}
}

func mapAccountChannelChoice(s string) accountChannelSelection {
	idx := choiceFromInput(s, 1, 3)
	switch idx {
	case 2:
		return accountChannelSelection{kakao: true}
	case 3:
		return accountChannelSelection{telegram: true, kakao: true}
	default:
		return accountChannelSelection{telegram: true}
	}
}

func promptAccountProviderChoice(scanner *bufio.Scanner, stdin io.Reader, stdout io.Writer) (string, error) {
	options := setupLLMProviderChoices()
	if f, ok := stdin.(*os.File); ok && f == os.Stdin && isatty.IsTerminal(f.Fd()) {
		if out, ok := stdout.(*os.File); ok && out == os.Stdout && isatty.IsTerminal(out.Fd()) {
			if idx, ok := promptChoiceInteractive("  ", options, 1); ok {
				return fmt.Sprintf("%d", idx), nil
			}
		}
	}

	for i, opt := range options {
		_, _ = fmt.Fprintf(stdout, "  %d) %s\n", i+1, opt)
	}
	_, _ = fmt.Fprint(stdout, "  선택 [1]: ")
	return readLine(scanner)
}

func promptAccountLLM(scanner *bufio.Scanner, stdin io.Reader, stdout io.Writer, f *accountAddFlags) error {
	_, _ = fmt.Fprintln(stdout, "\n[3/5] LLM provider")
	choice, err := promptAccountProviderChoice(scanner, stdin, stdout)
	if err != nil {
		return fmt.Errorf("read provider: %w", err)
	}
	provider, presetBaseURL := resolveAccountProviderChoice(choice)

	var apiKey, localURL, localModel string
	switch provider {
	case "anthropic", "openai", "gemini", "openrouter":
		displayProvider := provider
		if provider == "openrouter" {
			displayProvider = "openrouter"
		}
		_, _ = fmt.Fprintf(stdout, "\n[4/5] %s API key\n", displayProvider)
		_, _ = fmt.Fprint(stdout, "  키: ")
		key, err := readSecret(scanner, stdin, stdout)
		if err != nil {
			return fmt.Errorf("read api key: %w", err)
		}
		apiKey = strings.TrimSpace(key)
		if apiKey == "" {
			return fmt.Errorf("api-key required for %s", displayProvider)
		}
		if provider == "openrouter" {
			localModel = core.OpenRouterDefaultModel
			break
		}
		models := setupLLMModelChoices(provider)
		modelIdx, err := promptAccountModelChoice(scanner, stdin, stdout, models)
		if err != nil {
			return fmt.Errorf("read model: %w", err)
		}
		localModel = models[modelIdx-1]
	case "local":
		localURL = promptAccountLine(scanner, stdout, "\n[4/5] Local URL", core.OllamaDefaultBaseURL)
		localModel = promptAccountLine(scanner, stdout, "[5/5] Local model", "llama3")
	default:
		return fmt.Errorf("unsupported provider %q", provider)
	}

	if presetBaseURL == core.OpenRouterBaseURL {
		provider = "openrouter"
	}
	resolvedProvider, model, baseURL := core.ResolveLLMConfig(provider, localURL, localModel)
	if presetBaseURL != "" {
		baseURL = presetBaseURL
	}
	f.llmProvider = resolvedProvider
	f.llmAPIKey = apiKey
	f.llmModel = model
	f.llmBaseURL = baseURL
	return nil
}

func resolveAccountProviderChoice(s string) (provider, baseURL string) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "openrouter") {
		return "openrouter", core.OpenRouterBaseURL
	}
	if strings.Contains(s, "\x1b[") {
		return providerFromChoiceIndex(choiceFromInput(s, 1, len(setupLLMProviderChoices())))
	}
	switch strings.ToLower(s) {
	case "", "1", "anthropic", "claude":
		return "anthropic", ""
	case "2", "openai", "gpt":
		return "openai", ""
	case "3", "gemini", "google":
		return "gemini", ""
	case "4":
		return "openrouter", core.OpenRouterBaseURL
	case "5", "local", "ollama":
		return "local", ""
	default:
		return s, ""
	}
}

func providerFromChoiceIndex(idx int) (string, string) {
	switch idx {
	case 2:
		return "openai", ""
	case 3:
		return "gemini", ""
	case 4:
		return "openrouter", core.OpenRouterBaseURL
	case 5:
		return "local", ""
	default:
		return "anthropic", ""
	}
}

func promptAccountModelChoice(scanner *bufio.Scanner, stdin io.Reader, stdout io.Writer, models []string) (int, error) {
	_, _ = fmt.Fprintln(stdout, "\n[5/5] LLM model")
	if len(models) == 0 {
		return 0, fmt.Errorf("no model choices available")
	}
	if f, ok := stdin.(*os.File); ok && f == os.Stdin && isatty.IsTerminal(f.Fd()) {
		if out, ok := stdout.(*os.File); ok && out == os.Stdout && isatty.IsTerminal(out.Fd()) {
			if idx, ok := promptChoiceInteractive("  Model > ", models, 1); ok {
				return idx, nil
			}
		}
	}
	for i, model := range models {
		_, _ = fmt.Fprintf(stdout, "  %d) %s\n", i+1, model)
	}
	_, _ = fmt.Fprint(stdout, "  선택 [1]: ")
	return choiceFromInputForScanner(scanner, 1, len(models))
}

func choiceFromInputForScanner(scanner *bufio.Scanner, defaultIdx, optionCount int) (int, error) {
	line, err := readLine(scanner)
	if err != nil {
		return 0, err
	}
	return choiceFromInput(line, defaultIdx, optionCount), nil
}

func promptAccountLine(scanner *bufio.Scanner, stdout io.Writer, label, def string) string {
	_, _ = fmt.Fprintf(stdout, "%s [%s]: ", label, def)
	value, _ := readLine(scanner)
	value = strings.TrimSpace(value)
	if value == "" {
		return def
	}
	return value
}
