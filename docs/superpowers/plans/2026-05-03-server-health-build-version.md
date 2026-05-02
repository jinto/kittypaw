# Server Health Build Version Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every server deployed to `second` exposes build identity through `/health` so a deploy can be verified immediately.

**Architecture:** Keep the health contract local to each service. Go services expose `status`, `version`, and `commit` from package-level build variables populated by `go build -ldflags`; KittyChat keeps its runtime `KITTYCHAT_VERSION` override but falls back to the build value. KittyKakao already has Cargo package version and git hash in `build.rs`, so its health JSON only needs the same `status` key and a deployment smoke check.

**Tech Stack:** Go, chi, Rust/Axum, Fabric deploy scripts, shell smoke scripts.

---

### Task 1: Pin Health Contracts With Tests

**Files:**
- Modify: `apps/kittyapi/cmd/server/main_test.go`
- Modify: `apps/portal/cmd/server/main_test.go`
- Modify: `apps/chat/internal/server/router_test.go`
- Modify: `apps/kakao/tests/integration.rs`

- [ ] Add assertions that `/health` includes `version` and `commit` for KittyAPI, Portal, and Chat.
- [ ] Add assertions that KittyKakao `/health` returns JSON with `status`, `version`, and `commit`.
- [ ] Run focused tests and confirm failures before implementation.

### Task 2: Implement Build Identity

**Files:**
- Modify: `apps/kittyapi/cmd/server/main.go`
- Modify: `apps/portal/cmd/server/main.go`
- Modify: `apps/chat/cmd/kittychat/main.go`
- Modify: `apps/chat/internal/config/config.go`
- Modify: `apps/chat/internal/server/router.go`
- Modify: `apps/kakao/src/routes.rs`

- [ ] Add package build variables for Go server binaries.
- [ ] Return `status`, `version`, and `commit` from each health endpoint.
- [ ] Preserve existing `status` values so existing smoke checks keep working.

### Task 3: Inject Version During Builds

**Files:**
- Modify: `apps/kittyapi/Makefile`
- Modify: `apps/portal/Makefile`
- Modify: `apps/chat/Makefile`
- Modify: `apps/kittyapi/fabfile.py`
- Modify: `apps/portal/fabfile.py`
- Modify: `apps/chat/fabfile.py`

- [ ] Use repo git metadata for `VERSION` and `COMMIT` in build and deploy commands.
- [ ] Keep local default builds functional without requiring git metadata.

### Task 4: Strengthen Deploy Smoke Output

**Files:**
- Modify: `apps/kittyapi/deploy/smoke.sh`
- Modify: `apps/portal/deploy/smoke.sh`
- Modify: `apps/chat/deploy/smoke.sh`
- Create: `apps/kakao/deploy/smoke.sh`
- Modify: `apps/kakao/fabfile.py`

- [ ] Print `/health` version and commit in smoke output.
- [ ] Add Kakao smoke task and run it automatically after Kakao deploy.

### Task 5: Verify, Commit, Deploy

**Files:**
- All files above.

- [ ] Run focused Go/Rust tests.
- [ ] Run `make smoke-local`.
- [ ] Commit only this work, leaving unrelated `apps/kittypaw/cli/chat_tui*` changes untouched.
- [ ] Deploy `kittyapi`, `portal`, `chat`, and `kakao` to `second`.
- [ ] Query each production `/health` and record version/commit in the final report.
