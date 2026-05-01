package broker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kittypaw-app/kittychat/internal/protocol"
)

type fakeConn struct {
	requests chan protocol.Frame
	closed   chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		requests: make(chan protocol.Frame, 8),
		closed:   make(chan struct{}),
	}
}

func (c *fakeConn) Send(ctx context.Context, frame protocol.Frame) error {
	select {
	case c.requests <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *fakeConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func TestBrokerRequestForwardsToRegisteredDevice(t *testing.T) {
	b := New(Config{RequestTimeout: time.Second, MaxInflightPerDevice: 2})
	conn := newFakeConn()
	principal := DevicePrincipal{
		UserID:          "user_1",
		DeviceID:        "dev_1",
		LocalAccountIDs: []string{"alice"},
	}
	if err := b.Register(context.Background(), principal, conn); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	stream, err := b.Request(context.Background(), Request{
		UserID:    "user_1",
		DeviceID:  "dev_1",
		AccountID: "alice",
		Method:    "GET",
		Path:      "/v1/models",
	})
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}

	var sent protocol.Frame
	select {
	case sent = <-conn.requests:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request frame")
	}
	if sent.Type != protocol.FrameRequest || sent.ID == "" || sent.Path != "/v1/models" {
		t.Fatalf("sent frame = %+v", sent)
	}

	b.Deliver("dev_1", protocol.Frame{Type: protocol.FrameResponseEnd, ID: sent.ID})
	select {
	case got := <-stream:
		if got.Type != protocol.FrameResponseEnd || got.ID != sent.ID {
			t.Fatalf("stream frame = %+v, want response_end for %s", got, sent.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response frame")
	}
}

func TestBrokerOfflineDeviceReturnsError(t *testing.T) {
	b := New(Config{RequestTimeout: time.Second, MaxInflightPerDevice: 2})
	_, err := b.Request(context.Background(), Request{
		UserID:    "user_1",
		DeviceID:  "missing",
		AccountID: "alice",
		Method:    "GET",
		Path:      "/health",
	})
	if !errors.Is(err, ErrDeviceOffline) {
		t.Fatalf("Request() error = %v, want ErrDeviceOffline", err)
	}
}

func TestBrokerRejectsWrongUserAndAccount(t *testing.T) {
	b := New(Config{RequestTimeout: time.Second, MaxInflightPerDevice: 2})
	conn := newFakeConn()
	if err := b.Register(context.Background(), DevicePrincipal{
		UserID:          "user_1",
		DeviceID:        "dev_1",
		LocalAccountIDs: []string{"alice"},
	}, conn); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := b.Request(context.Background(), Request{
		UserID:    "user_2",
		DeviceID:  "dev_1",
		AccountID: "alice",
		Method:    "GET",
		Path:      "/health",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong user error = %v, want ErrForbidden", err)
	}

	_, err = b.Request(context.Background(), Request{
		UserID:    "user_1",
		DeviceID:  "dev_1",
		AccountID: "bob",
		Method:    "GET",
		Path:      "/health",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong account error = %v, want ErrForbidden", err)
	}
}

func TestBrokerDuplicateConnectionClosesOldConnection(t *testing.T) {
	b := New(Config{RequestTimeout: time.Second, MaxInflightPerDevice: 2})
	oldConn := newFakeConn()
	newConn := newFakeConn()
	principal := DevicePrincipal{UserID: "user_1", DeviceID: "dev_1", LocalAccountIDs: []string{"alice"}}
	if err := b.Register(context.Background(), principal, oldConn); err != nil {
		t.Fatalf("register old: %v", err)
	}
	if err := b.Register(context.Background(), principal, newConn); err != nil {
		t.Fatalf("register new: %v", err)
	}

	select {
	case <-oldConn.closed:
	case <-time.After(time.Second):
		t.Fatal("old connection was not closed")
	}
}
