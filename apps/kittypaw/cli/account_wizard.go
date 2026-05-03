package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

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
func promptAccountSetup(stdin io.Reader, stdout io.Writer, f *accountAddFlags) error {
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
		if f.telegramToken == "" {
			return fmt.Errorf("telegram token required")
		}
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

	// [3/5] LLM provider
	_, _ = fmt.Fprintln(stdout, "\n[3/5] LLM provider")
	choice, err := promptAccountProviderChoice(scanner, stdin, stdout)
	if err != nil {
		return fmt.Errorf("read provider: %w", err)
	}
	f.llmProvider = mapProviderChoice(choice)

	// [4/5] LLM api-key (skip for local) — secret, masked in TTY
	if f.llmProvider != "local" {
		_, _ = fmt.Fprintf(stdout, "\n[4/5] %s API key\n", f.llmProvider)
		_, _ = fmt.Fprint(stdout, "  키: ")
		key, err := readSecret(scanner, stdin, stdout)
		if err != nil {
			return fmt.Errorf("read api key: %w", err)
		}
		f.llmAPIKey = strings.TrimSpace(key)
		if f.llmAPIKey == "" {
			return fmt.Errorf("api-key required for %s", f.llmProvider)
		}
	}

	// [5/5] LLM model — show default + accept Enter
	defaultModel := defaultModelFor(f.llmProvider)
	_, _ = fmt.Fprintf(stdout, "\n[5/5] LLM model [%s]: ", defaultModel)
	model, err := readLine(scanner)
	if err != nil {
		return fmt.Errorf("read model: %w", err)
	}
	f.llmModel = strings.TrimSpace(model)
	if f.llmModel == "" {
		f.llmModel = defaultModel
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
	options := []string{"anthropic", "openai", "local (Ollama / LM Studio)"}
	if f, ok := stdin.(*os.File); ok && f == os.Stdin && isatty.IsTerminal(f.Fd()) {
		if out, ok := stdout.(*os.File); ok && out == os.Stdout && isatty.IsTerminal(out.Fd()) {
			if idx, ok := promptChoiceInteractive("  ", options, 1); ok {
				return fmt.Sprintf("%d", idx), nil
			}
		}
	}

	_, _ = fmt.Fprintln(stdout, "  1) anthropic   2) openai   3) local (Ollama / LM Studio)")
	_, _ = fmt.Fprint(stdout, "  선택 [1]: ")
	return readLine(scanner)
}

// mapProviderChoice resolves the numeric menu pick to a provider name.
// Empty / "1" → anthropic (default). A free-form input passes through so a
// caller can specify e.g. "openrouter" without the prompt knowing about it.
func mapProviderChoice(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "\x1b[") {
		return providerFromChoiceIndex(choiceFromInput(s, 1, 3))
	}
	switch s {
	case "", "1":
		return "anthropic"
	case "2":
		return "openai"
	case "3":
		return "local"
	default:
		return s
	}
}

func providerFromChoiceIndex(idx int) string {
	switch idx {
	case 2:
		return "openai"
	case 3:
		return "local"
	default:
		return "anthropic"
	}
}

// defaultModelFor returns the default model name shown in the prompt for the
// given provider. These mirror the defaults in the main setup wizard so a
// user who picks "Enter" gets a working setup. Update both locations when
// upstream defaults shift.
func defaultModelFor(provider string) string {
	switch provider {
	case "openai":
		return "gpt-4o"
	case "local":
		return "llama3"
	default:
		return "claude-sonnet-4-5"
	}
}
