package browser

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeCDPConn struct {
	writes chan []byte
	reads  chan []byte
}

func newFakeCDPConn() *fakeCDPConn {
	return &fakeCDPConn{writes: make(chan []byte, 8), reads: make(chan []byte, 8)}
}

func (f *fakeCDPConn) Write(ctx context.Context, b []byte) error {
	select {
	case f.writes <- append([]byte(nil), b...):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeCDPConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case b := <-f.reads:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeCDPConn) Close() error { return nil }

func TestCDPClientCallMatchesResponseByID(t *testing.T) {
	conn := newFakeCDPConn()
	client := newCDPClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer client.Close()

	done := make(chan map[string]any, 1)
	go func() {
		var out map[string]any
		if err := client.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "1+1"}, &out); err != nil {
			t.Errorf("Call: %v", err)
			return
		}
		done <- out
	}()

	var req cdpRequest
	if err := json.Unmarshal(<-conn.writes, &req); err != nil {
		t.Fatalf("request json: %v", err)
	}
	if req.ID == 0 || req.Method != "Runtime.evaluate" {
		t.Fatalf("request = %#v", req)
	}
	conn.reads <- []byte(`{"id":1,"result":{"value":2}}`)

	got := <-done
	if got["value"].(float64) != 2 {
		t.Fatalf("result = %#v", got)
	}
}

func TestCDPClientIgnoresEventsWhileWaiting(t *testing.T) {
	conn := newFakeCDPConn()
	client := newCDPClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		var out map[string]any
		done <- client.Call(ctx, "Page.navigate", nil, &out)
	}()
	var req cdpRequest
	_ = json.Unmarshal(<-conn.writes, &req)
	conn.reads <- []byte(`{"method":"Page.loadEventFired","params":{}}`)
	conn.reads <- []byte(`{"id":1,"result":{"frameId":"f1"}}`)
	if err := <-done; err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
}

func TestCDPClientReturnsCDPError(t *testing.T) {
	conn := newFakeCDPConn()
	client := newCDPClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- client.Call(ctx, "Bad.method", nil, nil)
	}()
	<-conn.writes
	conn.reads <- []byte(`{"id":1,"error":{"code":-32601,"message":"method not found"}}`)
	if err := <-done; err == nil || err.Error() != "cdp error -32601: method not found" {
		t.Fatalf("error = %v", err)
	}
}
