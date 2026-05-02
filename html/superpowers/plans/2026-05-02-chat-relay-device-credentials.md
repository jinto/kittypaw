# Chat Relay Device Credentials Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add server-side lifecycle management for API-issued chat relay device credentials.

**Architecture:** Store auth topology and device tokens in the existing per-account `SecretsStore`. Refresh access tokens before `/daemon/connect` and after unauthorized relay dials. Keep RS256/JWKS verification out of the server; kittychat verifies Bearer tokens.

**Tech Stack:** Go 1.25, `core.APITokenManager`, `remote/chatrelay`, Cobra CLI, `nhooyr.io/websocket`.

---

## Task 1: Discovery And Token Store

**Files:**
- Modify `core/discovery.go`
- Modify `core/discovery_test.go`
- Modify `core/api_token.go`
- Modify `core/api_token_test.go`

- [x] Add failing tests for `auth_base_url`, `SaveChatRelayDeviceTokens`, `LoadChatRelayDeviceTokens`, `ClearChatRelayDeviceTokens`, `PairChatRelayDevice`, and `RefreshChatRelayDeviceToken`.
- [x] Implement `auth_base_url` trimming and persistence helpers.
- [x] Implement pair/refresh request helpers using `{auth_base_url}/devices/pair` and `{auth_base_url}/devices/refresh`.
- [x] Run `go test ./core -run 'Discovery|ChatRelayDevice' -count=1`.

## Task 2: Connector Unauthorized Refresh

**Files:**
- Modify `remote/chatrelay/connector.go`
- Modify `remote/chatrelay/connector_test.go`

- [x] Add a failing test where `/daemon/connect` returns 401 first, the connector calls a refresh callback, then reconnects with the new Bearer token.
- [x] Implement typed unauthorized dial errors and `RefreshCredential func(context.Context) (string, error)`.
- [x] Run `go test ./remote/chatrelay -run 'Unauthorized|Retry|Connector' -count=1`.

## Task 3: Server Start Wiring

**Files:**
- Modify `cli/chat_relay.go`
- Modify `cli/chat_relay_test.go`

- [x] Update connector config resolution to use `chat_relay_access_token` and refresh via `chat_relay_refresh_token`.
- [x] Remove use of `chat_daemon_credential` from new connector config tests.
- [x] Attach the refresh callback to the connector started by `kittypaw server start`.
- [x] Run `go test ./cli -run TestChatRelayConnectorConfigs -count=1`.

## Task 4: Internal CLI Diagnostics

**Files:**
- Create `cli/cmd_chat_relay.go`
- Create `cli/cmd_chat_relay_test.go`
- Modify `cli/chat_relay_test.go`
- Modify `cli/main.go`

- [x] Add hidden `kittypaw chat-relay pair` and `status` diagnostics.
- [x] `pair` uses the user access token and stores device tokens.
- [x] `status` prints only coarse hosted chat readiness and does not expose device IDs or token state.
- [x] Do not expose `disconnect`; stale device cleanup is an automatic API/chat service lifecycle concern.
- [x] Run `go test ./cli -run 'TestChatRelay' -count=1`.

## Final Verification

- [x] `go test ./core ./remote/chatrelay ./cli -count=1`
- [x] `go test ./...`
- [x] `make build`
- [x] `git diff --check`
