package daemonws

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"
	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/protocol"
)

const (
	readTimeout  = 5 * time.Minute
	writeTimeout = 30 * time.Second
)

var ErrUnauthorized = errors.New("unauthorized")

type DeviceAuthenticator interface {
	Authenticate(r *http.Request) (broker.DevicePrincipal, error)
}

type Broker interface {
	Register(ctx context.Context, principal broker.DevicePrincipal, conn broker.DeviceConn) error
	Deliver(deviceID string, frame protocol.Frame)
	Unregister(deviceID string)
}

type Handler struct {
	auth   DeviceAuthenticator
	broker Broker
}

func NewHandler(auth DeviceAuthenticator, b Broker) *Handler {
	return &Handler{auth: auth, broker: b}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/connect", h.handleConnect)
	return r
}

func (h *Handler) handleConnect(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || h.broker == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	principal, err := h.auth.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)

	ctx := r.Context()
	var hello protocol.Frame
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = wsjson.Read(readCtx, conn, &hello)
	cancel()
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "hello required")
		return
	}
	if err := hello.Validate(); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	if err := ensureHelloMatchesPrincipal(hello, principal); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	wsConn := &deviceConn{conn: conn}
	if err := h.broker.Register(ctx, principal, wsConn); err != nil {
		_ = conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	defer h.broker.Unregister(principal.DeviceID)
	defer func() { _ = conn.CloseNow() }()

	for {
		var frame protocol.Frame
		readCtx, cancel := context.WithTimeout(ctx, readTimeout)
		err := wsjson.Read(readCtx, conn, &frame)
		cancel()
		if err != nil {
			return
		}
		if err := frame.Validate(); err != nil {
			_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
			return
		}
		h.broker.Deliver(principal.DeviceID, frame)
	}
}

func ensureHelloMatchesPrincipal(hello protocol.Frame, principal broker.DevicePrincipal) error {
	if hello.DeviceID != principal.DeviceID {
		return fmt.Errorf("hello device_id does not match credential")
	}
	allowed := make(map[string]struct{}, len(principal.LocalAccountIDs))
	for _, accountID := range principal.LocalAccountIDs {
		allowed[accountID] = struct{}{}
	}
	for _, accountID := range hello.LocalAccounts {
		if _, ok := allowed[accountID]; !ok {
			return fmt.Errorf("hello local account does not match credential")
		}
	}
	return nil
}

type deviceConn struct {
	conn *websocket.Conn
}

func (c *deviceConn) Send(ctx context.Context, frame protocol.Frame) error {
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(writeCtx, c.conn, frame)
}

func (c *deviceConn) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "device disconnected")
}

type StaticTokenAuthenticator struct {
	Token     string
	Principal broker.DevicePrincipal
}

func (a StaticTokenAuthenticator) Authenticate(r *http.Request) (broker.DevicePrincipal, error) {
	token := bearerToken(r)
	if token == "" || a.Token == "" || token != a.Token {
		return broker.DevicePrincipal{}, ErrUnauthorized
	}
	return a.Principal, nil
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if key := r.Header.Get("x-device-token"); key != "" {
		return key
	}
	return ""
}
