# Route Discovery Implementation Plan

> Historical plan snapshot. This document records an app-local implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and the app README/DEPLOY docs for the current live shape.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an authenticated endpoint that lets a user discover which daemon device/account routes are currently online.

**Architecture:** The broker owns active daemon connection state, so it exposes a read-only snapshot filtered by `user_id`. The existing OpenAI-compatible HTTP handler exposes `GET /v1/routes`, authenticates with the same API credentials, applies optional device/account restrictions from the principal, and returns stable JSON.

**Tech Stack:** Go, chi, existing `broker`, `openai`, `identity`, and `protocol` packages.

---

### Task 1: Broker Route Snapshot

**Files:**
- Modify: `internal/broker/broker.go`
- Modify: `internal/broker/broker_test.go`

- [ ] **Step 1: Write failing broker snapshot tests**

Add tests proving a snapshot:
- returns only routes for the requested `user_id`
- includes active `local_accounts` and `capabilities`
- returns copied slices so callers cannot mutate broker state
- is sorted for stable API output

- [ ] **Step 2: Run broker tests**

Run: `go test ./internal/broker -run TestBrokerRoutes -count=1`

Expected: fail because the snapshot API does not exist.

- [ ] **Step 3: Implement minimal broker snapshot**

Add:

```go
type Route struct {
    DeviceID        string
    LocalAccountIDs []string
    Capabilities    []protocol.Operation
}

func (b *Broker) Routes(userID string) []Route
```

The method must lock the broker, read only matching `userID` entries, copy and sort account/capability slices, sort routes by `device_id`, and return the copied slice.

- [ ] **Step 4: Re-run broker tests**

Run: `go test ./internal/broker -run TestBrokerRoutes -count=1`

Expected: pass.

### Task 2: HTTP Route Discovery

**Files:**
- Modify: `internal/openai/handler.go`
- Modify: `internal/openai/handler_test.go`
- Modify: `README.md`

- [ ] **Step 1: Write failing handler tests**

Add tests proving `GET /v1/routes`:
- requires auth
- requires either `models:read` or `chat:relay`
- passes the authenticated `user_id` to the broker
- filters by optional principal `device_id` and `account_id`
- returns `object: "list"` with route data

- [ ] **Step 2: Run handler tests**

Run: `go test ./internal/openai -run TestRoutes -count=1`

Expected: fail because `/v1/routes` is not registered.

- [ ] **Step 3: Implement route handler**

Add `Routes(userID string) []broker.Route` to the handler's broker interface, register `GET /v1/routes`, authenticate, check route discovery scope, filter restricted principals, and encode JSON.

- [ ] **Step 4: Re-run handler tests**

Run: `go test ./internal/openai -run TestRoutes -count=1`

Expected: pass.

### Task 3: Full Verification and Release

**Files:**
- Modify only files above unless verification reveals a tightly related issue.

- [ ] **Step 1: Run full verification**

Run:

```bash
make lint
make build
make test
git diff --check
```

Expected: all pass.

- [ ] **Step 2: Commit, push, deploy**

Commit message:

```bash
git commit -m "feat: add route discovery endpoint"
```

Deploy:

```bash
DEPLOY_DOMAIN=chat.kittypaw.app uv run fab deploy
```
