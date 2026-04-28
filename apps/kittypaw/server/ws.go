package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/engine"
)

const (
	wsIdleTimeout    = 5 * time.Minute
	wsMaxLifetime    = 30 * time.Minute
	wsMaxMessageSize = 64 * 1024
	// wsWriteTimeout was 10s; LLM streaming with retry can pause >10s
	// between chunks (Anthropic SSE retry, MoA fan-out, slow tool calls).
	// 30s gives those legitimate gaps headroom without holding a dead
	// conn for too long.
	wsWriteTimeout = 30 * time.Second
	// maxTurnIDLen caps the client-supplied turn_id at the WS layer.
	// A standard UUIDv4 is 36 chars; the slack tolerates whitespace or
	// future format extensions while bounding the cache key against a
	// malicious client trying to allocate 64KB-keyed entries.
	maxTurnIDLen = 64
	// wsHeartbeatInterval is how often the server pings the client to
	// keep the WS connection alive across long in-flight RunTurn calls
	// (Phase 11 mediation tool-use loops can span 60s+ without
	// surfacing any user-visible frames; intermediate proxies / NAT
	// boxes close idle TCP sooner). 30s is short enough to defeat the
	// stricter common idle thresholds and long enough that heartbeat
	// traffic is negligible.
	wsHeartbeatInterval = 30 * time.Second
	// wsPingTimeout caps how long a single ping waits for a pong
	// before the heartbeat declares the conn dead and exits.
	wsPingTimeout = 5 * time.Second
)

// validateTurnID checks that a client-supplied turn_id is empty
// (legacy fallback) or a UUID under maxTurnIDLen. Returns (errMsg,
// true) when the id passes; (errMsg, false) when it fails — the
// caller surfaces errMsg to the client and skips the chat handler.
func validateTurnID(id string) (string, bool) {
	if id == "" {
		return "", true
	}
	if len(id) > maxTurnIDLen {
		return "turn_id exceeds maximum length", false
	}
	if _, err := uuid.Parse(id); err != nil {
		return "turn_id must be a valid UUID", false
	}
	return "", true
}

// pinger abstracts websocket.Conn for the heartbeat loop. Lets unit
// tests substitute a fake without standing up a real WS.
type pinger interface {
	Ping(ctx context.Context) error
}

// runHeartbeat ticks every interval and pings the peer with a per-
// ping timeout. When the ping fails the peer is unresponsive; the
// loop calls onFail (typically the handler's ctx cancel so the
// session tears down promptly) and exits. Without onFail a dead
// peer would only be detected on the next app-level read/write,
// allowing in-flight RunTurn calls to keep burning model + tool
// cost for an absent client. Exits cleanly on ctx cancel as well.
func runHeartbeat(ctx context.Context, p pinger, interval, timeout time.Duration, onFail func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := p.Ping(pingCtx)
			cancel()
			if err != nil {
				slog.Info("ws heartbeat ping failed; ending session", "error", err)
				if onFail != nil {
					onFail()
				}
				return
			}
		}
	}
}

// startHeartbeat is the production entry point — wires the live
// websocket.Conn into runHeartbeat with the package-level interval
// and timeout constants. onFail is the handler's ctx cancel, which
// promotes a dead-conn detection into a session teardown.
func startHeartbeat(ctx context.Context, conn *websocket.Conn, onFail func()) {
	runHeartbeat(ctx, conn, wsHeartbeatInterval, wsPingTimeout, onFail)
}

// readPump owns conn.Read for the connection's lifetime. nhooyr's
// Conn.Ping requires a Reader to be active concurrently so pongs
// can be dispatched (without it, ping always times out and the
// heartbeat goroutine would self-terminate during the very long-
// running RunTurn calls heartbeat exists to keep alive). Type-
// dispatches client frames: WsMsgChat goes to chatCh (drives the
// main turn loop), WsMsgPermit resolves OnPermission via permCh.
// Calls cancel on exit so a read error or session-lifetime expiry
// promptly tears the whole handler down.
func readPump(ctx context.Context, conn *websocket.Conn, sessionID string,
	chatCh chan<- core.WsClientMsg, permCh chan<- bool, cancel context.CancelFunc) {
	defer cancel()
	for {
		readCtx, readCancel := context.WithTimeout(ctx, wsIdleTimeout)
		_, msgBytes, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			lifetimeExpired := ctx.Err() != nil
			slog.Info("ws session ended",
				"session_id", sessionID,
				"error", err.Error(),
				"lifetime_expired", lifetimeExpired,
			)
			if lifetimeExpired {
				sendWsMsg(ctx, conn, core.NewErrorMsg("session expired"))
			}
			return
		}

		var msg core.WsClientMsg
		if jerr := json.Unmarshal(msgBytes, &msg); jerr != nil {
			sendWsMsg(ctx, conn, core.NewErrorMsg("invalid message format"))
			continue
		}

		switch msg.Type {
		case core.WsMsgChat:
			select {
			case chatCh <- msg:
			case <-ctx.Done():
				return
			}
		case core.WsMsgPermit:
			ok := msg.OK != nil && *msg.OK
			select {
			case permCh <- ok:
			default:
				// No pending permission request; drop silently.
			}
		default:
			slog.Debug("ws: unknown client msg type", "type", msg.Type)
		}
	}
}

// handleWebSocket upgrades to WebSocket and runs a multi-turn streaming chat session.
// Auth via ?token= query param or Authorization header.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Auth — read config under RLock for reload safety.
	s.configMu.RLock()
	apiKey := s.config.Server.APIKey
	originPatterns := s.config.Server.AllowedOrigins
	s.configMu.RUnlock()

	if apiKey != "" {
		token := r.URL.Query().Get("token")
		if token == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if !fixedLenEqual(token, apiKey) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	if len(originPatterns) == 0 {
		originPatterns = []string{"*"}
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: originPatterns,
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

	// chatCh carries WsMsgChat frames from readPump to the main turn
	// loop. permCh carries WsMsgPermit responses to the engine's
	// OnPermission callback during a RunTurn.
	chatCh := make(chan core.WsClientMsg, 1)
	permCh := make(chan bool, 1)

	// readPump is the sole goroutine calling conn.Read — the nhooyr
	// Ping path requires this so pongs can be dispatched even while
	// the main loop is blocked inside RunTurn. cancel propagates a
	// read error (or session-lifetime expiry) to the rest of the
	// handler and the heartbeat.
	go readPump(ctx, conn, sessionID, chatCh, permCh, cancel)

	// Heartbeat keeps the conn alive across long in-flight RunTurn
	// calls (Phase 11 mediation can span 60+ seconds silent). Ping
	// failure cancels the handler ctx so a dead client doesn't keep
	// burning model + tool cost.
	go startHeartbeat(ctx, conn, cancel)

	for {
		select {
		case <-ctx.Done():
			return
		case clientMsg := <-chatCh:
			if clientMsg.Text == "" {
				continue
			}

			// Validate the client-supplied turn_id. UUID format +
			// length cap together keep the cache safe from oracle
			// attacks (a victim's turn_id is impossible to guess
			// under 122-bit entropy) and keep a malicious client
			// from allocating 64KB-keyed entries. Empty TurnID is
			// allowed — it falls through to the legacy
			// non-idempotent path.
			if msg, ok := validateTurnID(clientMsg.TurnID); !ok {
				sendWsMsg(ctx, conn, core.NewErrorMsg(msg))
				continue
			}

			payload, _ := json.Marshal(core.ChatPayload{
				ChatID:    sessionID,
				Text:      clientMsg.Text,
				SessionID: sessionID,
			})
			event := core.Event{
				Type:    core.EventWebChat,
				Payload: payload,
			}

			runOpts := &engine.RunOptions{
				OnToken: func(token string) {
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

			// RunTurn dedupes retries that share clientMsg.TurnID via
			// Session.turnCache. Empty TurnID falls through to plain
			// Run (legacy client without idempotency).
			result, err := s.session.RunTurn(ctx, clientMsg.TurnID, event, runOpts)
			if err != nil {
				sendWsMsg(ctx, conn, core.NewErrorMsgForTurn(clientMsg.TurnID, err.Error()))
				continue
			}
			sendWsMsg(ctx, conn, core.NewDoneMsgForTurn(clientMsg.TurnID, result, nil))
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
	if err := conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		slog.Warn("ws write failed", "type", msg.Type, "error", err.Error())
	}
}
