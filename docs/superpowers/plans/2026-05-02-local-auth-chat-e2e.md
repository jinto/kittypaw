# Local Auth Chat E2E Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `make e2e-local`, an opt-in Docker-backed E2E that proves Portal login, Chat BFF session access, JWKS verification, device pairing, daemon route discovery, and chat completion relay.

**Architecture:** Keep `make smoke-local` fast and Docker-free. Add a separate root compose file for Postgres and a Go harness in `testkit/e2e` that starts real Portal/Chat binaries with a fake OAuth provider. Add only narrow Portal OAuth endpoint configuration seams.

**Tech Stack:** Go, Docker Compose, PostgreSQL 17, golang-migrate, Portal/Chat binaries, Kittypaw chatrelay connector.

---

### Task 1: Portal OAuth Endpoint Overrides

**Files:**
- Modify: `apps/portal/internal/auth/google.go`
- Modify: `apps/portal/internal/auth/google_test.go`
- Modify: `apps/portal/internal/auth/web_test.go`
- Modify: `apps/portal/internal/config/config.go`
- Modify: `apps/portal/internal/config/config_test.go`
- Modify: `apps/portal/cmd/server/main.go`

- [ ] Add `GoogleAuthURL`, `GoogleTokenURL`, and `GoogleUserInfoURL` to Portal config.
- [ ] Add `googleAuthURL()` to `OAuthHandler`, defaulting to Google's production authorization URL.
- [ ] Replace hardcoded Google authorization URLs in CLI/web login handlers with `h.googleAuthURL()`.
- [ ] Wire config values into the `OAuthHandler` in `NewRouter`.
- [ ] Add tests for default URLs and override URLs.
- [ ] Run:

```bash
go test ./internal/auth ./internal/config ./cmd/server -count=1
```

### Task 2: Root E2E Compose and Runner

**Files:**
- Create: `docker-compose.e2e.yml`
- Modify: `Makefile`
- Create: `scripts/e2e-local.sh`
- Modify: `scripts/README.md`

- [ ] Add a disposable `postgres-e2e` service on host port `15433`.
- [ ] Add `make e2e-local`.
- [ ] Script starts Compose, runs the Go harness, and tears down DB unless `KITTY_E2E_KEEP_DB=1`.
- [ ] Script exports:

```bash
DATABASE_URL=postgres://kittypaw:kittypaw@localhost:15433/kitty_e2e_test?sslmode=disable
```

- [ ] Run shell syntax check:

```bash
bash -n scripts/e2e-local.sh
```

### Task 3: Go E2E Harness Module

**Files:**
- Create: `testkit/e2e/go.mod`
- Create: `testkit/e2e/e2e_test.go`
- Modify: `go.work`

- [ ] Add `testkit/e2e` to `go.work`.
- [ ] The harness refuses `DATABASE_URL` values that do not contain `_test`.
- [ ] The harness migrates Portal DB from `apps/portal/migrations`.
- [ ] The harness starts fake Google, Portal, and Chat.
- [ ] The harness gets a user access token through Portal normal Google flow.
- [ ] The harness pairs a device with `/auth/devices/pair`.
- [ ] The harness connects `github.com/jinto/kittypaw/remote/chatrelay.Connector`.
- [ ] The harness completes the browser Chat BFF login flow with a cookie jar.
- [ ] The harness asserts no browser request to `/app/api/*` includes `Authorization`.
- [ ] The harness verifies routes include the paired device/account.
- [ ] The harness posts a chat completion through Chat BFF and sees the fake daemon SSE response.

### Task 4: Verification and Review

**Files:**
- Review all changed files.

- [ ] Run fast unit checks for changed Go packages:

```bash
go test ./internal/auth ./internal/config ./cmd/server -count=1
```

- [ ] Run harness tests through the script:

```bash
make e2e-local
```

- [ ] Run root smoke if local time permits:

```bash
make smoke-local
```

- [ ] Check docs and scripts match the final command names.
- [ ] Confirm no unrelated dirty files were modified.
