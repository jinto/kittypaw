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
// chat ID from the most recent message, and ACKs every update it saw so the
// daemon that takes over afterwards does not inherit a backlog and replay
// every /start as a fresh conversation.
func FetchTelegramChatID(ctx context.Context, token string) (string, error) {
	if !ValidateTelegramToken(token) {
		return "", fmt.Errorf("invalid token format")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Pull every pending update — we need all update_ids in order to ACK
	// them collectively via the follow-up getUpdates call below.
	url := "https://api.telegram.org/bot" + token + "/getUpdates?timeout=0"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return "", err
	}

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  struct {
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

	last := result.Result[len(result.Result)-1]

	// ACK: a second getUpdates with offset=last.UpdateID+1 confirms every
	// update up to `last` and drops them from Telegram's server-side queue.
	// Best-effort — if it fails the chat_id is still valid, but the user
	// may see replayed messages when the daemon starts.
	ackURL := fmt.Sprintf(
		"https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=0",
		token, last.UpdateID+1,
	)
	if ackReq, err := http.NewRequestWithContext(ctx, http.MethodGet, ackURL, nil); err == nil {
		if ackResp, err := http.DefaultClient.Do(ackReq); err == nil {
			ackResp.Body.Close()
		}
	}

	return fmt.Sprintf("%d", last.Message.Chat.ID), nil
}
