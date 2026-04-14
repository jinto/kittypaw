package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jinto/gopaw/core"
	"nhooyr.io/websocket"
)

// ChatOptions configures callbacks for streaming chat.
type ChatOptions struct {
	OnToken func(token string)
	OnDone  func(fullText string, tokensUsed *int64)
	OnError func(message string)
}

// ChatSession wraps a persistent WebSocket connection for multi-turn chat.
// Use DialChat to create, Send to exchange messages, Close when done.
type ChatSession struct {
	conn *websocket.Conn
	ctx  context.Context
}

// DialChat opens a WebSocket connection and returns a ChatSession.
// The connection stays open for multiple Send calls until Close.
func DialChat(ctx context.Context, wsURL, apiKey string) (*ChatSession, error) {
	headers := http.Header{}
	if apiKey != "" {
		headers.Set("Authorization", "Bearer "+apiKey)
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	conn.SetReadLimit(64 * 1024)

	// Read session message (ignore).
	if _, _, err := conn.Read(ctx); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("read session msg: %w", err)
	}

	return &ChatSession{conn: conn, ctx: ctx}, nil
}

// Send sends a chat message and streams the response via callbacks.
// Blocks until the server sends a "done" or "error" message.
func (cs *ChatSession) Send(text string, opts ChatOptions) error {
	chatMsg := core.WsClientMsg{Type: core.WsMsgChat, Text: text}
	data, err := json.Marshal(chatMsg)
	if err != nil {
		return fmt.Errorf("marshal chat msg: %w", err)
	}
	if err := cs.conn.Write(cs.ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write chat msg: %w", err)
	}

	// Read streaming response with per-read timeout.
	for {
		readCtx, readCancel := context.WithTimeout(cs.ctx, 5*time.Minute)
		_, msgBytes, err := cs.conn.Read(readCtx)
		readCancel()
		if err != nil {
			return fmt.Errorf("read ws msg: %w", err)
		}

		var msg core.WsServerMsg
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case core.WsMsgToken:
			if opts.OnToken != nil {
				opts.OnToken(msg.Text)
			}
		case core.WsMsgDone:
			if opts.OnDone != nil {
				opts.OnDone(msg.FullText, msg.TokensUsed)
			}
			return nil
		case core.WsMsgError:
			errMsg := msg.Message
			if opts.OnError != nil {
				opts.OnError(errMsg)
			}
			return fmt.Errorf("server error: %s", errMsg)
		case core.WsMsgPermission:
			deny := false
			permitMsg := core.WsClientMsg{Type: core.WsMsgPermit, OK: &deny}
			d, _ := json.Marshal(permitMsg)
			if err := cs.conn.Write(cs.ctx, websocket.MessageText, d); err != nil {
				return fmt.Errorf("write permit deny: %w", err)
			}
		}
	}
}

// Close cleanly closes the WebSocket connection.
func (cs *ChatSession) Close() {
	cs.conn.Close(websocket.StatusNormalClosure, "bye")
}

// StreamChat opens a single-turn WebSocket session: dials, sends one message,
// reads the response, and closes. For multi-turn chat, use DialChat + Send.
func StreamChat(ctx context.Context, wsURL, apiKey, text string, opts ChatOptions) error {
	cs, err := DialChat(ctx, wsURL, apiKey)
	if err != nil {
		return err
	}
	defer cs.Close()
	return cs.Send(text, opts)
}
