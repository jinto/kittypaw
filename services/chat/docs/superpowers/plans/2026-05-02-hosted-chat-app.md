# Hosted Chat App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a user-facing hosted chat app at `chat.kittypaw.app` with login entry, auth callback token capture, route discovery, and chat completion UI.

**Architecture:** Serve a new static app bundle from `kittychat` under `/`, `/app/`, and `/auth/callback` while keeping `/manual/` as the QA surface. The browser stores the API access token, calls existing same-origin OpenAI-compatible relay endpoints, and handles daemon offline/auth errors clearly. API-side web OAuth redirect is an explicit external contract; this plan implements the chat receiver and app.

**Tech Stack:** Go `embed` + `net/http` router for static assets, vanilla HTML/CSS/JS, Node test runner for pure frontend helpers, existing Go tests/lint/smoke.

---

## Files

- Create `internal/server/web/index.html`: public entry page.
- Create `internal/server/web/app.html`: hosted chat app page.
- Create `internal/server/web/auth-callback.html`: callback receiver page.
- Create `internal/server/web/style.css`: shared product UI styling.
- Create `internal/server/web/shared.js`: auth/token/routing/error helper functions exposed for browser and tests.
- Create `internal/server/web/entry.js`: `/` login-state behavior.
- Create `internal/server/web/callback.js`: callback token capture behavior.
- Create `internal/server/web/app.js`: route discovery and chat behavior.
- Create `internal/server/webtest/shared.test.mjs`: Node tests for pure helpers.
- Modify `internal/server/router.go`: embed and serve web app routes before OpenAI routes.
- Modify `internal/server/router_test.go`: assert root, app, callback, manual routing and no-store headers.
- Keep `internal/server/manual/*` unchanged except shared behavior is not required in this first slice.

## Task 1: Static Route Skeleton

**Files:**
- Create: `internal/server/web/index.html`
- Create: `internal/server/web/app.html`
- Create: `internal/server/web/auth-callback.html`
- Create: `internal/server/web/style.css`
- Modify: `internal/server/router.go`
- Modify: `internal/server/router_test.go`

- [ ] **Step 1: Write failing router tests**

Add tests to `internal/server/router_test.go`:

```go
func TestRouterServesHostedChatEntry(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want html", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", cc)
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="chat-entry"`) {
		t.Fatalf("hosted entry marker missing from body:\n%s", body)
	}
}

func TestRouterServesHostedChatApp(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="chat-app"`) {
		t.Fatalf("hosted app marker missing from body:\n%s", body)
	}
}

func TestRouterServesHostedAuthCallback(t *testing.T) {
	router := NewRouter(Config{
		Version:       "dev",
		OpenAIHandler: openai.NewHandler(nilAuth{}, nilBroker{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="auth-callback"`) {
		t.Fatalf("auth callback marker missing from body:\n%s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/server
```

Expected: FAIL because `/`, `/app/`, and `/auth/callback` are not served by static web assets yet.

- [ ] **Step 3: Add minimal static files and route serving**

Create minimal HTML files with markers and shared CSS. Modify `router.go` to embed `web/*`, add `webFileHandler`, and register:

```go
//go:embed manual/* web/*
var staticAssets embed.FS
```

Serve:

```go
r.Get("/", serveWebFile("web/index.html"))
r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/app/", http.StatusMovedPermanently)
})
r.Get("/app/", serveWebFile("web/app.html"))
r.Get("/auth/callback", serveWebFile("web/auth-callback.html"))
r.Handle("/assets/*", http.StripPrefix("/assets/", webAssetHandler()))
```

Set `X-Content-Type-Options: nosniff` and `Cache-Control: no-store` for HTML and JS/CSS assets.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/server
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/router.go internal/server/router_test.go internal/server/web
git commit -m "feat: add hosted chat static routes"
```

## Task 2: Shared Frontend Helpers

**Files:**
- Create: `internal/server/web/shared.js`
- Create: `internal/server/webtest/shared.test.mjs`

- [ ] **Step 1: Write failing Node tests**

Create tests for:

```js
parseTokenParams("#access_token=a&refresh_token=r&token_type=Bearer&expires_in=900")
formatHTTPError({ status: 503, statusText: "Service Unavailable" }, { error: "device offline" }, "{\"error\":\"device offline\"}")
selectFirstAvailableRoute({ deviceID: "old", accountID: "ghost" }, [{ device_id: "dev", local_accounts: ["jinto"] }])
```

Expected values:

```js
assert.equal(tokens.accessToken, "a");
assert.equal(tokens.refreshToken, "r");
assert.equal(tokens.tokenType, "Bearer");
assert.equal(tokens.expiresIn, 900);
assert.equal(formatHTTPError(...), "HTTP 503 Service Unavailable: device offline");
assert.deepEqual(selection, { deviceID: "dev", accountID: "jinto" });
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
node --test internal/server/webtest/shared.test.mjs
```

Expected: FAIL because `shared.js` does not exist.

- [ ] **Step 3: Implement helper module**

Expose helpers on `window.KittyChatWeb` for browser use and on `module.exports`/VM context for tests:

```js
function parseTokenParams(searchOrHash) { ... }
function formatHTTPError(resp, body, rawText) { ... }
function selectFirstAvailableRoute(current, routes) { ... }
function saveAuth(storage, tokenPayload, now = Date.now()) { ... }
function loadAuth(storage, now = Date.now()) { ... }
function clearAuth(storage) { ... }
```

Keep logic pure except storage helpers.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
node --test internal/server/webtest/shared.test.mjs
node --check internal/server/web/shared.js
```

Expected: PASS and no syntax errors.

- [ ] **Step 5: Commit**

```bash
git add internal/server/web/shared.js internal/server/webtest/shared.test.mjs
git commit -m "feat: add hosted chat frontend helpers"
```

## Task 3: Entry And Callback UX

**Files:**
- Create: `internal/server/web/entry.js`
- Create: `internal/server/web/callback.js`
- Modify: `internal/server/web/index.html`
- Modify: `internal/server/web/auth-callback.html`

- [ ] **Step 1: Write failing static router assertions**

Extend `TestRouterServesHostedChatEntry` to assert:

```go
if body := rr.Body.String(); !strings.Contains(body, `/assets/entry.js`) {
	t.Fatalf("entry script missing from body:\n%s", body)
}
```

Extend `TestRouterServesHostedAuthCallback` to assert:

```go
if body := rr.Body.String(); !strings.Contains(body, `/assets/callback.js`) {
	t.Fatalf("callback script missing from body:\n%s", body)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/server
```

Expected: FAIL until scripts are referenced.

- [ ] **Step 3: Implement entry and callback**

Entry page:

- If `loadAuth(localStorage)` returns a valid token, `location.replace("/app/")`.
- Login button target defaults to `https://api.kittypaw.app/auth/web/google?redirect_uri=<encoded chat callback>`.
- Show a precise note if the endpoint is not live yet: “API web login endpoint pending; use /manual/ for QA.”

Callback page:

- Parse `location.hash` first, then `location.search`.
- If access token exists, `saveAuth`, `history.replaceState(null, "", "/auth/callback")`, then `location.replace("/app/")`.
- If missing, show auth error and link to `/`.

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/server
node --check internal/server/web/entry.js
node --check internal/server/web/callback.js
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/web/index.html internal/server/web/auth-callback.html internal/server/web/entry.js internal/server/web/callback.js internal/server/router_test.go
git commit -m "feat: add hosted chat login receiver"
```

## Task 4: Hosted Chat App UX

**Files:**
- Create: `internal/server/web/app.js`
- Modify: `internal/server/web/app.html`
- Modify: `internal/server/web/style.css`

- [ ] **Step 1: Write failing app script assertion**

Extend `TestRouterServesHostedChatApp`:

```go
if body := rr.Body.String(); !strings.Contains(body, `/assets/app.js`) {
	t.Fatalf("app script missing from body:\n%s", body)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/server
```

Expected: FAIL until app script is referenced.

- [ ] **Step 3: Implement app page**

Implement:

- Auth guard: no valid auth -> `location.replace("/")`.
- Route load on startup and refresh button.
- Device/account/model controls.
- Message list and composer.
- `GET /v1/routes`.
- `GET /nodes/{device}/accounts/{account}/v1/models`.
- `POST /nodes/{device}/accounts/{account}/v1/chat/completions`.
- Logout button clears auth and app state.
- Offline/no-route copy.

Use `formatHTTPError` for all non-2xx responses and never render HTML error bodies as assistant messages.

- [ ] **Step 4: Run tests and syntax checks**

Run:

```bash
go test ./internal/server
node --check internal/server/web/app.js
node --test internal/server/webtest/shared.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/web/app.html internal/server/web/style.css internal/server/web/app.js internal/server/router_test.go
git commit -m "feat: add hosted chat app"
```

## Task 5: Final Verification And Deploy

**Files:**
- No code changes expected.

- [ ] **Step 1: Run full verification**

Run:

```bash
make build
make test
make lint
make smoke-local
node --test internal/server/webtest/shared.test.mjs
```

Expected: all pass.

- [ ] **Step 2: Push and deploy**

Run:

```bash
git push
uv run fab deploy
```

Expected: deploy prints `active` and `/health healthy (prod)`.

- [ ] **Step 3: Prod smoke**

Run:

```bash
curl -fsS https://chat.kittypaw.app/
curl -fsS https://chat.kittypaw.app/app/
curl -fsS https://chat.kittypaw.app/auth/callback
```

Expected: each returns HTML. Then verify the existing daemon route still works with the server token:

```bash
ssh second 'cd /home/jinto/kittychat && set -a && . ./.env && set +a && curl -sS -w "\nstatus=%{http_code}\n" -H "Authorization: Bearer $KITTYCHAT_API_TOKEN" https://chat.kittypaw.app/v1/routes'
```

Expected: `status=200` and a route while local daemon is online.

## Self-Review

- Spec coverage: covers `/`, `/auth/callback`, `/app/`, `/manual/` preservation, storage, errors, API contract gap, tests.
- Placeholder scan: no TBD/TODO language left.
- Scope check: chat-side hosted UX only; API web OAuth redirect endpoint remains explicitly external.
