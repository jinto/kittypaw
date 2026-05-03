package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jinto/kittypaw/client"
	"github.com/jinto/kittypaw/core"
)

var errTelegramPairingServerUnavailable = errors.New("telegram pairing server unavailable")

var telegramPairingLocalBaseURLs = func() []string {
	return []string{"http://127.0.0.1:3000", "http://localhost:3000"}
}

type telegramPairingStatus struct {
	Status  string
	ChatID  string
	Source  string
	Message string
}

type telegramPairingDeps struct {
	fetchChatID        func(context.Context, string) (string, error)
	serverChatID       func(context.Context, string) (telegramPairingStatus, error)
	serverPollInterval time.Duration
	maxServerPolls     int
}

var runTelegramChatIDWizard = func(scanner *bufio.Scanner, stdout io.Writer, accountID, token string) string {
	return promptTelegramChatID(scanner, stdout, token, telegramPairingDeps{
		fetchChatID:        core.FetchTelegramChatID,
		serverChatID:       defaultTelegramPairingClient(accountID),
		serverPollInterval: time.Second,
		maxServerPolls:     60,
	})
}

func promptTelegramChatID(scanner *bufio.Scanner, stdout io.Writer, token string, deps telegramPairingDeps) string {
	if stdout == nil {
		stdout = io.Discard
	}
	if deps.fetchChatID == nil {
		deps.fetchChatID = core.FetchTelegramChatID
	}
	if deps.serverChatID != nil {
		if chatID, handled := promptTelegramChatIDViaServer(scanner, stdout, token, deps); handled {
			return chatID
		}
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

func promptTelegramChatIDViaServer(scanner *bufio.Scanner, stdout io.Writer, token string, deps telegramPairingDeps) (string, bool) {
	maxPolls := deps.maxServerPolls
	if maxPolls <= 0 {
		maxPolls = 1
	}
	printedWaiting := false

	for attempt := 1; attempt <= maxPolls; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		status, err := deps.serverChatID(ctx, token)
		cancel()
		if errors.Is(err, errTelegramPairingServerUnavailable) {
			return "", false
		}
		if err != nil {
			_, _ = fmt.Fprintf(stdout, "  Local server Telegram pairing unavailable: %v\n", err)
			return promptTelegramChatIDManual(scanner, stdout), true
		}
		if status.ChatID != "" {
			_, _ = fmt.Fprintln(stdout, "  Telegram connected ✓")
			return status.ChatID, true
		}
		if status.Status == "paired" {
			return "", true
		}
		if !printedWaiting {
			printTelegramServerPairingGuideTo(stdout)
			printedWaiting = true
		}
		if deps.serverPollInterval > 0 && attempt < maxPolls {
			time.Sleep(deps.serverPollInterval)
		}
	}

	printTelegramServerPairingTimeoutTo(stdout)
	return promptTelegramChatIDManual(scanner, stdout), true
}

func promptTelegramChatIDManual(scanner *bufio.Scanner, stdout io.Writer) string {
	printTelegramChatIDHelpTo(stdout)
	_, _ = fmt.Fprint(stdout, "  Chat ID: ")
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func defaultTelegramPairingClient(accountID string) func(context.Context, string) (telegramPairingStatus, error) {
	return func(ctx context.Context, token string) (telegramPairingStatus, error) {
		conn, err := client.NewDaemonConnForAccount(flagRemote, accountID)
		if err != nil && accountID != "" {
			conn, err = client.NewDaemonConn(flagRemote)
		}
		if err != nil {
			return telegramPairingChatIDFromLocalServerIfLocal(ctx, token)
		}
		cl := client.New(conn.BaseURL, conn.APIKey)
		res, err := cl.TelegramPairingChatID(accountID, token)
		if err != nil {
			if isTelegramPairingConnectionError(err) {
				return telegramPairingChatIDFromLocalServerIfLocal(ctx, token)
			}
			return telegramPairingStatus{}, err
		}
		return telegramPairingStatusFromMap(res), nil
	}
}

type telegramPairingHTTPError struct {
	status int
	body   string
}

func (e telegramPairingHTTPError) Error() string {
	return fmt.Sprintf("server error %d: %s", e.status, e.body)
}

func telegramPairingChatIDFromLocalServerIfLocal(ctx context.Context, token string) (telegramPairingStatus, error) {
	if flagRemote != "" {
		return telegramPairingStatus{}, errTelegramPairingServerUnavailable
	}
	return telegramPairingChatIDFromLocalServer(ctx, token)
}

func telegramPairingChatIDFromLocalServer(ctx context.Context, token string) (telegramPairingStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for _, baseURL := range telegramPairingLocalBaseURLs() {
		status, err := requestTelegramPairingLocal(ctx, baseURL, token)
		if err == nil {
			return status, nil
		}
		if isTelegramPairingNoActiveLocalToken(err) {
			return telegramPairingStatus{}, errTelegramPairingServerUnavailable
		}
		lastErr = err
		if !isTelegramPairingConnectionError(err) {
			return telegramPairingStatus{}, err
		}
	}
	if lastErr != nil && !isTelegramPairingConnectionError(lastErr) {
		return telegramPairingStatus{}, lastErr
	}
	return telegramPairingStatus{}, errTelegramPairingServerUnavailable
}

func requestTelegramPairingLocal(ctx context.Context, baseURL, token string) (telegramPairingStatus, error) {
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return telegramPairingStatus{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/telegram/pairing/chat-id", bytes.NewReader(body))
	if err != nil {
		return telegramPairingStatus{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 2 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return telegramPairingStatus{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return telegramPairingStatus{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return telegramPairingStatus{}, telegramPairingHTTPError{status: resp.StatusCode, body: string(data)}
	}

	var res map[string]any
	if err := json.Unmarshal(data, &res); err != nil {
		return telegramPairingStatus{}, fmt.Errorf("parse response: %w", err)
	}
	return telegramPairingStatusFromMap(res), nil
}

func isTelegramPairingConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "request failed") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host")
}

func isTelegramPairingNoActiveLocalToken(err error) bool {
	var httpErr telegramPairingHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	return httpErr.status == http.StatusNotFound &&
		strings.Contains(httpErr.body, "no running Telegram channel is using this bot token")
}

func telegramPairingStatusFromMap(res map[string]any) telegramPairingStatus {
	status := telegramPairingStatus{}
	if v, ok := res["status"].(string); ok {
		status.Status = v
	}
	if v, ok := res["chat_id"].(string); ok {
		status.ChatID = v
	}
	if v, ok := res["source"].(string); ok {
		status.Source = v
	}
	if v, ok := res["message"].(string); ok {
		status.Message = v
	}
	return status
}

func detectTelegramChatID(accountID, token string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := defaultTelegramPairingClient(accountID)(ctx, token)
	if err == nil {
		if status.ChatID != "" {
			return status.ChatID, nil
		}
		return "", fmt.Errorf("no messages found — send a message to the bot first")
	}
	if !errors.Is(err, errTelegramPairingServerUnavailable) {
		return "", err
	}
	return core.FetchTelegramChatID(ctx, token)
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

func printTelegramServerPairingGuideTo(stdout io.Writer) {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		_, _ = fmt.Fprintln(stdout, "  텔레그램에서 봇에게 메시지를 보내면 자동으로 연결됩니다.")
	case strings.HasPrefix(lang, "ja"):
		_, _ = fmt.Fprintln(stdout, "  Telegramでボットにメッセージを送ると自動的に接続されます。")
	default:
		_, _ = fmt.Fprintln(stdout, "  Send a message to your Telegram bot; it will connect automatically.")
	}
}

func printTelegramServerPairingTimeoutTo(stdout io.Writer) {
	lang := detectLang()
	switch {
	case strings.HasPrefix(lang, "ko"):
		_, _ = fmt.Fprintln(stdout, "  자동 연결을 아직 확인하지 못했습니다.")
	case strings.HasPrefix(lang, "ja"):
		_, _ = fmt.Fprintln(stdout, "  自動接続をまだ確認できませんでした。")
	default:
		_, _ = fmt.Fprintln(stdout, "  Automatic Telegram connection was not detected yet.")
	}
}
