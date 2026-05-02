package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"
	"github.com/kittypaw-app/kittykakao/internal/relay"
)

const (
	wsReadLimit    = 1 << 20
	wsWriteTimeout = 30 * time.Second
)

func (s *State) handleWS(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	exists, err := s.Store.TokenExists(r.Context(), token)
	if err != nil {
		slog.Warn("websocket token check failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	conn.SetReadLimit(wsReadLimit)

	session := newWSSession()
	if old := s.setSession(token, session); old != nil {
		old.Close()
	}
	slog.Info("websocket connected", "token", token, "sessions", s.sessionCount())

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer conn.CloseNow()
	defer func() {
		session.Close()
		s.removeSession(token, session)
		slog.Info("websocket disconnected", "token", token, "sessions", s.sessionCount())
	}()

	errCh := make(chan error, 2)
	go s.wsWriter(ctx, conn, session, errCh)
	go s.wsReader(ctx, conn, errCh)

	<-errCh
	cancel()
	session.Close()
	conn.CloseNow()
}

func (s *State) wsWriter(ctx context.Context, conn *websocket.Conn, session *wsSession, errCh chan<- error) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		case <-session.done:
			errCh <- errSessionClosed
			return
		case frame := <-session.send:
			writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := wsjson.Write(writeCtx, conn, frame)
			cancel()
			if err != nil {
				errCh <- err
				return
			}
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				errCh <- err
				return
			}
		}
	}
}

func (s *State) wsReader(ctx context.Context, conn *websocket.Conn, errCh chan<- error) {
	for {
		var incoming relay.WSIncoming
		if err := wsjson.Read(ctx, conn, &incoming); err != nil {
			errCh <- err
			return
		}
		if incoming.ID == "" || incoming.Text == "" {
			slog.Warn("websocket frame missing id or text")
			continue
		}
		s.dispatchCallback(incoming)
	}
}

func (s *State) dispatchCallback(incoming relay.WSIncoming) {
	ctx := context.Background()
	pending, ok, err := s.Store.TakePending(ctx, incoming.ID)
	if err != nil {
		slog.Warn("take pending callback failed", "err", err)
		return
	}
	if !ok {
		slog.Warn("pending callback missing", "action_id", incoming.ID)
		return
	}
	if !IsAllowedCallbackHost(pending.CallbackURL) {
		slog.Warn("pending callback URL blocked by SSRF guard", "url", pending.CallbackURL)
		return
	}

	var body any
	if incoming.ImageURL != "" && isPublicHTTPSImageURL(incoming.ImageURL) {
		alt := incoming.ImageAlt
		if alt == "" {
			alt = "image"
		}
		body = relay.Image(incoming.ImageURL, alt)
	} else {
		body = relay.Text(incoming.Text)
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := json.Marshal(body)
	if err != nil {
		slog.Warn("marshal callback body failed", "err", err)
		return
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pending.CallbackURL, bytes.NewReader(raw))
	if err != nil {
		slog.Warn("create callback request failed", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		slog.Warn("callback dispatch failed", "err", err)
		return
	}
	_ = resp.Body.Close()
}
