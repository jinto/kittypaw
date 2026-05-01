# Chat Relay Daemon Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the daemon-side foundation for chat relay without guessing the unfinished API-server credential issuance or cloud broker behavior.

**Architecture:** The daemon stores a per-account device credential under the API-server namespace, builds an operation-based protocol v1 hello frame, and can open an outbound WebSocket to `{chat_relay_url}/daemon/connect`. Request dispatch and retry loops are deliberately kept separate so the wire contract can stabilize before `/v1/models` and `/v1/chat/completions` execution is attached. Credential issuance and JWT verification are owned by `api.kittypaw.app` and `kittychat`; the daemon only stores and presents the API-issued bearer credential.

**Tech Stack:** Go 1.25, `nhooyr.io/websocket`, existing `core.SecretsStore`, existing per-account `core.APITokenManager`.

---

## Scope

This plan implements the first daemon-side slice only:

- `chat_relay_url` is already fetched and persisted by discovery.
- Add storage helpers for the API-issued daemon/device credential.
- Add a small `remote/chatrelay` package for the wire protocol constants, scope vocabulary, operation vocabulary, and frame structs.
- Add URL building and one-shot WebSocket dial + hello send.

Out of scope for this slice:

- Pairing flow and credential issuance from `api.kittypaw.app`.
- Background reconnect loop.
- Kittychat server implementation.
- Operation dispatch into local account sessions.
- OpenAI-compatible response streaming.

Those need the API-server credential contract and kittychat broker endpoint to land first.

API-side source of truth:

- `https://github.com/kittypaw-app/kittyapi/blob/main/docs/specs/kittychat-credential-foundation.md`
- daemon credential claims are expected to carry `aud=["kittychat"]`, `scope=["daemon:connect"]`, and `v=1`.
- API-client credentials use `chat:relay` and `models:read`; daemon code records these names only as shared vocabulary.

## File Map

- Modify `core/api_token.go`
  - Add `chat_daemon_credential` key and save/load helpers.
- Modify `core/api_token_test.go`
  - Pin per-account credential persistence and empty-value delete behavior.
- Create `remote/chatrelay/protocol.go`
  - Own protocol version, frame type constants, operation/capability names, hello/request/response/error structs, and operation validation.
- Create `remote/chatrelay/protocol_test.go`
  - Pin hello JSON shape and allowed/unsupported operations.
- Create `remote/chatrelay/connector.go`
  - Build `/daemon/connect` WebSocket URL from `chat_relay_url`.
  - Dial once with `Authorization: Bearer <credential>`.
  - Send the hello frame and return the live connection to the caller.
- Create `remote/chatrelay/connector_test.go`
  - Use `httptest` + `nhooyr.io/websocket` to assert URL building, Authorization header, and hello payload.

## Task 1: Device Credential Storage

**Files:**
- Modify: `core/api_token.go`
- Modify: `core/api_token_test.go`

- [ ] **Step 1: Write failing test**

Add this test to `core/api_token_test.go`:

```go
func TestAPITokenManager_SaveAndLoadChatDaemonCredential(t *testing.T) {
	secrets := &SecretsStore{
		path: t.TempDir() + "/secrets.json",
		data: make(map[string]map[string]string),
	}
	mgr := NewAPITokenManager("", secrets)
	apiURL := "https://portal.kittypaw.app"
	ns := NamespaceForURL(apiURL)

	if err := mgr.SaveChatDaemonCredential(apiURL, "device-token-1"); err != nil {
		t.Fatal(err)
	}
	got, ok := mgr.LoadChatDaemonCredential(apiURL)
	if !ok || got != "device-token-1" {
		t.Fatalf("LoadChatDaemonCredential = (%q, %v), want device token", got, ok)
	}
	if stored, ok := secrets.Get(ns, "chat_daemon_credential"); !ok || stored != "device-token-1" {
		t.Fatalf("stored credential = (%q, %v), want chat_daemon_credential", stored, ok)
	}

	if err := mgr.SaveChatDaemonCredential(apiURL, ""); err != nil {
		t.Fatal(err)
	}
	got, ok = mgr.LoadChatDaemonCredential(apiURL)
	if ok || got != "" {
		t.Fatalf("after empty save, LoadChatDaemonCredential = (%q, %v), want empty false", got, ok)
	}
}
```

- [ ] **Step 2: Verify red**

Run:

```bash
go test ./core -run TestAPITokenManager_SaveAndLoadChatDaemonCredential -count=1
```

Expected: compile failure because `SaveChatDaemonCredential` and `LoadChatDaemonCredential` do not exist.

- [ ] **Step 3: Implement minimal storage helpers**

Add the key constant and methods to `core/api_token.go`:

```go
const chatDaemonCredentialKey = "chat_daemon_credential"

func (m *APITokenManager) SaveChatDaemonCredential(apiURL, credential string) error {
	return m.saveOrDelete(NamespaceForURL(apiURL), chatDaemonCredentialKey, credential)
}

func (m *APITokenManager) LoadChatDaemonCredential(apiURL string) (string, bool) {
	return m.secrets.Get(NamespaceForURL(apiURL), chatDaemonCredentialKey)
}
```

- [ ] **Step 4: Verify green**

Run:

```bash
go test ./core -run TestAPITokenManager_SaveAndLoadChatDaemonCredential -count=1
```

Expected: PASS.

## Task 2: Operation-Based Protocol Contract

**Files:**
- Create: `remote/chatrelay/protocol.go`
- Create: `remote/chatrelay/protocol_test.go`

- [ ] **Step 1: Write failing tests**

Create `remote/chatrelay/protocol_test.go` with tests for:

```go
func TestNewHelloFramePinsProtocolVersionAndCapabilities(t *testing.T)
func TestOperationSupportIsOperationBased(t *testing.T)
```

The hello JSON must include:

```json
{
  "type": "hello",
  "device_id": "dev_1",
  "local_accounts": ["alice"],
  "daemon_version": "0.1.5",
  "protocol_version": "1",
  "capabilities": ["openai.models", "openai.chat_completions"]
}
```

Allowed operations:

```text
openai.models
openai.chat_completions
```

- [ ] **Step 2: Verify red**

Run:

```bash
go test ./remote/chatrelay -run 'TestNewHelloFrame|TestOperationSupport' -count=1
```

Expected: package missing or compile failure.

- [ ] **Step 3: Implement protocol types**

Create `remote/chatrelay/protocol.go` with:

```go
const ProtocolVersion = "1"

const (
	OperationOpenAIModels          = "openai.models"
	OperationOpenAIChatCompletions = "openai.chat_completions"
)

const (
	ScopeChatRelay     = "chat:relay"
	ScopeModelsRead    = "models:read"
	ScopeDaemonConnect = "daemon:connect"
)

const (
	FrameHello          = "hello"
	FrameRequest        = "request"
	FrameResponseHeaders = "response_headers"
	FrameResponseChunk   = "response_chunk"
	FrameResponseEnd     = "response_end"
	FrameError           = "error"
)

type HelloFrame struct {
	Type            string   `json:"type"`
	DeviceID        string   `json:"device_id"`
	LocalAccounts   []string `json:"local_accounts"`
	DaemonVersion   string   `json:"daemon_version"`
	ProtocolVersion string   `json:"protocol_version"`
	Capabilities    []string `json:"capabilities"`
}
```

Include request/response/error structs even if dispatch is not wired in this slice, so kittychat and daemon share one vocabulary. Response frames must preserve the existing relay JSON shape:

```json
{"type":"response_headers","id":"req_1","status":200,"headers":{"content-type":"text/event-stream"}}
{"type":"response_chunk","id":"req_1","data":"data: ...\n\n"}
```

- [ ] **Step 4: Verify green**

Run:

```bash
go test ./remote/chatrelay -run 'TestNewHelloFrame|TestOperationSupport' -count=1
```

Expected: PASS.

## Task 3: Outbound Connector Handshake

**Files:**
- Create: `remote/chatrelay/connector.go`
- Create: `remote/chatrelay/connector_test.go`

- [ ] **Step 1: Write failing tests**

Create tests for:

```go
func TestBuildDaemonConnectURL(t *testing.T)
func TestDialAndSendHelloSendsAuthorizationAndHello(t *testing.T)
func TestDialAndSendHelloRejectsMissingInputs(t *testing.T)
```

URL cases:

```text
https://chat.kittypaw.app -> wss://chat.kittypaw.app/daemon/connect
http://localhost:8080 -> ws://localhost:8080/daemon/connect
wss://chat.kittypaw.app/base/ -> wss://chat.kittypaw.app/base/daemon/connect
```

- [ ] **Step 2: Verify red**

Run:

```bash
go test ./remote/chatrelay -run 'TestBuildDaemonConnectURL|TestDialAndSendHello' -count=1
```

Expected: compile failure because connector APIs do not exist.

- [ ] **Step 3: Implement connector**

Create `remote/chatrelay/connector.go` with:

```go
type ConnectorConfig struct {
	RelayURL       string
	Credential     string
	DeviceID       string
	LocalAccounts  []string
	DaemonVersion  string
	Capabilities   []string
}

type Connector struct {
	Config ConnectorConfig
}

func BuildDaemonConnectURL(base string) (string, error)
func (c *Connector) DialAndSendHello(ctx context.Context) (*websocket.Conn, error)
```

`DialAndSendHello` must set `Authorization: Bearer <credential>`, send a `HelloFrame`, and return the live WebSocket connection. It does not run a reconnect loop.

- [ ] **Step 4: Verify green**

Run:

```bash
go test ./remote/chatrelay -count=1
```

Expected: PASS.

## Final Verification

Run:

```bash
go test ./core ./remote/chatrelay -count=1
go test ./... -count=1
golangci-lint run
make build
git diff --check
```

Expected: all commands pass.

## Handoff Notes

After this slice, the next implementation slice should add:

- API-issued credential exchange/pairing command once `api.kittypaw.app` endpoint is ready.
- Daemon startup wiring that loads `chat_relay_url` + `chat_daemon_credential`.
- Background reconnect loop with health logging.
- Operation dispatch for `openai.models` and `openai.chat_completions` against account-scoped local handlers.
