package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

// TelegramTokenRe matches a valid Telegram bot token format.
var TelegramTokenRe = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]{30,50}$`)

// ValidateTelegramToken checks whether the token matches the expected format.
func ValidateTelegramToken(token string) bool {
	return TelegramTokenRe.MatchString(token)
}

// FetchTelegramChatID calls the Telegram Bot API getUpdates to discover the
// chat ID from the most recent message.
func FetchTelegramChatID(ctx context.Context, token string) (string, error) {
	if !ValidateTelegramToken(token) {
		return "", fmt.Errorf("invalid token format")
	}
	url := "https://api.telegram.org/bot" + token + "/getUpdates?limit=1&timeout=0"

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			Message struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("telegram API returned an error")
	}
	if len(result.Result) == 0 {
		return "", fmt.Errorf("no messages found — send a message to the bot first")
	}

	chatID := result.Result[0].Message.Chat.ID
	return fmt.Sprintf("%d", chatID), nil
}
