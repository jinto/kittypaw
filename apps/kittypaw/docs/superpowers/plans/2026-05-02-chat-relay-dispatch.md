# Chat Relay Dispatch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route chat relay `openai.models` and `openai.chat_completions` requests from the outbound server WebSocket into the correct local account session.

**Architecture:** `remote/chatrelay` owns the wire protocol and WebSocket frame lifecycle. It calls a small `Dispatcher` interface after account/capability validation, keeping relay framing independent from `server` and `engine`. `server` provides the server-local OpenAI-compatible adapter that maps `account_id` to an active account session.

**Tech Stack:** Go 1.25, `nhooyr.io/websocket`, existing `engine.Session.RunTurn`, existing `core.Config` model metadata.

---

## Scope

In scope:

- Add `remote/chatrelay.Dispatcher`.
- Emit `response_headers`, `response_chunk`, and `response_end` for successful dispatch.
- Implement `server.ChatRelayDispatcher` for:
  - `openai.models`
  - `openai.chat_completions`
- Extract a stable chat session key from OpenAI-compatible request JSON:
  - `metadata.kittypaw_session_id`
  - `metadata.session_id`
  - `user`
  - fallback to relay request id
- Keep permission callbacks out of this slice. Operations requiring approval remain denied by existing engine behavior when no callback is provided.
- Advertise default capabilities from server startup once dispatcher is wired.

Out of scope:

- API server pairing/device credential issuance.
- True token streaming from providers.
- Remote permission approval round-trips.
- Full OpenAI API compatibility beyond the fields needed by hosted chat.

## File Map

- Modify `remote/chatrelay/connector.go`
  - Add `Dispatcher`, `DispatchResult`, and `DispatchError`.
  - Call dispatcher from request handling.
  - Write response frames.
- Modify `remote/chatrelay/connector_test.go`
  - Add success dispatch tests.
- Create `server/chat_relay_dispatcher.go`
  - Implement OpenAI-compatible model list and chat completion handling.
- Create `server/chat_relay_dispatcher_test.go`
  - Pin account routing, model response, non-stream and stream chat response shapes.
- Modify `cli/chat_relay.go`
  - Accept a dispatcher and attach it to every connector.
  - Advertise default capabilities when dispatch is available.
- Modify `cli/main.go`
  - Pass `server.NewChatRelayDispatcher(srv)` into startup.

## Tasks

### Task 1: Remote Dispatcher Contract

- [ ] Add a failing test where a configured dispatcher returns status 200, headers, and body.
- [ ] Run `go test ./remote/chatrelay -run TestRunDispatchesRequestAndWritesResponseFrames -count=1` and confirm it fails.
- [ ] Add `Dispatcher`, `DispatchResult`, `DispatchError`, and response frame writing.
- [ ] Keep the existing nil-dispatcher behavior as `not_implemented`.
- [ ] Run `go test ./remote/chatrelay -count=1`.

### Task 2: Server OpenAI-Compatible Dispatcher

- [ ] Add failing tests for `openai.models`.
- [ ] Add failing tests for non-stream `openai.chat_completions`.
- [ ] Add failing tests for stream `openai.chat_completions`.
- [ ] Implement account lookup through the active `Server` account router.
- [ ] Implement model list from default LLM plus `[[models]]`.
- [ ] Implement chat request parsing and `Session.RunTurn`.
- [ ] Run `go test ./server -run ChatRelay -count=1`.

### Task 3: Server Start Wiring

- [ ] Add failing CLI config test proving dispatcher presence advertises default capabilities.
- [ ] Add `Dispatcher` to connector config.
- [ ] Wire `startChatRelayConnectors(ctx, deps, version, dispatcher)`.
- [ ] Run `go test ./cli -run ChatRelay -count=1`.

### Final Verification

Run:

```bash
go test ./remote/chatrelay ./server ./cli -count=1
go test ./... -count=1
golangci-lint run
make build
git diff --check
```

