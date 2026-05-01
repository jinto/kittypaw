package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kittypaw-app/kittychat/internal/protocol"
)

var (
	ErrDeviceOffline = errors.New("device offline")
	ErrForbidden     = errors.New("forbidden")
	ErrBackpressure  = errors.New("too many in-flight requests")
)

type Config struct {
	RequestTimeout       time.Duration
	MaxInflightPerDevice int
}

type DevicePrincipal struct {
	UserID          string
	DeviceID        string
	LocalAccountIDs []string
}

type DeviceConn interface {
	Send(ctx context.Context, frame protocol.Frame) error
	Close() error
}

type Request struct {
	UserID    string
	DeviceID  string
	AccountID string
	Method    string
	Path      string
	Body      []byte
}

type Broker struct {
	mu      sync.Mutex
	cfg     Config
	devices map[string]*deviceState
}

type deviceState struct {
	principal DevicePrincipal
	accounts  map[string]struct{}
	conn      DeviceConn
	pending   map[string]chan protocol.Frame
}

func New(cfg Config) *Broker {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 120 * time.Second
	}
	if cfg.MaxInflightPerDevice <= 0 {
		cfg.MaxInflightPerDevice = 16
	}
	return &Broker{
		cfg:     cfg,
		devices: make(map[string]*deviceState),
	}
}

func (b *Broker) Register(ctx context.Context, principal DevicePrincipal, conn DeviceConn) error {
	if principal.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	if principal.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if len(principal.LocalAccountIDs) == 0 {
		return fmt.Errorf("at least one local account is required")
	}
	accounts := make(map[string]struct{}, len(principal.LocalAccountIDs))
	for _, accountID := range principal.LocalAccountIDs {
		if accountID == "" {
			return fmt.Errorf("local account id is required")
		}
		accounts[accountID] = struct{}{}
	}
	if conn == nil {
		return fmt.Errorf("device connection is required")
	}

	b.mu.Lock()
	old := b.devices[principal.DeviceID]
	b.devices[principal.DeviceID] = &deviceState{
		principal: principal,
		accounts:  accounts,
		conn:      conn,
		pending:   make(map[string]chan protocol.Frame),
	}
	b.mu.Unlock()

	if old != nil {
		_ = old.conn.Close()
		for id, ch := range old.pending {
			ch <- protocol.Frame{Type: protocol.FrameError, ID: id, Code: "replaced", Message: "device connection replaced"}
			close(ch)
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (b *Broker) Unregister(deviceID string) {
	b.mu.Lock()
	state := b.devices[deviceID]
	delete(b.devices, deviceID)
	b.mu.Unlock()
	if state == nil {
		return
	}
	_ = state.conn.Close()
	for id, ch := range state.pending {
		ch <- protocol.Frame{Type: protocol.FrameError, ID: id, Code: "offline", Message: "device offline"}
		close(ch)
	}
}

func (b *Broker) IsOnline(deviceID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.devices[deviceID]
	return ok
}

func (b *Broker) Request(ctx context.Context, req Request) (<-chan protocol.Frame, error) {
	frame := protocol.Frame{
		Type:      protocol.FrameRequest,
		ID:        "req_" + uuid.NewString(),
		AccountID: req.AccountID,
		Method:    req.Method,
		Path:      req.Path,
		Body:      json.RawMessage(req.Body),
	}
	if err := frame.Validate(); err != nil {
		return nil, err
	}

	b.mu.Lock()
	state := b.devices[req.DeviceID]
	if state == nil {
		b.mu.Unlock()
		return nil, ErrDeviceOffline
	}
	if state.principal.UserID != req.UserID {
		b.mu.Unlock()
		return nil, ErrForbidden
	}
	if _, ok := state.accounts[req.AccountID]; !ok {
		b.mu.Unlock()
		return nil, ErrForbidden
	}
	if len(state.pending) >= b.cfg.MaxInflightPerDevice {
		b.mu.Unlock()
		return nil, ErrBackpressure
	}

	stream := make(chan protocol.Frame, 16)
	state.pending[frame.ID] = stream
	conn := state.conn
	timeout := b.cfg.RequestTimeout
	b.mu.Unlock()

	if err := conn.Send(ctx, frame); err != nil {
		b.finish(req.DeviceID, frame.ID, protocol.Frame{
			Type:    protocol.FrameError,
			ID:      frame.ID,
			Code:    "send_failed",
			Message: err.Error(),
		})
		return nil, err
	}

	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			b.finish(req.DeviceID, frame.ID, protocol.Frame{
				Type:    protocol.FrameError,
				ID:      frame.ID,
				Code:    "canceled",
				Message: ctx.Err().Error(),
			})
		case <-timer.C:
			b.finish(req.DeviceID, frame.ID, protocol.Frame{
				Type:    protocol.FrameError,
				ID:      frame.ID,
				Code:    "timeout",
				Message: "request timed out",
			})
		}
	}()

	return stream, nil
}

func (b *Broker) Deliver(deviceID string, frame protocol.Frame) {
	if frame.Type == protocol.FrameResponseEnd || frame.Type == protocol.FrameError {
		b.finish(deviceID, frame.ID, frame)
		return
	}

	b.mu.Lock()
	state := b.devices[deviceID]
	var ch chan protocol.Frame
	if state != nil {
		ch = state.pending[frame.ID]
	}
	b.mu.Unlock()

	if ch == nil {
		return
	}
	ch <- frame
}

func (b *Broker) finish(deviceID, requestID string, frame protocol.Frame) {
	b.mu.Lock()
	state := b.devices[deviceID]
	var ch chan protocol.Frame
	if state != nil {
		ch = state.pending[requestID]
		delete(state.pending, requestID)
	}
	b.mu.Unlock()

	if ch == nil {
		return
	}
	ch <- frame
	close(ch)
}
