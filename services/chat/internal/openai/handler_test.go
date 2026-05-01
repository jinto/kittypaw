package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/protocol"
)

type staticAuth struct {
	principal Principal
	err       error
}

func (a staticAuth) Authenticate(*http.Request) (Principal, error) {
	return a.principal, a.err
}

type fakeBroker struct {
	req    broker.Request
	frames []protocol.Frame
	err    error
}

func (b *fakeBroker) Request(_ context.Context, req broker.Request) (<-chan protocol.Frame, error) {
	b.req = req
	if b.err != nil {
		return nil, b.err
	}
	ch := make(chan protocol.Frame, len(b.frames))
	for _, frame := range b.frames {
		ch <- frame
	}
	close(ch)
	return ch, nil
}

func TestHandlerReturnsUnauthorizedWithoutAPIKey(t *testing.T) {
	h := NewHandler(staticAuth{err: ErrUnauthorized}, &fakeBroker{})
	req := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	rr := httptest.NewRecorder()

	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
}

func TestModelsRelaysThroughBroker(t *testing.T) {
	fb := &fakeBroker{frames: []protocol.Frame{
		{Type: protocol.FrameResponseHeaders, ID: "req_1", Status: http.StatusOK, Headers: map[string]string{"content-type": "application/json"}},
		{Type: protocol.FrameResponseChunk, ID: "req_1", Data: `{"object":"list","data":[]}`},
		{Type: protocol.FrameResponseEnd, ID: "req_1"},
	}}
	h := NewHandler(staticAuth{principal: Principal{
		UserID: "user_1", DeviceID: "dev_1", AccountID: "alice",
	}}, fb)

	req := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	req.Header.Set("Authorization", "Bearer kp_test")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if fb.req.UserID != "user_1" || fb.req.DeviceID != "dev_1" || fb.req.AccountID != "alice" {
		t.Fatalf("broker request = %+v", fb.req)
	}
	if fb.req.Method != http.MethodGet || fb.req.Path != "/v1/models" {
		t.Fatalf("broker method/path = %s %s", fb.req.Method, fb.req.Path)
	}
	if strings.TrimSpace(rr.Body.String()) != `{"object":"list","data":[]}` {
		t.Fatalf("body = %q", rr.Body.String())
	}
}

func TestChatCompletionsStreamingRelaysSSE(t *testing.T) {
	body := map[string]any{
		"model":  "kittypaw",
		"stream": true,
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}
	raw, _ := json.Marshal(body)
	fb := &fakeBroker{frames: []protocol.Frame{
		{Type: protocol.FrameResponseHeaders, ID: "req_1", Status: http.StatusOK, Headers: map[string]string{"content-type": "text/event-stream"}},
		{Type: protocol.FrameResponseChunk, ID: "req_1", Data: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"},
		{Type: protocol.FrameResponseChunk, ID: "req_1", Data: "data: [DONE]\n\n"},
		{Type: protocol.FrameResponseEnd, ID: "req_1"},
	}}
	h := NewHandler(staticAuth{principal: Principal{
		UserID: "user_1", DeviceID: "dev_1", AccountID: "alice",
	}}, fb)

	req := httptest.NewRequest(http.MethodPost, "/nodes/dev_1/v1/chat/completions", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer kp_test")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	if !strings.Contains(rr.Body.String(), "data: [DONE]") {
		t.Fatalf("body = %q, want SSE done frame", rr.Body.String())
	}
	if fb.req.Method != http.MethodPost || fb.req.Path != "/v1/chat/completions" {
		t.Fatalf("broker method/path = %s %s", fb.req.Method, fb.req.Path)
	}
	if string(fb.req.Body) != string(raw) {
		t.Fatalf("broker body = %s, want %s", fb.req.Body, raw)
	}
}

func TestHandlerReturnsOfflineWhenBrokerHasNoDevice(t *testing.T) {
	h := NewHandler(staticAuth{principal: Principal{
		UserID: "user_1", DeviceID: "dev_1", AccountID: "alice",
	}}, &fakeBroker{err: broker.ErrDeviceOffline})
	req := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	req.Header.Set("Authorization", "Bearer kp_test")
	rr := httptest.NewRecorder()

	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestHandlerRejectsAPIKeyForAnotherDevice(t *testing.T) {
	h := NewHandler(staticAuth{principal: Principal{
		UserID: "user_1", DeviceID: "dev_allowed", AccountID: "alice",
	}}, &fakeBroker{})
	req := httptest.NewRequest(http.MethodGet, "/nodes/dev_other/v1/models", nil)
	req.Header.Set("Authorization", "Bearer kp_test")
	rr := httptest.NewRecorder()

	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestRoutesMountUnderChi(t *testing.T) {
	r := chi.NewRouter()
	r.Mount("/", NewHandler(staticAuth{err: errors.New("no auth")}, &fakeBroker{}).Routes())

	req := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want auth handler response", rr.Code)
	}
}
