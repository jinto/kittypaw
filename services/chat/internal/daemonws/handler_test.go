package daemonws

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"
	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/openai"
	"github.com/kittypaw-app/kittychat/internal/protocol"
)

func TestDaemonWebSocketRelaysOpenAIRequestToDaemon(t *testing.T) {
	b := broker.New(broker.Config{
		RequestTimeout:       2 * time.Second,
		MaxInflightPerDevice: 4,
	})
	principal := broker.DevicePrincipal{
		UserID:          "user_1",
		DeviceID:        "dev_1",
		LocalAccountIDs: []string{"alice"},
	}

	r := chi.NewRouter()
	r.Mount("/daemon", NewHandler(StaticTokenAuthenticator{
		Token:     "dev_secret",
		Principal: principal,
	}, b).Routes())
	r.Mount("/", openai.NewHandler(openai.StaticTokenAuthenticator{
		Token: "api_secret",
		Principal: openai.Principal{
			UserID:    "user_1",
			DeviceID:  "dev_1",
			AccountID: "alice",
		},
	}, b).Routes())

	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/daemon/connect"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer dev_secret"}},
	})
	if err != nil {
		t.Fatalf("dial daemon websocket: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()

	if err := wsjson.Write(ctx, conn, protocol.Frame{
		Type:            protocol.FrameHello,
		DeviceID:        "dev_1",
		LocalAccounts:   []string{"alice"},
		DaemonVersion:   "test",
		ProtocolVersion: protocol.ProtocolVersion1,
		Capabilities:    []protocol.Operation{protocol.OperationOpenAIModels, protocol.OperationOpenAIChatCompletions},
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		var reqFrame protocol.Frame
		if err := wsjson.Read(ctx, conn, &reqFrame); err != nil {
			errCh <- err
			return
		}
		if reqFrame.Type != protocol.FrameRequest || reqFrame.Operation != protocol.OperationOpenAIModels || reqFrame.Path != "/v1/models" {
			errCh <- &unexpectedFrameError{frame: reqFrame}
			return
		}
		frames := []protocol.Frame{
			{
				Type:    protocol.FrameResponseHeaders,
				ID:      reqFrame.ID,
				Status:  http.StatusOK,
				Headers: map[string]string{"content-type": "application/json"},
			},
			{
				Type: protocol.FrameResponseChunk,
				ID:   reqFrame.ID,
				Data: `{"object":"list","data":[{"id":"kittypaw","object":"model","owned_by":"kittypaw"}]}`,
			},
			{Type: protocol.FrameResponseEnd, ID: reqFrame.ID},
		}
		for _, frame := range frames {
			if err := wsjson.Write(ctx, conn, frame); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/nodes/dev_1/v1/models", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	httpReq.Header.Set("Authorization", "Bearer api_secret")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("openai client request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, raw)
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	if body.Object != "list" || len(body.Data) != 1 || body.Data[0].ID != "kittypaw" {
		t.Fatalf("body = %+v", body)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("daemon goroutine: %v", err)
	}
}

func TestDaemonWebSocketRejectsBadToken(t *testing.T) {
	b := broker.New(broker.Config{})
	r := chi.NewRouter()
	r.Mount("/daemon", NewHandler(StaticTokenAuthenticator{
		Token: "dev_secret",
		Principal: broker.DevicePrincipal{
			UserID:          "user_1",
			DeviceID:        "dev_1",
			LocalAccountIDs: []string{"alice"},
		},
	}, b).Routes())
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/daemon/connect"
	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer wrong"}},
	})
	if err == nil {
		t.Fatal("dial succeeded with wrong token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", responseStatus(resp))
	}
}

type unexpectedFrameError struct {
	frame protocol.Frame
}

func (e *unexpectedFrameError) Error() string {
	raw, _ := json.Marshal(e.frame)
	return "unexpected frame: " + string(raw)
}

func responseStatus(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
