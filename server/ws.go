package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/engine"
	"nhooyr.io/websocket"
)

const (
	wsIdleTimeout    = 5 * time.Minute
	wsMaxLifetime    = 30 * time.Minute
	wsMaxMessageSize = 64 * 1024
	wsWriteTimeout   = 10 * time.Second
)

// handleWebSocket upgrades to WebSocket and runs a multi-turn streaming chat session.
// Auth via ?token= query param or Authorization header.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Auth
	if s.config.Server.APIKey != "" {
		token := r.URL.Query().Get("token")
		if token == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if !fixedLenEqual(token, s.config.Server.APIKey) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		slog.Error("ws upgrade failed", "error", err)
		return
	}
	conn.SetReadLimit(wsMaxMessageSize)
	defer conn.CloseNow()

	sessionID := uuid.New().String()
	slog.Info("ws session started", "session_id", sessionID)

	ctx, cancel := context.WithTimeout(r.Context(), wsMaxLifetime)
	defer cancel()

	// Send session ID.
	sendWsMsg(ctx, conn, core.NewSessionMsg(sessionID))

	// permCh carries permission responses from the client back to the engine
	// when the agent requests approval for a destructive action.
	permCh := make(chan bool, 1)

	// Multi-turn loop
	var mu sync.Mutex
	for {
		readCtx, readCancel := context.WithTimeout(ctx, wsIdleTimeout)
		_, msgBytes, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			if ctx.Err() != nil {
				sendWsMsg(ctx, conn, core.NewErrorMsg("session expired"))
			}
			return
		}

		var clientMsg core.WsClientMsg
		if err := json.Unmarshal(msgBytes, &clientMsg); err != nil {
			sendWsMsg(ctx, conn, core.NewErrorMsg("invalid message format"))
			continue
		}

		switch clientMsg.Type {
		case core.WsMsgChat:
			if clientMsg.Text == "" {
				continue
			}

			// Build event
			payload, _ := json.Marshal(core.ChatPayload{
				ChatID:    sessionID,
				Text:      clientMsg.Text,
				SessionID: sessionID,
			})
			event := core.Event{
				Type:    core.EventWebChat,
				Payload: payload,
			}

			// Accumulate full text from streaming tokens.
			var fullText strings.Builder

			// Build per-call options with streaming and permission callbacks.
			runOpts := &engine.RunOptions{
				OnToken: func(token string) {
					mu.Lock()
					fullText.WriteString(token)
					mu.Unlock()
					sendWsMsg(ctx, conn, core.NewTokenMsg(token))
				},
				OnPermission: func(pCtx context.Context, description, resource string) (bool, error) {
					sendWsMsg(pCtx, conn, core.NewPermissionMsg(description, resource))
					select {
					case ok := <-permCh:
						return ok, nil
					case <-pCtx.Done():
						return false, pCtx.Err()
					case <-time.After(2 * time.Minute):
						return false, fmt.Errorf("permission timeout")
					}
				},
			}

			result, err := s.session.Run(ctx, event, runOpts)

			if err != nil {
				sendWsMsg(ctx, conn, core.NewErrorMsg(err.Error()))
				continue
			}

			// Prefer streamed text; fall back to engine output.
			mu.Lock()
			ft := fullText.String()
			mu.Unlock()
			if ft == "" {
				ft = result
			}
			sendWsMsg(ctx, conn, core.NewDoneMsg(ft, nil))

		case core.WsMsgPermit:
			ok := clientMsg.OK != nil && *clientMsg.OK
			select {
			case permCh <- ok:
			default:
				// No pending permission request; drop silently.
			}

		default:
			slog.Debug("ws: unknown client msg type", "type", clientMsg.Type)
		}
	}
}

func sendWsMsg(ctx context.Context, conn *websocket.Conn, msg core.WsServerMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	_ = conn.Write(writeCtx, websocket.MessageText, data)
}
