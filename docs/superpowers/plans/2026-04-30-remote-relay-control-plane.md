# Remote Relay Control Plane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go relay/control-plane that lets `chat.kittypaw.app` and Open WebUI reach a user's local Kittypaw daemon through outbound WSS, with first-party auth and account-scoped routing.

**Architecture:** Cloudflare fronts a Go `kittypaw-relayd` origin. Local Kittypaw daemons connect outbound over WSS, advertise paired local accounts, and serve a narrow OpenAI-compatible API through relay request frames. Hosted browser/API traffic authenticates to cloud users, selects a device, and streams responses through the tunnel.

**Tech Stack:** Go, chi, nhooyr WebSocket, Postgres via `pgx`, existing Kittypaw server/engine packages, JSON-over-WebSocket MVP protocol, Cloudflare as edge.

---

## Required Phase 0

Complete and merge:

```text
docs/superpowers/plans/2026-04-30-local-multi-user-account-identity.md
```

This is not optional. The remote relay must target `device_id + local_account_id`; implementing relay first would recreate the unsafe `default` account assumption over the network.

---

## File Structure

- Create: `server/openai.go`  
  Account-scoped local OpenAI-compatible endpoints.

- Create: `server/openai_test.go`  
  Tests for `/v1/models`, non-stream chat, streaming chat, and account scoping.

- Create: `remote/protocol/frame.go`  
  Shared relay frame structs and validation.

- Create: `remote/protocol/frame_test.go`  
  Round-trip and validation tests.

- Create: `remote/connector/connector.go`  
  Local daemon outbound WSS connector.

- Create: `remote/connector/connector_test.go`  
  Reconnect, heartbeat, request dispatch, stream forwarding tests.

- Create: `remote/control/device_store.go`  
  Local device credential storage helpers.

- Create: `cli/cmd_remote.go`  
  `kittypaw remote pair/status/disconnect`.

- Create: `cloud/schema.sql`  
  Hosted control-plane schema.

- Create: `cloud/store/postgres.go`  
  Postgres access for users, sessions, devices, pairing, API keys, audit.

- Create: `cloud/auth/auth.go`  
  Cloud user auth, sessions, password hashing, API key hashing.

- Create: `cloud/devices/devices.go`  
  Pairing and device registry service.

- Create: `cloud/relay/broker.go`  
  In-memory online device connection registry and request multiplexer.

- Create: `cloud/openai/handler.go`  
  Hosted OpenAI-compatible API that relays to online devices.

- Create: `cmd/kittypaw-relayd/main.go`  
  Cloud relay binary.

- Create: `docs/deployment-relay.md`  
  Cloudflare/origin deployment notes.

---

### Task 1: Local OpenAI-Compatible API

**Files:**
- Create: `server/openai.go`
- Create: `server/openai_test.go`
- Modify: `server/server.go`

- [ ] **Step 1: Write failing tests**

Create tests that call:

```text
GET /v1/models
POST /v1/chat/completions
POST /v1/chat/completions with stream=true
```

The tests must create two local accounts and assert the handler uses the authenticated/request account from Phase 0, not `s.session`.

Example expected `/v1/models` response:

```json
{
  "object": "list",
  "data": [
    {"id": "kittypaw", "object": "model", "owned_by": "kittypaw"}
  ]
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./server -run 'TestOpenAI' -count=1
```

Expected: 404 or compile failure because `server/openai.go` does not exist.

- [ ] **Step 3: Implement handlers**

Add routes:

```go
r.Get("/v1/models", s.handleOpenAIModels)
r.Post("/v1/chat/completions", s.handleOpenAIChatCompletions)
```

Request shape:

```go
type openAIChatRequest struct {
    Model    string `json:"model"`
    Messages []struct {
        Role    string `json:"role"`
        Content string `json:"content"`
    } `json:"messages"`
    Stream bool `json:"stream"`
}
```

MVP behavior:

- concatenate the last user message into a Kittypaw event
- call the request account's `engine.Session`
- return OpenAI-compatible JSON for non-streaming
- return SSE chunks for streaming

- [ ] **Step 4: Run tests**

```bash
go test ./server -run 'TestOpenAI' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/openai.go server/openai_test.go server/server.go
git commit -m "feat: add local OpenAI-compatible API"
```

---

### Task 2: Shared Relay Protocol

**Files:**
- Create: `remote/protocol/frame.go`
- Create: `remote/protocol/frame_test.go`

- [ ] **Step 1: Write failing tests**

Tests cover:

```go
func TestFrameRoundTripRequest(t *testing.T)
func TestValidateHelloRequiresDeviceAndAccount(t *testing.T)
func TestValidateRequestRestrictsPaths(t *testing.T)
func TestValidateFrameRejectsOversizedID(t *testing.T)
```

Allowed paths:

```text
/health
/v1/models
/v1/chat/completions
```

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./remote/protocol -count=1
```

Expected: package missing.

- [ ] **Step 3: Implement frame types**

Define:

```go
type FrameType string

const (
    FrameHello FrameType = "hello"
    FrameRequest FrameType = "request"
    FrameResponseHeaders FrameType = "response_headers"
    FrameResponseChunk FrameType = "response_chunk"
    FrameResponseEnd FrameType = "response_end"
    FrameError FrameType = "error"
    FramePing FrameType = "ping"
    FramePong FrameType = "pong"
)

type Frame struct {
    Type FrameType `json:"type"`
    ID string `json:"id,omitempty"`
    DeviceID string `json:"device_id,omitempty"`
    AccountID string `json:"account_id,omitempty"`
    LocalAccounts []string `json:"local_accounts,omitempty"`
    Method string `json:"method,omitempty"`
    Path string `json:"path,omitempty"`
    Status int `json:"status,omitempty"`
    Headers map[string]string `json:"headers,omitempty"`
    Body json.RawMessage `json:"body,omitempty"`
    Data string `json:"data,omitempty"`
    Code string `json:"code,omitempty"`
    Message string `json:"message,omitempty"`
}
```

Add `Validate()` and `AllowedRelayPath(path string) bool`.

- [ ] **Step 4: Run tests**

```bash
go test ./remote/protocol -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add remote/protocol/frame.go remote/protocol/frame_test.go
git commit -m "feat: define remote relay protocol"
```

---

### Task 3: Local Device Credential Store And CLI Skeleton

**Files:**
- Create: `remote/control/device_store.go`
- Create: `remote/control/device_store_test.go`
- Create: `cli/cmd_remote.go`
- Modify: `cli/main.go`

- [ ] **Step 1: Write failing tests**

Credential file:

```text
~/.kittypaw/remote/devices.json
```

Tests:

```go
func TestDeviceStoreSaveLoadAndRevoke(t *testing.T)
func TestDeviceStoreRejectsInvalidLocalAccount(t *testing.T)
func TestRemoteCommandRegistered(t *testing.T)
```

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./remote/control ./cli -run 'TestDeviceStore|TestRemoteCommandRegistered' -count=1
```

Expected: missing packages/command.

- [ ] **Step 3: Implement credential store**

Credential record:

```go
type DeviceCredential struct {
    DeviceID string `json:"device_id"`
    LocalAccountID string `json:"local_account_id"`
    RelayURL string `json:"relay_url"`
    Secret string `json:"secret"`
    CreatedAt time.Time `json:"created_at"`
    RevokedAt *time.Time `json:"revoked_at,omitempty"`
}
```

Store writes mode `0600`, parent dir `0700`.

- [ ] **Step 4: Add CLI skeleton**

Add:

```text
kittypaw remote status
kittypaw remote disconnect <device-id>
kittypaw remote pair --account <id> --relay-url <url>
```

For this task, `pair` may return:

```text
remote pair server exchange not implemented yet
```

but it must validate account selection and flags.

- [ ] **Step 5: Run tests**

```bash
go test ./remote/control ./cli -run 'TestDeviceStore|TestRemoteCommandRegistered' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add remote/control cli/cmd_remote.go cli/main.go
git commit -m "feat: add remote device credential store"
```

---

### Task 4: Local Outbound Connector

**Files:**
- Create: `remote/connector/connector.go`
- Create: `remote/connector/connector_test.go`
- Modify: `server/server.go`
- Modify: `cli/main.go`

- [ ] **Step 1: Write failing tests**

Use an httptest WebSocket server to assert:

- connector sends `hello`
- connector reconnects after close
- connector dispatches allowed request to local handler
- connector rejects forbidden relay path before local HTTP dispatch
- streaming response becomes `response_chunk` frames

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./remote/connector -count=1
```

Expected: package missing.

- [ ] **Step 3: Implement connector**

Connector inputs:

```go
type ConnectorConfig struct {
    RelayURL string
    DeviceID string
    Secret string
    LocalAccountID string
    Version string
}
```

Connector dependencies:

```go
type LocalRoundTripper interface {
    RoundTrip(ctx context.Context, accountID, method, path string, body []byte) (*LocalResponse, error)
}
```

This keeps connector independent from server internals.

- [ ] **Step 4: Wire connector into daemon**

Daemon startup loads active device credentials and starts connectors after HTTP server construction. If relay is unreachable, daemon startup should continue and connector should retry in the background.

- [ ] **Step 5: Run tests**

```bash
go test ./remote/connector ./server -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add remote/connector server/server.go cli/main.go
git commit -m "feat: add outbound remote connector"
```

---

### Task 5: Cloud Relay Schema And Store

**Files:**
- Create: `cloud/schema.sql`
- Create: `cloud/store/postgres.go`
- Create: `cloud/store/postgres_test.go`
- Modify: `go.mod`

- [ ] **Step 1: Write failing store tests**

Tests use `database/sql` and can run against a test Postgres URL from `KITTYPAW_RELAY_TEST_DATABASE_URL`. If unset, skip integration tests.

Required operations:

- create user
- create session
- create pairing code
- consume pairing code once
- create device
- revoke device
- create API key hash
- audit event insert

- [ ] **Step 2: Run tests to verify skip/failure**

```bash
go test ./cloud/store -count=1
```

Expected before implementation: package missing.

- [ ] **Step 3: Implement schema**

Tables:

```sql
users(id, email, password_hash, created_at, disabled_at)
sessions(id, user_id, token_hash, expires_at, revoked_at, created_at)
devices(id, user_id, local_account_id, name, credential_hash, last_seen_at, revoked_at, created_at)
pairing_codes(code_hash, user_id, local_account_id, expires_at, consumed_at, created_at)
api_keys(id, user_id, device_id, key_hash, name, last_used_at, revoked_at, created_at)
audit_events(id, user_id, device_id, action, metadata_json, created_at)
```

- [ ] **Step 4: Implement store**

Use `github.com/jackc/pgx/v5/stdlib`.

- [ ] **Step 5: Run tests**

```bash
go test ./cloud/store -count=1
```

Expected: PASS or SKIP when `KITTYPAW_RELAY_TEST_DATABASE_URL` is unset.

- [ ] **Step 6: Commit**

```bash
git add cloud/schema.sql cloud/store go.mod go.sum
git commit -m "feat: add relay control-plane store"
```

---

### Task 6: Cloud Auth And Pairing Services

**Files:**
- Create: `cloud/auth/auth.go`
- Create: `cloud/auth/auth_test.go`
- Create: `cloud/devices/devices.go`
- Create: `cloud/devices/devices_test.go`

- [ ] **Step 1: Write failing service tests**

Tests cover:

- signup/login creates session
- wrong password rejected
- API key generated once and only hash stored
- pairing code expires
- pairing code can be consumed only once
- device credential is scoped to user + local account

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./cloud/auth ./cloud/devices -count=1
```

Expected: missing packages.

- [ ] **Step 3: Implement services**

Auth service exposes:

```go
Signup(ctx, email, password string) (*User, error)
Login(ctx, email, password string) (*Session, error)
CreateAPIKey(ctx, userID, deviceID, name string) (plaintext string, err error)
VerifyAPIKey(ctx, plaintext string) (*APIKeyPrincipal, error)
```

Device service exposes:

```go
CreatePairingCode(ctx, userID, localAccountID string) (plaintextCode string, err error)
ConsumePairingCode(ctx, plaintextCode, deviceName string) (*DeviceCredential, error)
RevokeDevice(ctx, userID, deviceID string) error
```

- [ ] **Step 4: Run tests**

```bash
go test ./cloud/auth ./cloud/devices -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cloud/auth cloud/devices
git commit -m "feat: add relay auth and pairing services"
```

---

### Task 7: Cloud Relay Broker

**Files:**
- Create: `cloud/relay/broker.go`
- Create: `cloud/relay/broker_test.go`

- [ ] **Step 1: Write failing broker tests**

Tests cover:

- registering one device connection
- duplicate connection replaces old connection
- request to offline device returns offline error
- concurrent requests use unique IDs
- backpressure limit rejects excess in-flight requests
- device revoke closes active connection

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./cloud/relay -run 'TestBroker' -count=1
```

Expected: missing package.

- [ ] **Step 3: Implement broker**

Broker public methods:

```go
Register(ctx context.Context, principal DevicePrincipal, conn DeviceConn) error
Unregister(deviceID string)
Request(ctx context.Context, userID, deviceID string, frame protocol.Frame) (<-chan protocol.Frame, error)
IsOnline(deviceID string) bool
```

Set MVP limits:

```text
max in-flight per device: 16
request timeout: 120s
max frame size: 1 MiB
```

- [ ] **Step 4: Run tests**

```bash
go test ./cloud/relay -run 'TestBroker' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cloud/relay
git commit -m "feat: add relay broker"
```

---

### Task 8: Hosted OpenAI-Compatible API

**Files:**
- Create: `cloud/openai/handler.go`
- Create: `cloud/openai/handler_test.go`

- [ ] **Step 1: Write failing handler tests**

Tests call:

```text
GET /nodes/{device_id}/v1/models
POST /nodes/{device_id}/v1/chat/completions
```

Assert:

- missing API key returns 401
- API key for another user returns 403
- offline device returns 503
- streaming chunks are forwarded as SSE

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./cloud/openai -count=1
```

Expected: missing package.

- [ ] **Step 3: Implement handler**

Handler dependencies:

```go
type APIKeyVerifier interface {
    VerifyAPIKey(ctx context.Context, plaintext string) (*auth.APIKeyPrincipal, error)
}

type Broker interface {
    Request(ctx context.Context, userID, deviceID string, frame protocol.Frame) (<-chan protocol.Frame, error)
}
```

Forward only allowed paths from `remote/protocol`.

- [ ] **Step 4: Run tests**

```bash
go test ./cloud/openai -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cloud/openai
git commit -m "feat: expose hosted OpenAI-compatible relay API"
```

---

### Task 9: Relay Binary And Cloudflare Deployment Notes

**Files:**
- Create: `cmd/kittypaw-relayd/main.go`
- Create: `cmd/kittypaw-relayd/main_test.go`
- Create: `docs/deployment-relay.md`
- Modify: `Makefile`

- [ ] **Step 1: Write failing binary config test**

Test environment parsing:

```text
DATABASE_URL required
RELAY_PUBLIC_BASE_URL required
SESSION_SECRET required
BIND defaults to :8080
```

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./cmd/kittypaw-relayd -count=1
```

Expected: package missing.

- [ ] **Step 3: Implement binary**

Routes:

```text
POST /auth/signup
POST /auth/login
POST /auth/logout
GET  /devices
POST /devices/pairing-codes
DELETE /devices/{device_id}
GET  /connect
GET  /nodes/{device_id}/v1/models
POST /nodes/{device_id}/v1/chat/completions
GET  /health
```

`/connect` upgrades daemon WSS connections and registers the device in the broker.

- [ ] **Step 4: Add Makefile target**

```make
relay:
	go build -o bin/kittypaw-relayd ./cmd/kittypaw-relayd
```

- [ ] **Step 5: Document Cloudflare**

`docs/deployment-relay.md` must include:

- Cloudflare proxied DNS records for `chat.kittypaw.app` and `api.kittypaw.app`
- WebSocket enabled
- origin only accepts Cloudflare IP ranges or private ingress
- WAF/rate limit suggestions
- no Cloudflare Access dependency for auth

- [ ] **Step 6: Run tests**

```bash
go test ./cmd/kittypaw-relayd ./cloud/... ./remote/... ./server -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/kittypaw-relayd Makefile docs/deployment-relay.md
git commit -m "feat: add relay server binary"
```

---

### Task 10: End-To-End Local Relay Smoke Test

**Files:**
- Create: `remote/e2e/relay_smoke_test.go`

- [ ] **Step 1: Write E2E test**

Test launches:

1. local Kittypaw test server with one account
2. in-process broker/API handler
3. connector wired to broker
4. hosted OpenAI request

Assert response comes from the local account session and stream completes.

- [ ] **Step 2: Run test to verify behavior**

```bash
go test ./remote/e2e -count=1
```

Expected: PASS.

- [ ] **Step 3: Run full test suite**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add remote/e2e
git commit -m "test: add remote relay smoke test"
```

---

## Self-Review Notes

- The plan starts with local multi-user identity because remote access must not inherit the legacy `default` assumption.
- The relay is application-level, not a generic proxy.
- Cloudflare is edge infrastructure only; Go origin still owns auth decisions.
- Multi-instance relay routing is deliberately excluded from MVP. The in-memory broker is enough for a single origin instance and can later be replaced with Redis/NATS routing.
- Open WebUI compatibility is provided by hosted OpenAI-compatible endpoints, not by bundling Open WebUI.
