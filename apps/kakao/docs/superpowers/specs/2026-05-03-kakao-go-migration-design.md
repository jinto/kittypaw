# KittyKakao Go Migration Design

## Context

`apps/kakao` is the only hosted Kitty app implemented in Rust. The rest of the
hosted apps in this monorepo (`apps/chat`, `apps/kittyapi`, and `apps/portal`)
are Go services with simple static Linux builds. Keeping Kakao in Rust adds
separate toolchain and deployment paths for a small relay service.

The Kakao app owns the Kakao OpenBuilder webhook, Kakao async callback dispatch,
Kakao-specific pairing, relay WebSocket sessions, and its private SQLite store.
The migration must preserve those app boundaries and must not move runtime code
into another app.

## Goals

- Replace the Rust implementation with a Go implementation under `apps/kakao`.
- Preserve the existing HTTP API, WebSocket frame contract, environment
  variables, SQLite table names, and deploy-time binary name.
- Align local build, test, and deploy commands with the other Go services.
- Remove Rust-specific source, lockfile, build script, and test execution from
  this app after the Go implementation is in place.

## Non-Goals

- Changing Kakao OpenBuilder behavior or the upstream Kakao payload contract.
- Changing the `contracts/kakao-relay` wire schema.
- Sharing runtime packages with `apps/chat`, `apps/kittyapi`, `apps/portal`, or
  `apps/kittypaw`.
- Changing the production database schema beyond compatible `CREATE TABLE IF NOT
  EXISTS` initialization.
- Reworking pairing UX, limits policy, or admin authentication.

## Architecture

`apps/kakao` becomes an independent Go module:

```text
apps/kakao/
  cmd/kittykakao/main.go
  internal/config/
  internal/server/
  internal/store/
  internal/relay/
  go.mod
  Makefile
```

`cmd/kittykakao` loads config, initializes JSON logging, wires the store and
router, starts the pending-callback sweeper, and serves HTTP over either TCP or a
Unix socket. The binary remains named `kittykakao`.

The HTTP router uses `github.com/go-chi/chi/v5`, matching the surrounding Go
services. WebSocket support uses `github.com/coder/websocket`, matching
`apps/chat`. SQLite uses `modernc.org/sqlite` so deploy builds can stay
`CGO_ENABLED=0`.

## Components

### Config

`internal/config` reads the existing environment variables:

- `WEBHOOK_SECRET`
- `DAILY_LIMIT`
- `MONTHLY_LIMIT`
- `CHANNEL_URL`
- `DATABASE_PATH`
- `BIND_ADDR`

Defaults stay compatible with the Rust app:

- `DAILY_LIMIT=10000`
- `MONTHLY_LIMIT=100000`
- `DATABASE_PATH=relay.db`
- `BIND_ADDR=0.0.0.0:8787`

`WEBHOOK_SECRET` may be empty at startup, but webhook and admin requests must
then fail auth.

### Store

`internal/store` owns the private SQLite database. It creates and uses the same
tables:

- `tokens`
- `user_mappings`
- `killswitch`
- `pending_callbacks`
- `rate_counters`

The store exposes methods equivalent to the Rust trait: token registration,
Kakao user mapping, killswitch toggling, pending callback put/take/cleanup, rate
limit accounting, and stats. For `:memory:` tests it pins the connection pool to
one connection so all goroutines see the same database.

### Relay Types

`internal/relay` contains Kakao request/response DTOs, WebSocket frame DTOs, and
message constants. JSON field names remain byte-compatible where externally
visible:

- `userRequest` and `callbackUrl` for inbound Kakao payloads.
- `simpleText`, `simpleImage`, `imageUrl`, `altText` for Kakao responses.
- `useCallback` for async acknowledgements.
- `user_id`, `image_url`, and `image_alt` for relay WebSocket frames.

### Server

`internal/server` owns runtime state and routes:

- `POST /register`
- `GET /pair-status/{token}`
- `POST /webhook?secret=...`
- `GET /ws/{token}`
- `POST /admin/killswitch?secret=...`
- `GET /admin/stats?secret=...`
- `GET /health`

Runtime state includes the store, active WebSocket sessions keyed by relay token,
in-memory pair codes with five-minute TTL, in-memory paired markers with
ten-minute TTL, config, and an HTTP client with redirects disabled.

The webhook flow remains:

1. Validate `secret`.
2. Reject malformed required fields.
3. Enforce killswitch.
4. Check and increment rate counters.
5. Treat six-digit utterances as pairing codes.
6. Require Kakao callback URL for non-pairing messages.
7. Resolve the Kakao user mapping.
8. Require an online WebSocket session.
9. Restrict callback URLs to Kakao-owned HTTPS hosts.
10. Store pending callback context.
11. Send `{id,text,user_id}` to the paired WebSocket client.
12. Return Kakao async callback acknowledgement.

Client WebSocket replies take the pending callback atomically and dispatch either
a `simpleText` response or a `simpleImage` response when `image_url` is public
HTTPS. Callback dispatch failures are logged and do not crash the session.

## Compatibility

The migration must preserve:

- Existing relay registration clients in `apps/kittypaw/core/relay.go`.
- Existing Kakao channel WebSocket consumer behavior in `apps/kittypaw/channel`.
- Existing production `.env` files.
- Existing systemd and nginx config expectations.
- Existing SQLite database files using the current table layout.
- Existing `/health` body shape: `status`, `version`, `commit`.

Build metadata uses Go `-ldflags` with `main.version` and `main.commit`, matching
the other Go apps.

## Deployment

`fabfile.py` changes from Rust `cross` or Docker builds to a Go static Linux
build:

```text
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "...version/commit..." -o kittykakao-linux ./cmd/kittykakao
```

The upload, restart, smoke, rollback, status, and logs tasks keep their current
operator-facing behavior. `deploy/kittykakao.service`, nginx, env example, and
smoke script remain compatible, with documentation updated from `cargo` commands
to `go` or `make` commands.

The root `go.work` includes `./apps/kakao`. The root smoke script stops running
Rust tests for Kakao and instead runs `go test ./apps/kakao/...`.

## Testing

Tests are ported before or alongside implementation to pin behavior:

- Config default tests.
- Kakao payload and response JSON tests.
- WebSocket frame JSON tests.
- SSRF guard tests.
- SQLite store tests for token, mapping, killswitch, pending callback, rate
  limit, stats, and cleanup.
- HTTP/WebSocket integration tests for register-pair-webhook-ws happy path,
  invalid pair code, rate limit, killswitch, SSRF rejection, unpaired user,
  invalid WebSocket token, offline session, health, admin stats, and auth.

Verification commands:

```bash
go test ./...
make build
bash deploy/smoke.sh
```

Root smoke verification is expected to run through `make smoke-local` from the
repository root after the migration is complete.

## Rollout

This is a source migration with a stable binary name and stable runtime
interfaces. The deployment still uploads `kittykakao` to the same remote path.
Rollback remains the existing binary backup flow in `fabfile.py`; if production
deploy needs rollback immediately after this migration, `fab rollback` restores
the previous uploaded binary regardless of implementation language.
