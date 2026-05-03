# Services Portal Extraction Implementation Plan

> Historical plan snapshot. This document records the implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the logical portal identity surface from `apps/kittyapi` into a separately deployable Go service at `apps/portal`.

**Architecture:** Keep the phase-1 public contract unchanged: `portal.kittypaw.app` owns discovery, OAuth, token issuance, JWKS, and device credentials; `api.kittypaw.app` owns `/v1/*` resource routes. Move identity code and identity-owned tables to `apps/portal`, then remove identity routes from `apps/kittyapi`. Keep services independently deployed and verify the split through shared contracts, fixtures, and smoke tests.

**Tech Stack:** Go 1.26 workspace, chi router, pgx, RS256/JWKS, root JSON contracts, bash smoke scripts, nginx/systemd deployment on `second`.

---

## File Structure

- Create `apps/portal/go.mod`: new module `github.com/kittypaw-app/kittyportal`.
- Create `apps/portal/cmd/server/main.go`: portal process entrypoint.
- Create `apps/portal/internal/config/config.go`: portal env loading. Required: `DATABASE_URL`, `JWT_PRIVATE_KEY_PEM_B64`, OAuth client secrets, `BASE_URL=https://portal.kittypaw.app`, `API_BASE_URL=https://api.kittypaw.app`.
- Create `apps/portal/cmd/server/main.go`: portal routes only: `/health`, `/discovery`, `/.well-known/jwks.json`, `/auth/*`.
- Move identity packages from `apps/kittyapi/internal/auth` to `apps/portal/internal/auth`.
- Move identity-owned model files from `apps/kittyapi/internal/model` to `apps/portal/internal/model`: `user*`, `refresh_token*`, `device*`, and shared DB pool helpers needed by those stores.
- Keep resource-owned model files in `apps/kittyapi/internal/model`: places and addresses.
- Move identity migrations to `apps/portal/migrations`: `001_create_users`, `002_create_refresh_tokens`, `006_create_devices`, `007_add_device_id_to_refresh_tokens`, `008_add_lifecycle_indexes`.
- Keep resource migrations in `apps/kittyapi/migrations`: places, alias overrides, addresses.
- Modify `apps/kittyapi/cmd/server/main.go`: remove auth/discovery/JWKS/device routes, remove JWT signing config, keep `/health` and `/v1/*`.
- Modify `apps/kittyapi/internal/ratelimit`: remove hard dependency on `internal/auth` or keep API traffic anonymous until resource auth is explicitly needed.
- Create `apps/portal/deploy/*` and update root CI/contracts checks.

## Task 1: Portal Module Skeleton

**Files:**
- Create: `apps/portal/go.mod`
- Create: `apps/portal/cmd/server/main.go`
- Create: `apps/portal/internal/config/config.go`
- Create: `apps/portal/cmd/server/main.go`
- Create: `apps/portal/cmd/server/main_test.go`
- Modify: `go.work`

- [ ] **Step 1: Write failing router contract tests**

Add `apps/portal/cmd/server/main_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittyportal/internal/config"
)

func TestDiscoveryReturnsPortalIdentityAndAPIResourceURLs(t *testing.T) {
	cfg := config.LoadForTest()
	cfg.BaseURL = "https://portal.kittypaw.app"
	cfg.APIBaseURL = "https://api.kittypaw.app"
	r := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/discovery", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}
	if body["auth_base_url"] != "https://portal.kittypaw.app/auth" {
		t.Fatalf("auth_base_url = %q", body["auth_base_url"])
	}
	if body["api_base_url"] != "https://api.kittypaw.app" {
		t.Fatalf("api_base_url = %q", body["api_base_url"])
	}
}

func TestPortalDoesNotServeResourceRoutes(t *testing.T) {
	r := NewRouter(config.LoadForTest())
	req := httptest.NewRequest(http.MethodGet, "/v1/geo/resolve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
go test ./apps/portal/cmd/server -count=1
```

Expected: FAIL because `apps/portal` does not exist yet.

- [ ] **Step 3: Implement minimal skeleton**

Create module, config, and router. `NewRouter` should return only `/health` and `/discovery` at this task.

- [ ] **Step 4: Add module to workspace**

Run:

```bash
go work use ./apps/portal
go test ./apps/portal/cmd/server -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.work apps/portal
git commit -m "feat(portal): scaffold identity service"
```

## Task 2: Move JWT Issuer and JWKS Publication

**Files:**
- Move: `apps/kittyapi/internal/auth/jwks*.go` to `apps/portal/internal/auth/`
- Move: `apps/kittyapi/internal/auth/jwt.go`, `scopes.go`, and related tests to `apps/portal/internal/auth/`
- Modify: `contracts/auth/*`
- Modify: `apps/chat/internal/identity/*` tests only if contract drift appears

- [ ] **Step 1: Write failing portal JWKS and issuer tests**

Add tests in `apps/portal/internal/auth/jwt_test.go` asserting:

```go
if auth.Issuer != "https://portal.kittypaw.app/auth" {
	t.Fatalf("Issuer = %q", auth.Issuer)
}
```

Add router test:

```go
req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
```

Expected: 200 JSON JWK Set with one `kid`.

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./apps/portal/internal/auth ./apps/portal/cmd/server -count=1
```

Expected: FAIL because JWKS is not wired in portal yet.

- [ ] **Step 3: Move JWKS/signing code**

Use `git mv` for files that are no longer needed by API. If API still needs token verification later, create a small API-local verifier package in a separate task rather than importing portal internals.

- [ ] **Step 4: Run contract and consumer tests**

```bash
make contracts-check
go test ./apps/portal/internal/auth ./apps/portal/cmd/server -count=1
go test ./apps/chat/internal/identity -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(portal): own jwt issuer and jwks"
```

## Task 3: Move OAuth and Device Identity Routes

**Files:**
- Move: `apps/kittyapi/internal/auth/google*.go`, `github*.go`, `devices*.go`, `refresh*.go`, `state*.go`, `web*.go`, `me.go`, and tests to `apps/portal/internal/auth/`
- Move: identity model stores and tests to `apps/portal/internal/model/`
- Move: identity migrations to `apps/portal/migrations/`
- Modify: `apps/portal/cmd/server/main.go`

- [ ] **Step 1: Write failing route wiring tests**

In `apps/portal/cmd/server/main_test.go`, add checks:

```go
func TestDeviceRoutesAreWired(t *testing.T) {
	r := testRouterWithStores(t)
	cases := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/auth/devices/refresh", http.StatusBadRequest},
		{http.MethodPost, "/auth/devices/pair", http.StatusUnauthorized},
		{http.MethodGet, "/auth/devices", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./apps/portal/cmd/server ./apps/portal/internal/auth ./apps/portal/internal/model -count=1
```

Expected: FAIL because routes and stores are not moved yet.

- [ ] **Step 3: Move identity code and fix imports**

Use `git mv` for identity files. Keep package names stable where possible. Replace module imports from `github.com/kittypaw-app/kittyapi/internal/...` to `github.com/kittypaw-app/kittyportal/internal/...`.

- [ ] **Step 4: Run portal identity tests**

```bash
go test ./apps/portal/... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(portal): move identity routes and stores"
```

## Task 4: Slim API to Resource Server

**Files:**
- Modify: `apps/kittyapi/cmd/server/main.go`
- Modify: `apps/kittyapi/internal/config/config.go`
- Modify: `apps/kittyapi/internal/ratelimit/middleware.go`
- Modify: `apps/kittyapi/migrations/`
- Modify: `apps/kittyapi/deploy/smoke.sh`

- [ ] **Step 1: Write failing API boundary tests**

In `apps/kittyapi/cmd/server/main_test.go`, keep or add:

```go
func TestAPIDoesNotServeIdentityRoutes(t *testing.T) {
	r := testRouter(t)
	for _, path := range []string{"/discovery", "/.well-known/jwks.json", "/auth/google"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, w.Code)
		}
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./apps/kittyapi/cmd/server -count=1
```

Expected: FAIL while API still wires identity routes.

- [ ] **Step 3: Remove identity wiring from API**

Remove OAuth handlers, JWT signing key loading, user/refresh/device stores, and identity groups from API. Keep `/health` and `/v1/*`.

- [ ] **Step 4: Split migrations**

Move identity migrations to portal and renumber only if the migration tool requires a contiguous local sequence. Keep a mapping note in `apps/portal/DEPLOY.md` so production DB history is not ambiguous.

- [ ] **Step 5: Run API tests**

```bash
go test ./apps/kittyapi/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(api): keep only resource routes"
```

## Task 5: Deployment Split on `second`

**Files:**
- Create: `apps/portal/deploy/kittyportal.service`
- Create: `apps/portal/deploy/kittyportal.nginx`
- Create: `apps/portal/deploy/env.example`
- Create: `apps/portal/fabfile.py`
- Modify: `apps/kittyapi/deploy/kittyapi.nginx`
- Modify: `apps/chat/deploy/env.example`

- [ ] **Step 1: Write smoke script expectations**

Portal smoke must check:

```bash
curl https://portal.kittypaw.app/health
curl https://portal.kittypaw.app/discovery
curl https://portal.kittypaw.app/.well-known/jwks.json
```

API smoke must check:

```bash
curl https://api.kittypaw.app/health
curl https://api.kittypaw.app/discovery # 404
curl https://api.kittypaw.app/.well-known/jwks.json # 404
```

- [ ] **Step 2: Implement deploy files**

Use service name `kittyportal`, remote dir `/home/jinto/kittyportal`, port `9714` unless the operator chooses another free port.

- [ ] **Step 3: Run local build/tests**

```bash
go test ./apps/portal/... ./apps/kittyapi/... ./apps/chat/... -count=1
make contracts-check
```

Expected: PASS.

- [ ] **Step 4: Deploy in order**

Deploy portal first, then chat env JWKS URL if needed, then slim API:

```bash
cd apps/portal && DEPLOY_DOMAIN=portal.kittypaw.app fab setup deploy
cd ../chat && fab deploy
cd ../api && DEPLOY_DOMAIN=api.kittypaw.app fab deploy
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore(deploy): split portal service"
```

## Task 6: Cross-Service E2E Harness

**Files:**
- Create: `testkit/e2e/portal_api_chat_test.go` or `scripts/e2e_portal_api_chat.sh`
- Modify: `.github/workflows/*`

- [ ] **Step 1: Add failing harness**

The harness must verify:

- Portal discovery advertises portal auth and API resource separately.
- Portal-issued token has `iss=https://portal.kittypaw.app/auth`.
- Chat verifier accepts portal-issued token using portal JWKS.
- API does not serve identity endpoints.

- [ ] **Step 2: Run and verify RED if services are not all wired**

```bash
go test ./testkit/e2e -count=1
```

- [ ] **Step 3: Implement harness helpers**

Prefer local httptest servers for CI; live smoke remains bash-based and opt-in.

- [ ] **Step 4: Add CI target**

Root CI should run portal, API, chat tests when any file under `contracts/`, `apps/portal/`, `apps/kittyapi/`, `apps/chat/`, or `apps/kittypaw/` changes.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "test(e2e): cover portal api chat contract"
```

## Self-Review

- Spec coverage: Phase 1 contract remains unchanged; phase 2 creates a separate deployable and moves identity ownership out of API.
- Placeholder scan: no task depends on an unspecified endpoint or path; each task names files and verification commands.
- Type consistency: issuer stays `https://portal.kittypaw.app/auth`; API audience stays `https://api.kittypaw.app`; chat audience stays `https://chat.kittypaw.app`.
