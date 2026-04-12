package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jinto/gopaw/core"
	"nhooyr.io/websocket"
)

// --- Kakao relay DTOs ---

// kakaoRelayMessage is a message frame from the relay WebSocket.
// Matches the JSON the KittyPawSession DO sends: {id, text, user_id}.
type kakaoRelayMessage struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	UserID string `json:"user_id,omitempty"`
}

// kakaoReplyMessage is sent back to the relay to dispatch to Kakao callback.
type kakaoReplyMessage struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// --- KakaoChannel ---

// KakaoChannel implements Channel by maintaining a WebSocket connection to a
// Cloudflare Worker relay that bridges KakaoTalk messages.
//
// Protocol:
//
//	WS /ws/{token} — receive messages, send replies
//	Recv: {"id":"action_id","text":"utterance","user_id":"kakao_user_id"}
//	Send: {"id":"action_id","text":"response_text"}
type KakaoChannel struct {
	relayURL  string
	userToken string
	conn      *websocket.Conn
	mu        sync.Mutex
}

// NewKakao creates a KakaoChannel that connects via WebSocket to the relay.
func NewKakao(relayURL, userToken string) *KakaoChannel {
	return &KakaoChannel{
		relayURL:  relayURL,
		userToken: userToken,
	}
}

func (k *KakaoChannel) Name() string { return "kakao_talk" }

// wsURL builds the WebSocket URL from the relay HTTP URL.
func (k *KakaoChannel) wsURL() string {
	u := k.relayURL
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	return u + "/ws/" + url.PathEscape(k.userToken)
}

// Start connects to the relay via WebSocket and emits incoming messages as events.
// Reconnects automatically on connection loss.
func (k *KakaoChannel) Start(ctx context.Context, eventCh chan<- core.Event) error {
	slog.Info("kakao: connecting to relay", "url", k.relayURL)

	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			slog.Info("kakao: shutting down")
			return ctx.Err()
		default:
		}

		connStart := time.Now()
		err := k.connectAndListen(ctx, eventCh)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Reset backoff only if the connection was alive long enough to be useful.
		if time.Since(connStart) > 30*time.Second {
			backoff = time.Second
		}

		slog.Warn("kakao: connection lost, reconnecting", "error", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

// connectAndListen establishes a WebSocket connection and reads messages until
// the connection drops or context is cancelled.
func (k *KakaoChannel) connectAndListen(ctx context.Context, eventCh chan<- core.Event) error {
	conn, _, err := websocket.Dial(ctx, k.wsURL(), nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20) // 1 MiB max frame

	k.mu.Lock()
	k.conn = conn
	k.mu.Unlock()

	slog.Info("kakao: connected to relay")

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			k.mu.Lock()
			k.conn = nil
			k.mu.Unlock()
			return fmt.Errorf("read: %w", err)
		}

		var msg kakaoRelayMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Warn("kakao: malformed frame", "error", err)
			continue
		}

		if msg.Text == "" {
			continue
		}

		payload := core.ChatPayload{
			ChatID:    msg.ID,
			Text:      msg.Text,
			SessionID: msg.UserID,
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

// SendResponse sends a reply frame through the WebSocket connection.
// The relay's Durable Object matches the ID to the pending Kakao callback.
func (k *KakaoChannel) SendResponse(ctx context.Context, actionID, response string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.conn == nil {
		return fmt.Errorf("kakao: not connected to relay")
	}

	reply := kakaoReplyMessage{
		ID:   actionID,
		Text: response,
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return err
	}

	if err := k.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("kakao reply: %w", err)
	}
	return nil
}
