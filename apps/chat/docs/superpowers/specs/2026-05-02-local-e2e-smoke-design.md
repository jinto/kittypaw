# Local E2E Smoke Design

> Historical plan snapshot. This document records an app-local implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and the app README/DEPLOY docs for the current live shape.

## Goal

Add a local end-to-end smoke command that proves KittyChat can relay an
OpenAI-compatible chat completion request through a daemon WebSocket connection.

The command must answer the operational question: "Can KittyChat chat through
the relay right now?"

## Scope

This slice adds a local smoke runner and command. It does not add a browser chat
UI, production daemon connector, API server key issuance, or pairing flow.

## Approach

The smoke runner starts the real KittyChat HTTP router in-process with static
MVP credentials, connects a fake daemon to `/daemon/connect`, verifies route
discovery, sends an OpenAI-compatible chat completion request, and checks that
the response is streamed back as SSE.

This keeps the smoke deterministic and fast while still exercising the same
router, authentication, WebSocket handler, broker, and OpenAI-compatible HTTP
handler used by the service.

## Components

- `internal/smoke`: reusable local smoke runner.
- `cmd/kittychat-smoke`: CLI wrapper that runs the local smoke and exits
  non-zero on failure.
- `Makefile`: adds `smoke-local`.
- `README.md`: documents when and how to run the smoke.

## Data Flow

1. The runner starts a KittyChat router using static credentials:
   - API token: `api_secret`
   - device token: `dev_secret`
   - user id: `user_1`
   - device id: `dev_1`
   - local account id: `alice`
2. A fake daemon connects to `/daemon/connect` with `dev_secret`.
3. The fake daemon sends a `hello` frame for `dev_1`, account `alice`, and
   capability `openai.chat_completions`.
4. The smoke client calls `GET /v1/routes` with `api_secret` and verifies that
   `dev_1/alice` is online.
5. The smoke client posts to
   `/nodes/dev_1/accounts/alice/v1/chat/completions`.
6. The fake daemon receives the broker request frame and validates:
   - type is `request`
   - account id is `alice`
   - operation is `openai.chat_completions`
   - method/path are `POST /v1/chat/completions`
   - body contains the smoke user message
7. The fake daemon sends `response_headers`, two `response_chunk` frames, and
   `response_end`.
8. The smoke client verifies HTTP 200, `text/event-stream`, the expected content,
   and `data: [DONE]`.

## Error Handling

Each failed check returns a concrete error. The CLI prints the error and exits
with status 1. Successful stages emit concise progress lines so a developer can
see where the smoke reached.

## Testing

Tests must cover the reusable smoke runner rather than only the CLI. The main
test runs the local smoke and asserts it succeeds. The CLI stays thin enough to
verify with `go test ./cmd/kittychat-smoke` plus `make smoke-local`.

## Acceptance Criteria

- `make smoke-local` exits 0 and prints progress for daemon connect, route
  discovery, and chat completion.
- The smoke fails if the daemon does not receive the expected chat request.
- The smoke fails if the HTTP response is not SSE or does not contain `[DONE]`.
- `go test ./...` passes.
