# KittyKakao Go Migration Implementation Plan

> Historical plan snapshot. This document records an app-local implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and the app README/DEPLOY docs for the current live shape.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `apps/kakao` Rust relay with a Go service that preserves the existing Kakao HTTP, WebSocket, SQLite, and deployment contracts.

**Architecture:** `apps/kakao` becomes an independent Go module with `cmd/kittykakao` for process wiring, `internal/config` for environment loading, `internal/relay` for wire DTOs, `internal/store` for private SQLite persistence, and `internal/server` for HTTP/WebSocket runtime behavior. The implementation uses Chi, coder/websocket, and modernc SQLite so local and production builds match the other hosted Go apps.

**Tech Stack:** Go 1.25+, `github.com/go-chi/chi/v5`, `github.com/coder/websocket`, `github.com/google/uuid`, `modernc.org/sqlite`, `database/sql`, `net/http`.

---

### Task 1: Add Go Module And Behavior Tests

**Files:**
- Create: `go.mod`
- Create: `internal/config/config_test.go`
- Create: `internal/relay/types_test.go`
- Create: `internal/store/store_test.go`
- Create: `internal/server/server_test.go`

- [ ] **Step 1: Create `go.mod` with the target dependencies**

```go
module github.com/kittypaw-app/kittykakao

go 1.25.0

require (
	github.com/coder/websocket v1.8.14
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	modernc.org/sqlite v1.48.2
)
```

- [ ] **Step 2: Write config tests**

Create `internal/config/config_test.go` with tests named:

```go
func TestLoadDefaults(t *testing.T)
func TestLoadReadsConfiguredValues(t *testing.T)
```

The tests must verify default daily/monthly limits, `relay.db`, `0.0.0.0:8787`,
and configured env overrides for all existing Kakao env variables.

- [ ] **Step 3: Write relay JSON tests**

Create `internal/relay/types_test.go` with tests named:

```go
func TestKakaoPayloadDeserializes(t *testing.T)
func TestKakaoPayloadWithoutCallback(t *testing.T)
func TestWSOutgoingSerializesSnakeCase(t *testing.T)
func TestWSIncomingDeserializesImageFields(t *testing.T)
func TestKakaoTextSerializesSimpleText(t *testing.T)
func TestKakaoImageSerializesSimpleImage(t *testing.T)
func TestKakaoAsyncAckUsesCamelCase(t *testing.T)
```

The expected JSON names are `userRequest`, `callbackUrl`, `simpleText`,
`simpleImage`, `imageUrl`, `altText`, `useCallback`, `user_id`, `image_url`, and
`image_alt`.

- [ ] **Step 4: Write SQLite store tests**

Create `internal/store/store_test.go` with tests named:

```go
func TestTokenRoundTrip(t *testing.T)
func TestUserMappingCRUD(t *testing.T)
func TestKillswitchToggle(t *testing.T)
func TestPendingPutTakeAtomic(t *testing.T)
func TestRateLimitIncrementsAndCaps(t *testing.T)
func TestStatsMatchAfterIncrements(t *testing.T)
func TestCleanupExpiredPendingRemovesOld(t *testing.T)
```

Use `Open(":memory:")`, `context.Background()`, and assert that a second
`TakePending` for the same action returns nil.

- [ ] **Step 5: Write server integration tests**

Create `internal/server/server_test.go` with tests named:

```go
func TestHappyPathRegisterPairWebhookWS(t *testing.T)
func TestExpiredPairCodeRejection(t *testing.T)
func TestRateLimitExceeded(t *testing.T)
func TestKillswitchBlocksWebhookWSStays(t *testing.T)
func TestSSRFGuardRejectsNonKakao(t *testing.T)
func TestUnpairedUserGetsGuide(t *testing.T)
func TestInvalidTokenWSReturns401(t *testing.T)
func TestOfflineSessionReturnsOfflineMessage(t *testing.T)
func TestHealthEndpoint(t *testing.T)
func TestAdminStats(t *testing.T)
func TestWebhookRequiresAuth(t *testing.T)
func TestSSRFGuardAllowsKakao(t *testing.T)
func TestSSRFGuardBlocksOthers(t *testing.T)
```

Use `httptest.NewServer`, `github.com/coder/websocket`, and `wsjson`.
Inject a test `http.Client` transport into state so Kakao callback dispatch does
not touch the network.

- [ ] **Step 6: Run tests and verify RED**

Run:

```bash
go test ./... -count=1
```

Expected: FAIL because production packages and symbols do not exist yet.

### Task 2: Implement Config, Relay DTOs, And Store

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/relay/types.go`
- Create: `internal/store/store.go`
- Modify: `go.sum`

- [ ] **Step 1: Implement config loading**

`internal/config.Config` must expose:

```go
type Config struct {
	WebhookSecret string
	DailyLimit    uint64
	MonthlyLimit  uint64
	ChannelURL    string
	DatabasePath  string
	BindAddr      string
}
```

`Load()` returns defaults and parses integer env vars with fallback behavior.

- [ ] **Step 2: Implement relay DTOs and response builders**

`internal/relay` must define the Korean message constants, Kakao payload
structs, response structs, `Text`, `Image`, `AsyncAck`, `WSOutgoing`,
`WSIncoming`, `PendingContext`, and API response structs.

- [ ] **Step 3: Implement SQLite store**

`internal/store.Store` must open SQLite via `modernc.org/sqlite`, create the
existing tables, insert the default killswitch row, and implement:

```go
TokenExists(ctx context.Context, token string) (bool, error)
PutToken(ctx context.Context, token string) error
GetUserMapping(ctx context.Context, kakaoID string) (string, bool, error)
PutUserMapping(ctx context.Context, kakaoID, token string) error
DeleteUserMapping(ctx context.Context, kakaoID string) error
GetKillswitch(ctx context.Context) (bool, error)
SetKillswitch(ctx context.Context, enabled bool) error
PutPending(ctx context.Context, actionID string, pending relay.PendingContext) error
TakePending(ctx context.Context, actionID string) (relay.PendingContext, bool, error)
CheckRateLimit(ctx context.Context, dailyLimit, monthlyLimit uint64) (relay.RateLimitResult, error)
GetStats(ctx context.Context) (relay.Stats, error)
CleanupExpiredPending(ctx context.Context, maxAgeSeconds int64) (uint64, error)
Close() error
```

- [ ] **Step 4: Run unit tests for config, relay, and store**

Run:

```bash
go test ./internal/config ./internal/relay ./internal/store -count=1
```

Expected: PASS.

### Task 3: Implement HTTP/WebSocket Server

**Files:**
- Create: `internal/server/state.go`
- Create: `internal/server/cache.go`
- Create: `internal/server/router.go`
- Create: `internal/server/ws.go`
- Create: `internal/server/metrics.go`

- [ ] **Step 1: Implement runtime state**

State must hold config, store, HTTP client, version, commit, session map,
five-minute pair-code TTL cache, and ten-minute paired-marker TTL cache.

- [ ] **Step 2: Implement router and HTTP handlers**

Handlers must preserve:

```text
POST /register
GET /pair-status/{token}
POST /webhook?secret=...
POST /admin/killswitch?secret=...
GET /admin/stats?secret=...
GET /health
```

The webhook handler must follow the ordered flow in the design spec.

- [ ] **Step 3: Implement WebSocket handling**

`GET /ws/{token}` validates the token, accepts the WebSocket, registers the
session, writes `{id,text,user_id}` frames to the client, reads `{id,text}` reply
frames, atomically claims pending callbacks, and dispatches Kakao callback
responses.

- [ ] **Step 4: Implement metrics and URL guards**

`AdminStats` must include daily/monthly usage, killswitch, `ws_sessions`,
`rss_bytes`, and `fd_count`. `IsAllowedCallbackHost` must allow HTTPS
`kakao.com`, `*.kakao.com`, `kakaoenterprise.com`, and
`*.kakaoenterprise.com` only.

- [ ] **Step 5: Run server tests**

Run:

```bash
go test ./internal/server -count=1
```

Expected: PASS.

### Task 4: Add Main Binary, Build, And Deployment Updates

**Files:**
- Create: `cmd/kittykakao/main.go`
- Create: `cmd/kittykakao/main_test.go`
- Create: `Makefile`
- Modify: `fabfile.py`
- Modify: `deploy/env.example`
- Modify: `README.md`
- Modify: `README.ko.md`
- Modify: `DEPLOY.md`
- Modify: `CLAUDE.md`
- Modify outside app with permission if required: `../../go.work`
- Modify outside app with permission if required: `../../scripts/smoke-local.sh`

- [ ] **Step 1: Implement process entrypoint**

`cmd/kittykakao/main.go` must load config, initialize slog logging from
`LOG_LEVEL` with legacy `RUST_LOG` fallback, build state, start the sweeper,
serve TCP or Unix sockets, handle SIGINT/SIGTERM, and expose `version`/`commit`
via `-ldflags`.

- [ ] **Step 2: Add main tests**

`cmd/kittykakao/main_test.go` must test build metadata fallback and Unix socket
path detection for both `/path.sock` and `unix:/path.sock`.

- [ ] **Step 3: Add Makefile**

Targets:

```make
build: go build -ldflags "$(LDFLAGS)" -o kittykakao ./cmd/kittykakao
test: go test ./... -count=1
lint: golangci-lint run ./...
fmt: gofmt -w cmd internal
run: go run ./cmd/kittykakao
smoke: bash deploy/smoke.sh
clean: rm -f kittykakao kittykakao-linux
```

- [ ] **Step 4: Update deployment script**

Replace Rust build logic in `fabfile.py` with `GOOS=linux GOARCH=amd64
CGO_ENABLED=0 go build ... -o kittykakao-linux ./cmd/kittykakao`. Keep setup,
deploy, smoke, rollback, status, and logs behavior.

- [ ] **Step 5: Update documentation and root smoke wiring**

Replace Cargo/Rust wording with Go/Make commands, switch env docs to
`LOG_LEVEL` while noting `RUST_LOG` compatibility, add `./apps/kakao` to
`go.work`, and replace root smoke Rust test with `go test ./apps/kakao/...`.

- [ ] **Step 6: Run build and package tests**

Run:

```bash
go test ./... -count=1
make build
```

Expected: both PASS.

### Task 5: Remove Rust Implementation

**Files:**
- Delete: `Cargo.toml`
- Delete: `Cargo.lock`
- Delete: `build.rs`
- Delete: `src/main.rs`
- Delete: `src/lib.rs`
- Delete: `src/routes.rs`
- Delete: `src/state.rs`
- Delete: `src/store.rs`
- Delete: `src/types.rs`
- Delete: `tests/integration.rs`

- [ ] **Step 1: Remove Rust files**

Use `git rm` for tracked Rust files after Go tests cover the behavior.

- [ ] **Step 2: Run final local verification**

Run:

```bash
go test ./... -count=1
make build
bash -n deploy/smoke.sh
python3 -m py_compile fabfile.py
```

Expected: all PASS.

### Task 6: Fabric Research, Self-Review, Fixes, And Commit

**Files:**
- Modify if needed: `DEPLOY.md`
- Modify if needed: `fabfile.py`
- Commit all migration changes after review and verification.

- [ ] **Step 1: Research Fabric necessity from local deploy files**

Compare current `fabfile.py`, `deploy/kittykakao.service`,
`deploy/kittykakao.nginx`, `deploy/env.example`, and neighboring Go app
`fabfile.py` files. Answer whether first install needs Fabric or can be
expressed as plain SSH/SCP/systemctl/nginx commands.

- [ ] **Step 2: Perform code review**

Review changed files for:

- Contract mismatches in JSON field names and route paths.
- Production DB compatibility.
- Network access in tests.
- WebSocket lifecycle leaks.
- Deployment behavior changes beyond build language.
- Root smoke coverage.

- [ ] **Step 3: Fix review findings and re-run verification**

Run:

```bash
go test ./... -count=1
make build
bash -n deploy/smoke.sh
python3 -m py_compile fabfile.py
```

If root files were updated, also run from repository root:

```bash
make contracts-check
go test ./apps/kakao/... -count=1
```

- [ ] **Step 4: Commit migration**

Commit message:

```bash
git commit -m "feat(kakao): migrate relay to go"
```

Do not run production deployment commands. Wait for explicit user approval
before any `fab setup`, `fab deploy`, or direct server modification.
