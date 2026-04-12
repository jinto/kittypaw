package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jinto/gopaw/core"
)

// --- Kakao relay DTOs ---

type kakaoRelayMessage struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	FromName string `json:"from_name,omitempty"`
}

type kakaoPollResponse struct {
	Messages []kakaoRelayMessage `json:"messages"`
}

// --- KakaoChannel ---

// KakaoChannel implements Channel by polling a Cloudflare Worker relay
// that bridges KakaoTalk messages. The relay provides a simple REST API:
//
//   GET  /poll   - returns pending messages
//   POST /reply  - sends a response
type KakaoChannel struct {
	relayURL   string
	userToken  string
	client     *http.Client
	lastSender string // last message sender for responses
	mu         sync.Mutex
}

// NewKakao creates a KakaoChannel that communicates via the given relay URL.
func NewKakao(relayURL, userToken string) *KakaoChannel {
	return &KakaoChannel{
		relayURL:  relayURL,
		userToken: userToken,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (k *KakaoChannel) Name() string { return "kakao_talk" }

// Start polls the relay for pending messages and emits them as events.
func (k *KakaoChannel) Start(ctx context.Context, eventCh chan<- core.Event) error {
	slog.Info("kakao: starting relay poll loop", "url", k.relayURL)

	for {
		select {
		case <-ctx.Done():
			slog.Info("kakao: shutting down")
			return ctx.Err()
		default:
		}

		messages, err := k.poll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("kakao: poll failed", "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}

		if len(messages) == 0 {
			// No messages; wait before next poll to avoid spinning.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}

		for _, msg := range messages {
			if msg.Text == "" {
				continue
			}

			k.mu.Lock()
			k.lastSender = msg.FromName
			k.mu.Unlock()

			payload := core.ChatPayload{
				ChatID:   msg.ID,
				Text:     msg.Text,
				FromName: msg.FromName,
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				slog.Error("kakao: marshal payload", "error", err)
				continue
			}

			event := core.Event{
				Type:    core.EventKakaoTalk,
				Payload: raw,
			}

			select {
			case eventCh <- event:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// SendResponse sends a reply back through the relay.
func (k *KakaoChannel) SendResponse(ctx context.Context, agentID, response string) error {
	body := map[string]string{
		"text":     response,
		"agent_id": agentID,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		k.relayURL+"/reply", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+k.userToken)

	resp, err := k.client.Do(req)
	if err != nil {
		return fmt.Errorf("kakao reply: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kakao reply: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// --- internal ---

func (k *KakaoChannel) poll(ctx context.Context) ([]kakaoRelayMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		k.relayURL+"/poll", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+k.userToken)

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("poll: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result kakaoPollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode poll: %w", err)
	}
	return result.Messages, nil
}
