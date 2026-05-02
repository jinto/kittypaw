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
// info to proceed unattended: no Telegram token source AND no LLM key (and the
// account is not a shared coordinator). In that state we'd otherwise reject
// the command — the interactive prompt is a friendlier path.
func needsAccountPrompt(f *accountAddFlags) bool {
	if f.isShared {
		return false
	}
	hasTokenSource := f.telegramToken != "" || f.telegramTokenStdin || os.Getenv(accountEnvBotToken) != ""
	hasLLM := f.llmAPIKey != "" || f.llmProvider == "local"
	return !hasTokenSource && !hasLLM
}

// promptAccountSetup walks the user through 4 minimal questions and writes
// the answers back into f. Mutates f directly so the existing runAccountAdd
// flow can pick up the values without further branching.
//
// Steps: telegram token → LLM provider → LLM api-key (skipped for local) → LLM model.
// Admin chat ID is left for runAccountAdd's auto-detect (FetchTelegramChatID).
//
// Secrets (telegram token, api-key) are read with terminal echo disabled when
// stdin is a TTY — keeps shoulder-surfers and scrollback buffers from picking
// up the value. Tests use a non-TTY io.Reader and exercise the scanner path.
func promptAccountSetup(stdin io.Reader, stdout io.Writer, f *accountAddFlags) error {
	scanner := bufio.NewScanner(stdin)

	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprintln(stdout, "  새 account 설정 — 4 단계 질문")
	_, _ = fmt.Fprintln(stdout)

	// [1/4] Telegram bot token (secret — echo off in TTY)
	_, _ = fmt.Fprintln(stdout, "[1/4] Telegram 봇 토큰 (BotFather 의 /newbot 결과)")
	_, _ = fmt.Fprint(stdout, "  토큰: ")
	token, err := readSecret(scanner, stdin, stdout)
	if err != nil {
		return fmt.Errorf("read telegram token: %w", err)
	}
	f.telegramToken = strings.TrimSpace(token)
	if f.telegramToken == "" {
		return fmt.Errorf("telegram token required")
	}

	// [2/4] LLM provider
	_, _ = fmt.Fprintln(stdout, "\n[2/4] LLM provider")
	_, _ = fmt.Fprintln(stdout, "  1) anthropic   2) openai   3) local (Ollama / LM Studio)")
	_, _ = fmt.Fprint(stdout, "  선택 [1]: ")
	choice, err := readLine(scanner)
	if err != nil {
		return fmt.Errorf("read provider: %w", err)
	}
	f.llmProvider = mapProviderChoice(strings.TrimSpace(choice))

	// [3/4] LLM api-key (skip for local) — secret, echo off in TTY
	if f.llmProvider != "local" {
		_, _ = fmt.Fprintf(stdout, "\n[3/4] %s API key\n", f.llmProvider)
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

	// [4/4] LLM model — show default + accept Enter
	defaultModel := defaultModelFor(f.llmProvider)
	_, _ = fmt.Fprintf(stdout, "\n[4/4] LLM model [%s]: ", defaultModel)
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

// readLine returns the next scanner line (without trailing newline). EOF or
// scan error returns an empty string with the scanner error (or nil for
// clean EOF) — callers decide whether the empty string is acceptable.
func readLine(scanner *bufio.Scanner) (string, error) {
	if !scanner.Scan() {
		return "", scanner.Err()
	}
	return scanner.Text(), nil
}

// readSecret reads a secret with terminal echo disabled when stdin is a real
// TTY (*os.File whose fd is a terminal). Falls back to readLine for any other
// reader — primarily tests with strings.NewReader. The fallback intentionally
// echoes; production never hits it because os.Stdin is a *os.File.
func readSecret(scanner *bufio.Scanner, stdin io.Reader, stdout io.Writer) (string, error) {
	f, ok := stdin.(*os.File)
	if !ok || !isatty.IsTerminal(f.Fd()) {
		return readLine(scanner)
	}
	b, err := term.ReadPassword(int(f.Fd()))
	// term.ReadPassword swallows the user's Enter without printing a newline,
	// so the next prompt would otherwise sit on the same line as the masked
	// echo. Restore the cursor explicitly.
	_, _ = fmt.Fprintln(stdout)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mapProviderChoice resolves the numeric menu pick to a provider name.
// Empty / "1" → anthropic (default). A free-form input passes through so a
// caller can specify e.g. "openrouter" without the prompt knowing about it.
func mapProviderChoice(s string) string {
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
