package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jinto/kittypaw/core"
)

type telegramPairingDeps struct {
	fetchChatID func(context.Context, string) (string, error)
}

var runTelegramChatIDWizard = func(scanner *bufio.Scanner, stdout io.Writer, token string) string {
	return promptTelegramChatID(scanner, stdout, token, telegramPairingDeps{
		fetchChatID: core.FetchTelegramChatID,
	})
}

func promptTelegramChatID(scanner *bufio.Scanner, stdout io.Writer, token string, deps telegramPairingDeps) string {
	if stdout == nil {
		stdout = io.Discard
	}
	if deps.fetchChatID == nil {
		deps.fetchChatID = core.FetchTelegramChatID
	}

	printTelegramGuideTo(stdout)
	_, _ = fmt.Fprint(stdout, "  > ")
	_ = scanner.Scan()

	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		_, _ = fmt.Fprint(stdout, "  Chat ID auto-detect... ")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		chatID, err := deps.fetchChatID(ctx, token)
		cancel()
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "%s ✓\n", chatID)
			return chatID
		}

		if attempt < maxRetries {
			printTelegramRetryHintTo(stdout, attempt)
			_, _ = fmt.Fprint(stdout, "  > ")
			_ = scanner.Scan()
			continue
		}

		_, _ = fmt.Fprintln(stdout)
		printTelegramChatIDHelpTo(stdout)
		_, _ = fmt.Fprint(stdout, "  Chat ID: ")
		if scanner.Scan() {
			return strings.TrimSpace(scanner.Text())
		}
		return ""
	}
	return ""
}

func printTelegramGuideTo(stdout io.Writer) {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		_, _ = fmt.Fprintln(stdout, "  텔레그램에서 봇에게 /start 를 보내고 Enter를 누르세요.")
	case strings.HasPrefix(lang, "ja"):
		_, _ = fmt.Fprintln(stdout, "  Telegramでボットに /start を送信してEnterを押してください。")
	default:
		_, _ = fmt.Fprintln(stdout, "  Send /start to your bot in Telegram, then press Enter.")
	}
}

func printTelegramRetryHintTo(stdout io.Writer, attempt int) {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		_, _ = fmt.Fprintf(stdout, "아직 메시지를 찾지 못했습니다 (%d/3).\n", attempt)
		_, _ = fmt.Fprintln(stdout, "  텔레그램에서 봇에게 메시지를 보낸 뒤 Enter를 누르세요.")
	case strings.HasPrefix(lang, "ja"):
		_, _ = fmt.Fprintf(stdout, "まだメッセージが見つかりません (%d/3)。\n", attempt)
		_, _ = fmt.Fprintln(stdout, "  Telegramでボットにメッセージを送ってからEnterを押してください。")
	default:
		_, _ = fmt.Fprintf(stdout, "No message found yet (%d/3).\n", attempt)
		_, _ = fmt.Fprintln(stdout, "  Send any message to the bot in Telegram, then press Enter.")
	}
}

func printTelegramChatIDHelpTo(stdout io.Writer) {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		_, _ = fmt.Fprintln(stdout, "  자동 감지에 실패했습니다. Chat ID를 직접 입력해주세요.")
		_, _ = fmt.Fprintln(stdout, "  텔레그램에서 @userinfobot 에게 메시지를 보내면 Chat ID를 알 수 있습니다.")
	case strings.HasPrefix(lang, "ja"):
		_, _ = fmt.Fprintln(stdout, "  自動検出に失敗しました。Chat IDを手動で入力してください。")
		_, _ = fmt.Fprintln(stdout, "  Telegramで @userinfobot にメッセージを送るとChat IDを確認できます。")
	default:
		_, _ = fmt.Fprintln(stdout, "  Auto-detect failed. Please enter your Chat ID manually.")
		_, _ = fmt.Fprintln(stdout, "  Message @userinfobot in Telegram to find your Chat ID.")
	}
}
