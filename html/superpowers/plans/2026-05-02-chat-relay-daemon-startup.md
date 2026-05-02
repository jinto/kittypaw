# Chat Relay Server Startup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Start the outbound chat relay connector from `kittypaw server start` when an account already has relay topology and a device credential stored.

**Architecture:** Keep API device issuance out of this slice. The server loads per-account secrets, builds connector configs only when `chat_relay_url`, `chat_relay_device_id`, and `chat_daemon_credential` are all present, groups accounts by `relay_url + device credential + device_id`, and starts background retry loops that do not block server startup.

**Tech Stack:** Go 1.25, existing `core.APITokenManager`, existing `server.AccountDeps`, `remote/chatrelay` WebSocket connector.

---

## Scope

In scope:

- Store/load `chat_relay_device_id` beside `chat_daemon_credential`.
- Add a retrying connector loop to `remote/chatrelay`.
- Keep the relay WebSocket alive after hello and explicitly reject unsupported request frames with `error` frames.
- Resolve connector configs from account dependencies during server startup.
- Advertise all active local accounts that share the same device credential in one hello frame.
- Advertise an empty capability set until request dispatch is wired; the connection is a lifecycle/routing advertisement, not an operation-ready endpoint yet.
- Start configured connectors in the `server start` context; if relay is missing or down, local server still starts.

Out of scope:

- Pairing and credential issuance API calls.
- CLI `remote pair/status/disconnect`.
- Request frame dispatch to local account sessions.
- OpenAI-compatible streaming.

## File Map

- Modify `core/api_token.go`
  - Add `SaveChatRelayDeviceID` and `LoadChatRelayDeviceID`.
- Modify `core/api_token_test.go`
  - Pin storage key and empty-delete behavior.
- Modify `remote/chatrelay/connector.go`
  - Add `Run` with bounded retry backoff.
- Modify `remote/chatrelay/connector_test.go`
  - Assert retry after an initial failed dial.
  - Assert request frames for inactive accounts or unadvertised capabilities receive `error` frames.
- Create `cli/chat_relay.go`
  - Resolve configured connectors from `server.AccountDeps`.
  - Start background connector goroutines.
- Create `cli/chat_relay_test.go`
  - Assert complete secrets produce one config.
  - Assert partial secrets produce no config.
- Modify `cli/main.go`
  - Call startup helper from `runServe`.

## Task 1: Store Device ID

- [ ] Add a failing test:

```go
func TestAPITokenManager_SaveAndLoadChatRelayDeviceID(t *testing.T)
```

The test writes `chat_relay_device_id`, reads it back, and verifies empty save deletes it.

- [ ] Run:

```bash
go test ./core -run TestAPITokenManager_SaveAndLoadChatRelayDeviceID -count=1
```

Expected: compile failure.

- [ ] Implement:

```go
func (m *APITokenManager) SaveChatRelayDeviceID(apiURL, deviceID string) error
func (m *APITokenManager) LoadChatRelayDeviceID(apiURL string) (string, bool)
```

## Task 2: Retrying Connector Loop

- [ ] Add a failing test:

```go
func TestRunRetriesUntilRelayAccepts(t *testing.T)
```

The test uses an `httptest` WebSocket server that returns `503` on the first `/daemon/connect` request and accepts the second. `Connector.Run` must retry and send hello on the second attempt.

- [ ] Run:

```bash
go test ./remote/chatrelay -run TestRunRetriesUntilRelayAccepts -count=1
```

Expected: compile failure because `Run` and `RunOptions` do not exist.

- [ ] Implement:

```go
type RunOptions struct {
	RetryInitialDelay time.Duration
	RetryMaxDelay     time.Duration
	Logf              func(format string, args ...any)
}

func (c *Connector) Run(ctx context.Context, opts RunOptions)
```

On successful connect, `Run` blocks reading until the relay closes or the context is cancelled. On failed dial or dropped connection, it waits and retries. It must return promptly when `ctx` is cancelled.

While request dispatch is still out of scope, `Run` must not silently drop relay requests:

- request for a local account not present in this connection's hello: `unknown_account`
- request for an unknown operation: `unsupported_operation`
- request for a supported operation not advertised in hello capabilities: `unsupported_capability`
- request for an advertised-but-unimplemented operation: `not_implemented`

## Task 3: Server Start Config Resolution

- [ ] Add failing tests in `cli/chat_relay_test.go`:

```go
func TestChatRelayConnectorConfigsRequiresCompleteAccountSecrets(t *testing.T)
func TestChatRelayConnectorConfigsSkipsPartialSecrets(t *testing.T)
```

Complete account secrets are:

- `chat_relay_url`
- `chat_relay_device_id`
- `chat_daemon_credential`

Accounts sharing the same `chat_relay_url + chat_relay_device_id + chat_daemon_credential` must produce one connector config whose `local_accounts` contains every matching local account. Accounts with the same `device_id` but different credentials must remain separate because they may belong to different API users.

- [ ] Run:

```bash
go test ./cli -run 'TestChatRelayConnectorConfigs' -count=1
```

Expected: compile failure because the helper does not exist.

- [ ] Implement `cli/chat_relay.go`:

```go
func chatRelayConnectorConfigs(deps []*server.AccountDeps, daemonVersion string) []chatrelay.ConnectorConfig
func startChatRelayConnectors(ctx context.Context, deps []*server.AccountDeps, daemonVersion string)
```

The helper must use `kittypaw-api/api_url` from account secrets when present, otherwise `core.DefaultAPIServerURL`. Each complete account creates one connector advertising only that local account ID.

## Task 4: Wire Into Server Start

- [ ] Modify `runServe` after `srv.StartChannels(ctx)`:

```go
startChatRelayConnectors(ctx, deps, version)
```

No error is returned to `server start`; relay failures are background-only.

## Final Verification

Run:

```bash
go test ./core ./remote/chatrelay ./cli -count=1
go test ./... -count=1
golangci-lint run
make build
git diff --check
```

Expected: all pass.
