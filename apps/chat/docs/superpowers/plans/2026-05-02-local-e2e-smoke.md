# Local E2E Smoke Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a local smoke command that proves KittyChat relays an OpenAI-compatible chat completion through a daemon WebSocket.

**Architecture:** A reusable `internal/smoke` runner starts the real router in-process with seeded static credentials, connects a fake daemon, verifies `/v1/routes`, performs a chat completion request, and validates SSE output. A thin `cmd/kittychat-smoke` CLI runs the smoke, and `make smoke-local` exposes it.

**Tech Stack:** Go 1.25, `httptest`, `net/http`, `coder/websocket`, existing `broker`, `daemonws`, `identity`, `openai`, `protocol`, and `server` packages.

---

### Task 1: Local Smoke Runner Test

**Files:**
- Create: `internal/smoke/local_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/smoke/local_test.go`:

```go
package smoke

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunLocalCompletesChatRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out bytes.Buffer
	if err := RunLocal(ctx, &out); err != nil {
		t.Fatalf("RunLocal() error = %v; output=%s", err, out.String())
	}

	output := out.String()
	for _, want := range []string{
		"daemon connected",
		"route discovery",
		"chat completion",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want progress containing %q", output, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/smoke -run TestRunLocalCompletesChatRoundTrip -count=1`

Expected: FAIL because package `internal/smoke` or `RunLocal` does not exist.

---

### Task 2: Local Smoke Runner Implementation

**Files:**
- Create: `internal/smoke/local.go`
- Test: `internal/smoke/local_test.go`

- [ ] **Step 1: Implement the runner**

Create `internal/smoke/local.go` with a real in-process router, fake daemon, route check, and chat check:

```go
package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/daemonws"
	"github.com/kittypaw-app/kittychat/internal/identity"
	"github.com/kittypaw-app/kittychat/internal/openai"
	"github.com/kittypaw-app/kittychat/internal/protocol"
	"github.com/kittypaw-app/kittychat/internal/server"
)

const (
	localAPIToken      = "api_secret"
	localDeviceToken   = "dev_secret"
	localUserID        = "user_1"
	localDeviceID      = "dev_1"
	localAccountID     = "alice"
	localSmokeUserText = "hello from smoke"
)

func RunLocal(ctx context.Context, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	router, err := localRouter()
	if err != nil {
		return err
	}
	srv, err := newLocalServer(router, net.Listen)
	if err != nil {
		return err
	}
	defer srv.Close()

	daemonReady := make(chan struct{})
	daemonDone := make(chan error, 1)
	go func() {
		daemonDone <- runFakeDaemon(ctx, srv.URL, daemonReady)
	}()

	select {
	case <-daemonReady:
		fmt.Fprintln(out, "ok daemon connected")
	case err := <-daemonDone:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := waitForRoute(ctx, srv.URL); err != nil {
		return err
	}
	fmt.Fprintln(out, "ok route discovery dev_1/alice")

	if err := runChatCompletion(ctx, srv.URL); err != nil {
		return err
	}
	fmt.Fprintln(out, "ok chat completion relayed")

	select {
	case err := <-daemonDone:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func localRouter() (http.Handler, error) {
	verifier := identity.NewMemoryCredentialVerifier()
	if err := verifier.AddAPIClient(localAPIToken, identity.APIClientClaims{
		Subject:   localUserID,
		Audiences: []string{identity.AudienceKittyChat},
		Version:   identity.CredentialVersion1,
		Scopes:    []identity.Scope{identity.ScopeChatRelay, identity.ScopeModelsRead},
		UserID:    localUserID,
		DeviceID:  localDeviceID,
		AccountID: localAccountID,
	}); err != nil {
		return nil, fmt.Errorf("seed api client: %w", err)
	}
	if err := verifier.AddDevice(localDeviceToken, identity.DeviceClaims{
		Subject:         "device:" + localDeviceID,
		Audiences:       []string{identity.AudienceKittyChat},
		Version:         identity.CredentialVersion1,
		Scopes:          []identity.Scope{identity.ScopeDaemonConnect},
		UserID:          localUserID,
		DeviceID:        localDeviceID,
		LocalAccountIDs: []string{localAccountID},
	}); err != nil {
		return nil, fmt.Errorf("seed device: %w", err)
	}

	b := broker.New(broker.Config{RequestTimeout: 2 * time.Second})
	return server.NewRouter(server.Config{
		Version: "smoke",
		DaemonHandler: daemonws.NewHandler(identity.DeviceAuthenticator{
			Verifier: verifier,
		}, b),
		OpenAIHandler: openai.NewHandler(identity.APIAuthenticator{
			Verifier: verifier,
		}, b),
	}), nil
}

type listenFunc func(network, address string) (net.Listener, error)

func newLocalServer(handler http.Handler, listen listenFunc) (*httptest.Server, error) {
	if listen == nil {
		listen = net.Listen
	}
	ln, err := listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen local smoke server: %w", err)
	}
	srv := &httptest.Server{
		Listener: ln,
		Config:   &http.Server{Handler: handler},
	}
	srv.Start()
	return srv, nil
}

func runFakeDaemon(ctx context.Context, baseURL string, ready chan<- struct{}) error {
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/daemon/connect"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + localDeviceToken}},
	})
	if err != nil {
		return fmt.Errorf("connect fake daemon: %w", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "smoke done") }()

	if err := wsjson.Write(ctx, conn, protocol.Frame{
		Type:            protocol.FrameHello,
		DeviceID:        localDeviceID,
		LocalAccounts:   []string{localAccountID},
		DaemonVersion:   "smoke",
		ProtocolVersion: protocol.ProtocolVersion1,
		Capabilities:    []protocol.Operation{protocol.OperationOpenAIChatCompletions},
	}); err != nil {
		return fmt.Errorf("write hello: %w", err)
	}
	close(ready)

	var req protocol.Frame
	if err := wsjson.Read(ctx, conn, &req); err != nil {
		return fmt.Errorf("read relay request: %w", err)
	}
	if err := validateSmokeRequest(req); err != nil {
		return err
	}

	for _, frame := range []protocol.Frame{
		{
			Type:    protocol.FrameResponseHeaders,
			ID:      req.ID,
			Status:  http.StatusOK,
			Headers: map[string]string{"content-type": "text/event-stream"},
		},
		{
			Type: protocol.FrameResponseChunk,
			ID:   req.ID,
			Data: "data: {\"choices\":[{\"delta\":{\"content\":\"hello from fake daemon\"}}]}\n\n",
		},
		{Type: protocol.FrameResponseChunk, ID: req.ID, Data: "data: [DONE]\n\n"},
		{Type: protocol.FrameResponseEnd, ID: req.ID},
	} {
		if err := wsjson.Write(ctx, conn, frame); err != nil {
			return fmt.Errorf("write daemon response: %w", err)
		}
	}
	return nil
}

func validateSmokeRequest(frame protocol.Frame) error {
	if frame.Type != protocol.FrameRequest {
		return fmt.Errorf("request frame type = %q, want %q", frame.Type, protocol.FrameRequest)
	}
	if frame.AccountID != localAccountID {
		return fmt.Errorf("request account_id = %q, want %q", frame.AccountID, localAccountID)
	}
	if frame.Operation != protocol.OperationOpenAIChatCompletions {
		return fmt.Errorf("request operation = %q, want %q", frame.Operation, protocol.OperationOpenAIChatCompletions)
	}
	if frame.Method != http.MethodPost || frame.Path != "/v1/chat/completions" {
		return fmt.Errorf("request method/path = %s %s, want POST /v1/chat/completions", frame.Method, frame.Path)
	}

	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(frame.Body, &body); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	for _, message := range body.Messages {
		if message.Role == "user" && message.Content == localSmokeUserText {
			return nil
		}
	}
	return fmt.Errorf("request body does not contain smoke user message")
}

func waitForRoute(ctx context.Context, baseURL string) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		ok, err := routeOnline(ctx, baseURL)
		if ok {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("wait for route: %w", lastErr)
			}
			return ctx.Err()
		}
	}
}

func routeOnline(ctx context.Context, baseURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/routes", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+localAPIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("routes status = %d; body=%s", resp.StatusCode, raw)
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			DeviceID      string               `json:"device_id"`
			LocalAccounts []string             `json:"local_accounts"`
			Capabilities  []protocol.Operation `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return false, err
	}
	for _, route := range body.Data {
		if route.DeviceID == localDeviceID && hasString(route.LocalAccounts, localAccountID) &&
			hasOperation(route.Capabilities, protocol.OperationOpenAIChatCompletions) {
			return true, nil
		}
	}
	return false, nil
}

func runChatCompletion(ctx context.Context, baseURL string) error {
	body := []byte(`{"model":"kittypaw","stream":true,"messages":[{"role":"user","content":"hello from smoke"}]}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/nodes/dev_1/accounts/alice/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+localAPIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chat status = %d; body=%s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		return fmt.Errorf("chat content-type = %q, want text/event-stream", got)
	}
	text := string(raw)
	if !strings.Contains(text, "hello from fake daemon") || !strings.Contains(text, "data: [DONE]") {
		return fmt.Errorf("chat body = %q, want fake daemon content and done marker", text)
	}
	return nil
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasOperation(values []protocol.Operation, want protocol.Operation) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/smoke -run TestRunLocalCompletesChatRoundTrip -count=1`

Expected: PASS.

---

### Task 3: CLI and Make Target

**Files:**
- Create: `cmd/kittychat-smoke/main.go`
- Modify: `Makefile`

- [ ] **Step 1: Add the CLI wrapper**

Create `cmd/kittychat-smoke/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kittypaw-app/kittychat/internal/smoke"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := smoke.RunLocal(ctx, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "smoke failed: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Add `make smoke-local`**

Modify `Makefile` so the phony list and target include:

```make
.PHONY: build test lint fmt run smoke-local clean

smoke-local:
	go run ./cmd/kittychat-smoke
```

- [ ] **Step 3: Verify CLI package**

Run: `go test ./cmd/kittychat-smoke -count=1`

Expected: PASS with `[no test files]`.

- [ ] **Step 4: Verify make target**

Run: `make smoke-local`

Expected: exits 0 and prints:

```text
ok daemon connected
ok route discovery dev_1/alice
ok chat completion relayed
```

---

### Task 4: Documentation and Full Verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Document local smoke**

Add this to the Development section after the build commands:

````markdown
Run the local end-to-end smoke to verify a fake daemon can receive an
OpenAI-compatible chat completion request and stream a response back:

```bash
make smoke-local
```
````

- [ ] **Step 2: Format changed Go files**

Run: `gofmt -w internal/smoke cmd/kittychat-smoke`

Expected: exits 0.

- [ ] **Step 3: Run full tests**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 4: Check git status**

Run: `git status --short`

Expected: only the intended smoke runner, CLI, Makefile, README, and plan files are changed.
