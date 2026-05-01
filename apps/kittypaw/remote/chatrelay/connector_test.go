package chatrelay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestBuildDaemonConnectURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{
			name: "https base",
			base: "https://chat.kittypaw.app",
			want: "wss://chat.kittypaw.app/daemon/connect",
		},
		{
			name: "http local base",
			base: "http://localhost:8080",
			want: "ws://localhost:8080/daemon/connect",
		},
		{
			name: "wss path base",
			base: "wss://chat.kittypaw.app/base/",
			want: "wss://chat.kittypaw.app/base/daemon/connect",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildDaemonConnectURL(tt.base)
			if err != nil {
				t.Fatalf("BuildDaemonConnectURL: %v", err)
			}
			if got != tt.want {
				t.Fatalf("BuildDaemonConnectURL(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestDialAndSendHelloSendsAuthorizationAndHello(t *testing.T) {
	helloCh := make(chan HelloFrame, 1)
	errCh := make(chan error, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/daemon/connect" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer device-token-1" {
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "bye")

		_, data, err := conn.Read(r.Context())
		if err != nil {
			errCh <- err
			return
		}
		var hello HelloFrame
		if err := json.Unmarshal(data, &hello); err != nil {
			errCh <- err
			return
		}
		helloCh <- hello
	}))
	defer ts.Close()

	connector := &Connector{Config: ConnectorConfig{
		RelayURL:      ts.URL,
		Credential:    "device-token-1",
		DeviceID:      "dev_1",
		LocalAccounts: []string{"alice"},
		DaemonVersion: "0.1.5",
	}}
	conn, err := connector.DialAndSendHello(context.Background())
	if err != nil {
		t.Fatalf("DialAndSendHello: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	select {
	case err := <-errCh:
		t.Fatalf("server read hello: %v", err)
	case hello := <-helloCh:
		if hello.Type != FrameHello || hello.DeviceID != "dev_1" || hello.ProtocolVersion != ProtocolVersion {
			t.Fatalf("hello = %#v", hello)
		}
		if len(hello.LocalAccounts) != 1 || hello.LocalAccounts[0] != "alice" {
			t.Fatalf("hello local accounts = %#v", hello.LocalAccounts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for hello frame")
	}
}

func TestDialAndSendHelloRejectsMissingInputs(t *testing.T) {
	tests := []struct {
		name string
		cfg  ConnectorConfig
		want string
	}{
		{name: "missing relay url", cfg: ConnectorConfig{Credential: "tok", DeviceID: "dev"}, want: "relay url"},
		{name: "missing credential", cfg: ConnectorConfig{RelayURL: "https://chat.kittypaw.app", DeviceID: "dev"}, want: "credential"},
		{name: "missing device id", cfg: ConnectorConfig{RelayURL: "https://chat.kittypaw.app", Credential: "tok"}, want: "device id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := (&Connector{Config: tt.cfg}).DialAndSendHello(context.Background())
			if conn != nil {
				conn.CloseNow()
			}
			if err == nil {
				t.Fatal("DialAndSendHello error = nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("DialAndSendHello error = %q, want containing %q", err.Error(), tt.want)
			}
		})
	}
}

func TestRunRetriesUntilRelayAccepts(t *testing.T) {
	var attempts atomic.Int32
	helloCh := make(chan HelloFrame, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/daemon/connect" {
			http.NotFound(w, r)
			return
		}
		if attempts.Add(1) == 1 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "bye")

		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Logf("read hello: %v", err)
			return
		}
		var hello HelloFrame
		if err := json.Unmarshal(data, &hello); err != nil {
			t.Logf("decode hello: %v", err)
			return
		}
		helloCh <- hello
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connector := &Connector{Config: ConnectorConfig{
		RelayURL:      ts.URL,
		Credential:    "device-token-1",
		DeviceID:      "dev_1",
		LocalAccounts: []string{"alice"},
		DaemonVersion: "0.1.5",
	}}
	go connector.Run(ctx, RunOptions{
		RetryInitialDelay: time.Millisecond,
		RetryMaxDelay:     time.Millisecond,
	})

	select {
	case hello := <-helloCh:
		if hello.DeviceID != "dev_1" {
			t.Fatalf("hello device id = %q", hello.DeviceID)
		}
		if attempts.Load() < 2 {
			t.Fatalf("attempts = %d, want retry after first failure", attempts.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for retried hello")
	}
}

func TestRunRejectsRequestForUnadvertisedCapability(t *testing.T) {
	errCh := make(chan error, 1)
	errorFrameCh := make(chan ErrorFrame, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "bye")

		if _, _, err := conn.Read(r.Context()); err != nil {
			errCh <- err
			return
		}
		req := RequestFrame{
			Type:      FrameRequest,
			ID:        "req_1",
			Operation: OperationOpenAIModels,
			AccountID: "alice",
		}
		raw, err := json.Marshal(req)
		if err != nil {
			errCh <- err
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageText, raw); err != nil {
			errCh <- err
			return
		}
		_, data, err := conn.Read(r.Context())
		if err != nil {
			errCh <- err
			return
		}
		var got ErrorFrame
		if err := json.Unmarshal(data, &got); err != nil {
			errCh <- err
			return
		}
		errorFrameCh <- got
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connector := &Connector{Config: ConnectorConfig{
		RelayURL:      ts.URL,
		Credential:    "device-token-1",
		DeviceID:      "dev_1",
		LocalAccounts: []string{"alice"},
		DaemonVersion: "0.1.5",
		Capabilities:  []string{},
	}}
	go connector.Run(ctx, RunOptions{
		RetryInitialDelay: time.Millisecond,
		RetryMaxDelay:     time.Millisecond,
	})

	select {
	case err := <-errCh:
		t.Fatalf("relay server: %v", err)
	case got := <-errorFrameCh:
		if got.Type != FrameError || got.ID != "req_1" || got.Code != "unsupported_capability" {
			t.Fatalf("error frame = %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for request rejection")
	}
}

func TestRunUsesDefaultCapabilitiesForRequestValidation(t *testing.T) {
	errCh := make(chan error, 1)
	errorFrameCh := make(chan ErrorFrame, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "bye")

		if _, _, err := conn.Read(r.Context()); err != nil {
			errCh <- err
			return
		}
		req := RequestFrame{
			Type:      FrameRequest,
			ID:        "req_default_caps",
			Operation: OperationOpenAIModels,
			AccountID: "alice",
		}
		raw, err := json.Marshal(req)
		if err != nil {
			errCh <- err
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageText, raw); err != nil {
			errCh <- err
			return
		}
		_, data, err := conn.Read(r.Context())
		if err != nil {
			errCh <- err
			return
		}
		var got ErrorFrame
		if err := json.Unmarshal(data, &got); err != nil {
			errCh <- err
			return
		}
		errorFrameCh <- got
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connector := &Connector{Config: ConnectorConfig{
		RelayURL:      ts.URL,
		Credential:    "device-token-1",
		DeviceID:      "dev_1",
		LocalAccounts: []string{"alice"},
		DaemonVersion: "0.1.5",
	}}
	go connector.Run(ctx, RunOptions{
		RetryInitialDelay: time.Millisecond,
		RetryMaxDelay:     time.Millisecond,
	})

	select {
	case err := <-errCh:
		t.Fatalf("relay server: %v", err)
	case got := <-errorFrameCh:
		if got.Type != FrameError || got.ID != "req_default_caps" || got.Code != "not_implemented" {
			t.Fatalf("error frame = %#v, want not_implemented because nil capabilities advertise defaults", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for request handling")
	}
}

func TestRunRejectsRequestForInactiveAccount(t *testing.T) {
	errCh := make(chan error, 1)
	errorFrameCh := make(chan ErrorFrame, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "bye")

		if _, _, err := conn.Read(r.Context()); err != nil {
			errCh <- err
			return
		}
		req := RequestFrame{
			Type:      FrameRequest,
			ID:        "req_2",
			Operation: OperationOpenAIModels,
			AccountID: "bob",
		}
		raw, err := json.Marshal(req)
		if err != nil {
			errCh <- err
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageText, raw); err != nil {
			errCh <- err
			return
		}
		_, data, err := conn.Read(r.Context())
		if err != nil {
			errCh <- err
			return
		}
		var got ErrorFrame
		if err := json.Unmarshal(data, &got); err != nil {
			errCh <- err
			return
		}
		errorFrameCh <- got
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connector := &Connector{Config: ConnectorConfig{
		RelayURL:      ts.URL,
		Credential:    "device-token-1",
		DeviceID:      "dev_1",
		LocalAccounts: []string{"alice"},
		DaemonVersion: "0.1.5",
		Capabilities:  DefaultCapabilities(),
	}}
	go connector.Run(ctx, RunOptions{
		RetryInitialDelay: time.Millisecond,
		RetryMaxDelay:     time.Millisecond,
	})

	select {
	case err := <-errCh:
		t.Fatalf("relay server: %v", err)
	case got := <-errorFrameCh:
		if got.Type != FrameError || got.ID != "req_2" || got.Code != "unknown_account" {
			t.Fatalf("error frame = %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for request rejection")
	}
}
