# KittyPaw Connect Gmail OAuth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add KittyPaw Connect on `connect.kittypaw.app` inside the existing portal binary, then connect Gmail OAuth tokens to local KittyPaw accounts for a read-only mail workflow.

**Architecture:** Keep `apps/portal` as the single deployed identity/connect binary. Add a separate `internal/connect` package and Connect host boundary instead of renaming or splitting the service. Portal brokers OAuth and refreshes tokens but does not persist Gmail tokens in v1; local KittyPaw stores account-scoped `oauth-gmail` credentials and injects fresh access tokens into packages or MCP servers.

**Tech Stack:** Go, chi, pgx-backed portal model where needed, in-memory short-lived code/state stores, Google OAuth 2.0 web-server flow with PKCE, account-scoped `core.SecretsStore`, existing MCP CommandTransport.

---

## File Map

Portal:

- Modify `apps/portal/internal/config/config.go`: add `ConnectBaseURL` and
  Connect-specific Google OAuth settings.
- Modify `apps/portal/internal/config/config_test.go`: env/default tests.
- Modify `apps/portal/cmd/server/main.go`: Connect host boundary, discovery
  field, and route group.
- Modify `apps/portal/cmd/server/main_test.go`: host split and route wiring tests.
- Modify `apps/portal/cmd/server/web.go`: optional Connect landing handler or separate file.
- Create `apps/portal/internal/connect/types.go`: token response and provider metadata.
- Create `apps/portal/internal/connect/code_store.go`: one-time Connect code store.
- Create `apps/portal/internal/connect/gmail.go`: Gmail auth URL, token exchange, refresh.
- Create `apps/portal/internal/connect/handler.go`: `/connect/gmail/*` and exchange handlers.
- Create tests beside each new Connect file.
- Modify `apps/portal/deploy/env.example`, `apps/portal/DEPLOY.md`, `apps/portal/deploy/kittyportal.nginx`, `apps/portal/fabfile.py`: dual-host deployment.
- Reuse `apps/portal/internal/auth.StateStore.CreateWithMeta` and
  `ConsumeMeta` for Connect OAuth state metadata. Do not create a second state
  abstraction unless tests prove the existing store cannot carry Connect mode
  and callback-port metadata.

KittyPaw:

- Modify `apps/kittypaw/core/discovery.go`: parse `connect_base_url`.
- Modify `apps/kittypaw/core/discovery_test.go`: discovery parsing tests.
- Modify `apps/kittypaw/core/api_token.go`: persist and resolve Connect base URL.
- Modify `apps/kittypaw/core/api_token_test.go`: Connect URL storage tests.
- Create `apps/kittypaw/core/oauth_service_token.go`: local external OAuth token manager.
- Test `apps/kittypaw/core/oauth_service_token_test.go`.
- Modify `apps/kittypaw/cli/main.go`: register `connect` command group.
- Create `apps/kittypaw/cli/cmd_connect.go`: `kittypaw connect gmail`.
- Test `apps/kittypaw/cli/cmd_connect_test.go`.
- Modify `apps/kittypaw/engine/executor.go`: refresh `oauth-gmail/access_token` source-bound config.
- Test `apps/kittypaw/engine/executor_test.go` or a focused package execution test.
- Modify `apps/kittypaw/mcp/registry.go`: optional dynamic env resolver for OAuth tokens.
- Test `apps/kittypaw/mcp/registry_test.go`.

Docs:

- Update `apps/portal/DEPLOY.md`.
- Add a Connect operator note if the implementation reaches Gmail verification.

---

## Task 1: Connect Host, Config, And Discovery Skeleton

**Files:**
- Modify: `apps/portal/internal/config/config.go`
- Modify: `apps/portal/internal/config/config_test.go`
- Modify: `apps/portal/cmd/server/main.go`
- Modify: `apps/portal/cmd/server/main_test.go`
- Modify: `apps/portal/cmd/server/web.go`
- Modify: `apps/portal/deploy/env.example`
- Modify: `apps/portal/DEPLOY.md`
- Modify: `apps/portal/deploy/kittyportal.nginx`
- Modify: `apps/portal/fabfile.py`

- [ ] **Step 1: Write failing config tests**

Add tests asserting:

```go
func TestConfig_LoadConnectBaseURL(t *testing.T) {
    pemStr := generatePEM(t, 2048)
    b64 := base64.StdEncoding.EncodeToString([]byte(pemStr))

    cfg, err := loadWithEnv(t, map[string]string{
        "JWT_PRIVATE_KEY_PEM_B64":     b64,
        "BASE_URL":                    "https://portal.kittypaw.app",
        "CONNECT_BASE_URL":            "https://connect.kittypaw.app",
        "CONNECT_GOOGLE_CLIENT_ID":     "connect-client-id",
        "CONNECT_GOOGLE_CLIENT_SECRET": "connect-secret",
    })
    if err != nil {
        t.Fatalf("Load: %v", err)
    }
    if cfg.ConnectBaseURL != "https://connect.kittypaw.app" {
        t.Fatalf("ConnectBaseURL = %q", cfg.ConnectBaseURL)
    }
    if cfg.ConnectGoogleClientID != "connect-client-id" {
        t.Fatalf("ConnectGoogleClientID = %q", cfg.ConnectGoogleClientID)
    }
    if cfg.ConnectGoogleClientSecret != "connect-secret" {
        t.Fatalf("ConnectGoogleClientSecret = %q", cfg.ConnectGoogleClientSecret)
    }
}

func TestConfig_LoadForTestConnectBaseURL(t *testing.T) {
    cfg := config.LoadForTest()
    if cfg.ConnectBaseURL == "" {
        t.Fatal("ConnectBaseURL should default in tests")
    }
}
```

- [ ] **Step 2: Run config tests and verify failure**

Run:

```bash
cd apps/portal && go test ./internal/config -run ConnectBaseURL -count=1
```

Expected: fails because `Config.ConnectBaseURL` and Connect Google fields do not
exist.

- [ ] **Step 3: Add config field**

Add:

```go
ConnectBaseURL            string
ConnectGoogleClientID     string
ConnectGoogleClientSecret string
ConnectGoogleAuthURL      string
ConnectGoogleTokenURL     string
ConnectGoogleUserInfoURL string
```

Load from:

```go
ConnectBaseURL:            strings.TrimRight(env("CONNECT_BASE_URL", ""), "/"),
ConnectGoogleClientID:     os.Getenv("CONNECT_GOOGLE_CLIENT_ID"),
ConnectGoogleClientSecret: os.Getenv("CONNECT_GOOGLE_CLIENT_SECRET"),
ConnectGoogleAuthURL:      os.Getenv("CONNECT_GOOGLE_AUTH_URL"),
ConnectGoogleTokenURL:     os.Getenv("CONNECT_GOOGLE_TOKEN_URL"),
ConnectGoogleUserInfoURL:  os.Getenv("CONNECT_GOOGLE_USERINFO_URL"),
```

In `LoadForTest`, set:

```go
ConnectBaseURL:            "https://connect.kittypaw.app",
ConnectGoogleClientID:     "connect-client-id",
ConnectGoogleClientSecret: "connect-secret",
```

- [ ] **Step 4: Write failing route/host tests**

In `apps/portal/cmd/server/main_test.go`, add tests:

```go
func TestConnectHomeEndpoint(t *testing.T) {
    cfg := config.LoadForTest()
    cfg.BaseURL = "https://portal.kittypaw.app"
    cfg.APIBaseURL = "https://api.kittypaw.app"
    cfg.ConnectBaseURL = "https://connect.kittypaw.app"
    r, cleanup := NewRouter(cfg, nil, nil, nil)
    t.Cleanup(cleanup)

    req := httptest.NewRequest(http.MethodGet, "/connect", nil)
    req.Host = "connect.kittypaw.app"
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
    }
    if !strings.Contains(w.Body.String(), "KittyPaw Connect") {
        t.Fatalf("connect page missing brand: %s", w.Body.String())
    }
}

func TestConnectHostRootShowsConnectHome(t *testing.T) {
    cfg := config.LoadForTest()
    cfg.BaseURL = "https://portal.kittypaw.app"
    cfg.APIBaseURL = "https://api.kittypaw.app"
    cfg.ConnectBaseURL = "https://connect.kittypaw.app"
    r, cleanup := NewRouter(cfg, nil, nil, nil)
    t.Cleanup(cleanup)

    req := httptest.NewRequest(http.MethodGet, "/", nil)
    req.Host = "connect.kittypaw.app"
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
    }
    if !strings.Contains(w.Body.String(), "KittyPaw Connect") {
        t.Fatalf("connect root missing brand: %s", w.Body.String())
    }
}

func TestConnectRoutesOnlyServedOnConnectHost(t *testing.T) {
    cfg := config.LoadForTest()
    cfg.BaseURL = "https://portal.kittypaw.app"
    cfg.APIBaseURL = "https://api.kittypaw.app"
    cfg.ConnectBaseURL = "https://connect.kittypaw.app"
    r, cleanup := NewRouter(cfg, nil, nil, nil)
    t.Cleanup(cleanup)

    req := httptest.NewRequest(http.MethodGet, "/connect", nil)
    req.Host = "portal.kittypaw.app"
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    if w.Code != http.StatusNotFound {
        t.Fatalf("portal host status = %d, want 404", w.Code)
    }
}

func TestConnectRoutesStayHostBoundWhenAPIHostIsCollapsed(t *testing.T) {
    cfg := config.LoadForTest()
    cfg.BaseURL = "https://portal.kittypaw.app"
    cfg.APIBaseURL = cfg.BaseURL
    cfg.ConnectBaseURL = "https://connect.kittypaw.app"
    r, cleanup := NewRouter(cfg, nil, nil, nil)
    t.Cleanup(cleanup)

    req := httptest.NewRequest(http.MethodGet, "/connect", nil)
    req.Host = "portal.kittypaw.app"
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    if w.Code != http.StatusNotFound {
        t.Fatalf("portal host status = %d, want 404", w.Code)
    }
}

func TestIdentityRoutesNotServedOnConnectHost(t *testing.T) {
    cfg := config.LoadForTest()
    cfg.BaseURL = "https://portal.kittypaw.app"
    cfg.APIBaseURL = "https://api.kittypaw.app"
    cfg.ConnectBaseURL = "https://connect.kittypaw.app"
    r, cleanup := NewRouter(cfg, nil, nil, nil)
    t.Cleanup(cleanup)

    req := httptest.NewRequest(http.MethodGet, "/discovery", nil)
    req.Host = "connect.kittypaw.app"
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    if w.Code != http.StatusNotFound {
        t.Fatalf("connect host discovery status = %d, want 404", w.Code)
    }
}

func TestDiscoveryIncludesConnectBaseURL(t *testing.T) {
    cfg := config.LoadForTest()
    cfg.BaseURL = "https://portal.kittypaw.app"
    cfg.APIBaseURL = "https://api.kittypaw.app"
    cfg.ConnectBaseURL = "https://connect.kittypaw.app"
    r, cleanup := NewRouter(cfg, nil, nil, nil)
    t.Cleanup(cleanup)

    req := httptest.NewRequest(http.MethodGet, "/discovery", nil)
    req.Host = "portal.kittypaw.app"
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)
    if w.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
    }
    var body map[string]string
    if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
        t.Fatalf("decode discovery: %v", err)
    }
    if body["connect_base_url"] != "https://connect.kittypaw.app" {
        t.Fatalf("connect_base_url = %q", body["connect_base_url"])
    }
}
```

- [ ] **Step 5: Run route tests and verify failure**

Run:

```bash
cd apps/portal && go test ./cmd/server -run 'Connect|IdentityRoutesNotServed' -count=1
```

Expected: fails because `/connect`, Connect host boundary, Connect root
dispatch, and discovery `connect_base_url` do not exist. The collapsed-API-host
test should also fail if the implementation tries to reuse the old
identity/resource split check for Connect.

- [ ] **Step 6: Implement route skeleton**

Add host-only middleware for user-facing host separation:

```go
identityOnly := hostOnlyMiddleware(cfg.BaseURL)
connectOnly := hostOnlyMiddleware(cfg.ConnectBaseURL)
```

Do not rely on the existing identity/resource split check for Connect. Connect
host routing must remain strict even when `API_BASE_URL` equals `BASE_URL` in a
collapsed deployment. `hostOnlyMiddleware` should allow exact configured hosts
and local request hosts for development, and should return 404 for other hosts.

Register root through one host-aware handler instead of registering duplicate
`GET /` routes:

```go
r.Get("/", handleHostRoot(cfg))
```

`handleHostRoot` should return the Connect landing page when the request host
matches `CONNECT_BASE_URL`, otherwise the existing portal landing page for the
identity host and local development hosts.

Register Connect routes:

```go
r.Group(func(r chi.Router) {
    r.Use(connectOnly)
    r.Get("/connect", handleConnectHome(cfg))
    r.Get("/connect/", handleConnectHome(cfg))
})
```

Add `handleConnectHome` returning a minimal no-store HTML page branded
`KittyPaw Connect`. Add `connect_base_url` to discovery only when
`cfg.ConnectBaseURL` is non-empty. Missing `CONNECT_BASE_URL` disables Connect
routes in production by skipping the Connect route group rather than falling
back to `BASE_URL`.

- [ ] **Step 7: Update deployment docs/template**

Add `CONNECT_BASE_URL=https://connect.kittypaw.app`,
`CONNECT_GOOGLE_CLIENT_ID`, and `CONNECT_GOOGLE_CLIENT_SECRET` to env examples
and docs.
Update nginx template so `server_name` can include both `portal.kittypaw.app`
and `connect.kittypaw.app`, for example by making `DEPLOY_DOMAIN` accept a
space-separated value.

- [ ] **Step 8: Run tests**

Run:

```bash
cd apps/portal && go test ./internal/config ./cmd/server -count=1
```

Expected: pass.

- [ ] **Step 9: Commit**

```bash
git add apps/portal/internal/config apps/portal/cmd/server apps/portal/deploy apps/portal/DEPLOY.md
git commit -m "feat(portal): add connect host skeleton"
```

---

## Task 2: Kittypaw Discovery Connect URL Wiring

**Files:**
- Modify: `apps/kittypaw/core/discovery.go`
- Test: `apps/kittypaw/core/discovery_test.go`
- Modify: `apps/kittypaw/core/api_token.go`
- Test: `apps/kittypaw/core/api_token_test.go`
- Modify: `apps/kittypaw/cli/cmd_login.go`
- Test: `apps/kittypaw/cli/cmd_login_test.go`

- [ ] **Step 1: Write failing discovery parsing tests**

Cover:

- `FetchDiscovery` decodes `connect_base_url`;
- trailing slash is trimmed from `connect_base_url`;
- missing `connect_base_url` is allowed for old portal deployments.

- [ ] **Step 2: Write failing persistence tests**

Cover:

- `APITokenManager.SaveConnectBaseURL(apiURL, connectURL)` stores the value
  under the same portal-host namespace as `auth_base_url`;
- empty value deletes stale `connect_base_url`;
- `ResolveConnectBaseURL(apiURL)` returns the stored value when present;
- fallback is explicit and narrow: replace the first `portal.` host label with
  `connect.` only for HTTPS portal hosts. Localhost/dev deployments return the
  original API URL as the Connect base only when tests opt into that helper.

- [ ] **Step 3: Write failing login discovery test**

Extend `applyDiscovery` tests so a portal discovery response containing
`connect_base_url` is persisted. This keeps `kittypaw connect gmail` from
guessing domains later.

- [ ] **Step 4: Run tests and verify failure**

```bash
cd apps/kittypaw && go test ./core ./cli -run 'Discovery|ConnectBaseURL|ApplyDiscovery' -count=1
```

Expected: fails because local discovery structs and URL helpers do not know
about `connect_base_url`.

- [ ] **Step 5: Implement discovery and storage helpers**

Add:

```go
type DiscoveryResponse struct {
    ...
    ConnectBaseURL string `json:"connect_base_url"`
}

func (m *APITokenManager) SaveConnectBaseURL(apiURL, connectBaseURL string) error
func (m *APITokenManager) LoadConnectBaseURL(apiURL string) (string, bool)
func (m *APITokenManager) ResolveConnectBaseURL(apiURL string) string
```

Teach `applyDiscovery` to call `SaveConnectBaseURL`. This discovery cache
belongs with other service topology keys under the portal-host
`kittypaw-api/<host>` namespace. After Gmail connection, `ServiceTokenManager`
may also snapshot the Connect base URL under `oauth-gmail/connect_base_url` so
refresh can keep working even if the user later changes API defaults.

- [ ] **Step 6: Run tests**

```bash
cd apps/kittypaw && go test ./core ./cli -run 'Discovery|ConnectBaseURL|ApplyDiscovery' -count=1
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add apps/kittypaw/core/discovery.go apps/kittypaw/core/discovery_test.go apps/kittypaw/core/api_token.go apps/kittypaw/core/api_token_test.go apps/kittypaw/cli/cmd_login.go apps/kittypaw/cli/cmd_login_test.go
git commit -m "feat(kittypaw): persist connect discovery url"
```

---

## Task 3: Portal Connect One-Time Code Store

**Files:**
- Create: `apps/portal/internal/connect/types.go`
- Create: `apps/portal/internal/connect/code_store.go`
- Test: `apps/portal/internal/connect/code_store_test.go`

- [ ] **Step 1: Write failing code-store tests**

Test behaviors:

- created codes are one-time use;
- expired codes are rejected;
- store is bounded;
- token payload includes provider, access token, refresh token, expiry, scope,
  and email.

Use an injectable clock if needed so expiry does not require sleeping.

- [ ] **Step 2: Run test and verify failure**

```bash
cd apps/portal && go test ./internal/connect -run CodeStore -count=1
```

Expected: fails because package does not exist.

- [ ] **Step 3: Implement minimal types**

Define:

```go
type TokenSet struct {
    Provider     string    `json:"provider"`
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token,omitempty"`
    TokenType    string    `json:"token_type"`
    ExpiresIn    int       `json:"expires_in,omitempty"`
    Scope        string    `json:"scope,omitempty"`
    Email        string    `json:"email,omitempty"`
    IssuedAt     time.Time `json:"issued_at"`
}

type CodeEntry struct {
    Tokens TokenSet
    CreatedAt time.Time
}
```

Implement `CodeStore.Create(tokens)` and `CodeStore.Consume(code)`.

- [ ] **Step 4: Run tests**

```bash
cd apps/portal && go test ./internal/connect -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add apps/portal/internal/connect
git commit -m "feat(portal): add connect code store"
```

---

## Task 4: Gmail Connect OAuth Broker

**Files:**
- Create: `apps/portal/internal/connect/gmail.go`
- Create: `apps/portal/internal/connect/handler.go`
- Test: `apps/portal/internal/connect/gmail_test.go`
- Test: `apps/portal/internal/connect/handler_test.go`
- Modify: `apps/portal/internal/config/config.go`
- Modify: `apps/portal/internal/config/config_test.go`
- Modify: `apps/portal/cmd/server/main.go`
- Modify: `apps/portal/cmd/server/main_test.go`

- [ ] **Step 1: Write failing Gmail provider tests**

Cover:

- auth URL uses `CONNECT_BASE_URL + /connect/gmail/callback`;
- auth URL uses `CONNECT_GOOGLE_CLIENT_ID`, not `GOOGLE_CLIENT_ID`;
- auth URL includes PKCE S256;
- scope is exactly `openid email profile https://www.googleapis.com/auth/gmail.readonly`;
- auth URL includes `access_type=offline`;
- auth URL includes `include_granted_scopes=true`;
- token exchange parses `access_token`, `refresh_token`, `expires_in`, `scope`;
- refresh exchange posts `grant_type=refresh_token`.

- [ ] **Step 2: Write failing handler tests**

Cover:

- `GET /connect/gmail/login?mode=http&port=12345` redirects to fake Google;
- invalid mode or port returns 400;
- callback exchanges fake Google code and redirects to localhost with only
  `code=...`, not access or refresh tokens;
- code mode renders a one-time code page;
- `POST /connect/cli/exchange` consumes the one-time code and returns token JSON;
- replaying the same code returns 401;
- `POST /connect/gmail/refresh` returns a fresh access token.

- [ ] **Step 3: Run tests and verify failure**

```bash
cd apps/portal && go test ./internal/connect ./cmd/server -run 'Gmail|Connect' -count=1
```

Expected: fails because handlers/provider are missing.

- [ ] **Step 4: Implement Gmail provider and handlers**

Use Google endpoints defaulting to:

```text
https://accounts.google.com/o/oauth2/v2/auth
https://oauth2.googleapis.com/token
https://www.googleapis.com/oauth2/v2/userinfo
```

Keep endpoint override fields for tests. Reuse `auth.GenerateVerifier` and
`auth.ChallengeS256`.

Do not log tokens. Do not put tokens in redirect URLs.

- [ ] **Step 5: Wire routes**

In `NewRouter`, instantiate `connect.CodeStore` and a Gmail handler using
`cfg.ConnectGoogleClientID`, `cfg.ConnectGoogleClientSecret`, and
`cfg.ConnectBaseURL`.

Do not reuse `cfg.GoogleClientID` or `cfg.GoogleClientSecret`; those remain the
KittyPaw identity login client. Gmail restricted scopes need a Connect-specific
client/project boundary.

Register on the Connect host only:

```text
GET  /connect/gmail/login
GET  /connect/gmail/callback
POST /connect/cli/exchange
POST /connect/gmail/refresh
```

- [ ] **Step 6: Run tests**

```bash
cd apps/portal && go test ./internal/connect ./cmd/server -count=1
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add apps/portal/internal/connect apps/portal/cmd/server
git commit -m "feat(portal): broker gmail connect oauth"
```

---

## Task 5: Local OAuth-Gmail Token Manager

**Files:**
- Create: `apps/kittypaw/core/oauth_service_token.go`
- Test: `apps/kittypaw/core/oauth_service_token_test.go`

- [ ] **Step 1: Write failing token manager tests**

Cover:

- `SaveServiceTokens("gmail", tokens)` writes namespace `oauth-gmail`;
- `LoadServiceAccessToken("gmail")` returns current token if not expired;
- expired token calls portal refresh endpoint and updates `access_token`;
- missing refresh token returns an actionable reconnect error;
- manager stores `connect_base_url`, `scope`, `email`, and `expires_at`.

- [ ] **Step 2: Run tests and verify failure**

```bash
cd apps/kittypaw && go test ./core -run ServiceToken -count=1
```

Expected: fails because the manager does not exist.

- [ ] **Step 3: Implement manager**

Use `SecretsStore` and `http.Client`. Keep API separate from
`APITokenManager` so KittyPaw API login and third-party provider tokens do not
get conflated.

Suggested names:

```go
type ServiceTokenManager struct { ... }
type ServiceTokenSet struct { ... }
func ServiceTokenNamespace(provider string) string
func (m *ServiceTokenManager) Save(provider string, tokens ServiceTokenSet) error
func (m *ServiceTokenManager) LoadAccessToken(provider string) (string, error)
func (m *ServiceTokenManager) Refresh(provider string) (ServiceTokenSet, error)
```

- [ ] **Step 4: Run tests**

```bash
cd apps/kittypaw && go test ./core -run ServiceToken -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add apps/kittypaw/core/oauth_service_token.go apps/kittypaw/core/oauth_service_token_test.go
git commit -m "feat(kittypaw): add service oauth token manager"
```

---

## Task 6: `kittypaw connect gmail` CLI

**Files:**
- Modify: `apps/kittypaw/cli/main.go`
- Create: `apps/kittypaw/cli/cmd_connect.go`
- Test: `apps/kittypaw/cli/cmd_connect_test.go`
- Depends on: Task 2 Connect discovery URL persistence.

- [ ] **Step 1: Write failing CLI tests**

Cover:

- command is registered as `connect gmail`;
- HTTP mode opens `/connect/gmail/login?mode=http&port=...`;
- callback accepts only one-time `code`, not token query params;
- CLI posts code to `/connect/cli/exchange`;
- successful exchange stores `oauth-gmail` secrets for the selected account;
- `--code` mode prints URL and exchanges pasted code.

- [ ] **Step 2: Run tests and verify failure**

```bash
cd apps/kittypaw && go test ./cli -run ConnectGmail -count=1
```

Expected: fails because command is missing.

- [ ] **Step 3: Implement command**

Follow existing `cmd_login.go` shape, but:

- call `applyDiscovery` before building the Connect login URL;
- resolve the Connect base URL through `APITokenManager.ResolveConnectBaseURL`;
- default to replacing `portal.kittypaw.app` with `connect.kittypaw.app` only as
  an explicit fallback helper covered by tests;
- store tokens through `ServiceTokenManager`;
- support `--account` through the existing account resolution path if available;
- error with `run kittypaw login first` only if a later refresh endpoint requires
  portal API auth.

- [ ] **Step 4: Run tests**

```bash
cd apps/kittypaw && go test ./cli -run ConnectGmail -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add apps/kittypaw/cli/main.go apps/kittypaw/cli/cmd_connect.go apps/kittypaw/cli/cmd_connect_test.go
git commit -m "feat(cli): add gmail connect command"
```

---

## Task 7: OAuth Source-Bound Package Config

**Files:**
- Modify: `apps/kittypaw/core/package.go`
- Modify: `apps/kittypaw/engine/executor.go`
- Test: `apps/kittypaw/core/package_test.go`
- Test: `apps/kittypaw/engine/executor_test.go`

- [ ] **Step 1: Write failing tests**

Cover:

- package config source `oauth-gmail/access_token` parses and is preserved;
- package execution refreshes Gmail access token before injecting config;
- missing Gmail connection returns `skill "<name>" requires Gmail connection — run: kittypaw connect gmail`;
- existing `kittypaw-api/access_token` behavior is unchanged.

- [ ] **Step 2: Run tests and verify failure**

```bash
cd apps/kittypaw && go test ./core ./engine -run 'OAuth|APILogin|Source' -count=1
```

Expected: fails because executor only special-cases `kittypaw-api`.

- [ ] **Step 3: Implement OAuth source resolution**

Add a small resolver in executor for source-bound fields:

```text
kittypaw-api/access_token -> existing APITokenManager
oauth-gmail/access_token  -> ServiceTokenManager
```

Do not make a generic stringly-typed resolver beyond these two forms in this
task.

- [ ] **Step 4: Run tests**

```bash
cd apps/kittypaw && go test ./core ./engine -run 'OAuth|APILogin|Source' -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add apps/kittypaw/core apps/kittypaw/engine
git commit -m "feat(engine): resolve gmail oauth package tokens"
```

---

## Task 8: Gmail Mail Digest Package PoC

**Files:**
- Create: `apps/kittypaw/core/gmail_client.go` or a small
  `apps/kittypaw/gmail/client.go` package if a separate package namespace is
  clearer during implementation.
- Test beside the chosen Gmail client implementation.
- Create a deterministic installed-package fixture or first-party package
  manifest that sources `oauth-gmail/access_token`.
- Test the package execution path through `apps/kittypaw/engine` so the token
  refresh from Task 7 is exercised.

Use the native package path for the first Gmail workflow. Do not add a new local
MCP server for this PoC. The goal of this task is to prove Gmail read access and
package source binding with the smallest moving parts; generic MCP token
injection is a follow-up in Task 9.

- [ ] **Step 1: Write failing Gmail client tests**

Cover:

- list recent messages;
- fetch one message body/snippet;
- handle 401 by returning reconnect/refresh guidance;
- never log message body or token;
- request only the fields needed for a digest.

- [ ] **Step 2: Write failing package execution test**

Cover:

- a package with `source = "oauth-gmail/access_token"` receives a fresh token;
- missing Gmail connection returns the exact actionable error from Task 7;
- the package can produce a deterministic mail digest fixture without a live
  Google account by using a fake Gmail HTTP server.

- [ ] **Step 3: Implement minimal read-only Gmail client and package fixture**

Use Gmail API endpoints that can support the first scenario:

```text
GET https://gmail.googleapis.com/gmail/v1/users/me/messages
GET https://gmail.googleapis.com/gmail/v1/users/me/messages/{id}
```

Request only the fields needed for summaries.

- [ ] **Step 4: Run tests**

```bash
cd apps/kittypaw && go test ./core ./engine -run 'Gmail|OAuth' -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add apps/kittypaw
git commit -m "feat(kittypaw): add gmail read-only proof of concept"
```

---

## Task 9: MCP Token Injection Follow-Up

**Files:**
- Modify: `apps/kittypaw/mcp/registry.go`
- Modify: `apps/kittypaw/core/config.go`
- Test: `apps/kittypaw/mcp/registry_test.go`

This is no longer on the critical path for the first Gmail digest. Keep it in
the roadmap because external Gmail/Slack/Notion MCP servers will need the same
token injection pattern later.

- [ ] **Step 1: Write failing MCP env resolver tests**

Cover:

- static env still works;
- dynamic env value from `oauth-gmail/access_token` is resolved before subprocess
  start;
- missing token returns a clear error and does not start the subprocess.

- [ ] **Step 2: Run tests and verify failure**

```bash
cd apps/kittypaw && go test ./mcp -run Env -count=1
```

Expected: fails because registry only supports static env.

- [ ] **Step 3: Implement narrow env resolver**

Extend `MCPServerConfig` with a backward-compatible optional map:

```go
EnvFrom map[string]string `toml:"env_from"`
```

Example:

```toml
[[mcp_servers]]
name = "gmail"
command = "kittypaw-gmail-mcp"

[mcp_servers.env_from]
GMAIL_ACCESS_TOKEN = "oauth-gmail/access_token"
```

Resolve only `oauth-gmail/access_token` in this task.

- [ ] **Step 4: Run tests**

```bash
cd apps/kittypaw && go test ./mcp ./core -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add apps/kittypaw/core/config.go apps/kittypaw/mcp
git commit -m "feat(mcp): support gmail oauth env injection"
```

---

## Task 10: Verification And Operational Docs

**Files:**
- Modify: `apps/portal/DEPLOY.md`
- Modify: `apps/portal/deploy/env.example`
- Create: `docs/connect-gmail-oauth.md` or equivalent repo-local docs path.

- [ ] **Step 1: Document Google Cloud setup**

Include:

- `kittypaw.app` authorized domain;
- `connect.kittypaw.app` redirect URI;
- app name `KittyPaw Connect`;
- public home page and privacy policy requirements;
- staging/test project recommendation;
- restricted-scope verification risk for `gmail.readonly`;
- security assessment possibility.

- [ ] **Step 2: Document beta limits**

State clearly:

- Gmail Connect may be limited to Google test users until verification clears.
- Some Workspace admins can block high-risk Gmail scopes.
- Users should rerun `kittypaw connect gmail` if refresh fails or access is
  revoked.

- [ ] **Step 3: Run final tests**

Run:

```bash
cd apps/portal && go test ./... -count=1
cd ../kittypaw && go test ./core ./cli ./engine ./mcp -count=1
```

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add apps/portal/DEPLOY.md apps/portal/deploy/env.example docs
git commit -m "docs: document kitty paw connect gmail oauth"
```

---

## Full Verification

After all tasks:

```bash
cd apps/portal && go test ./... -count=1
cd ../kittypaw && go test ./... -count=1
```

If a live Google test project is available, run one manual E2E:

```bash
kittypaw login
kittypaw connect gmail
kittypaw run mail-digest
```

Expected:

- Connect login opens on `connect.kittypaw.app`;
- Google callback returns only one-time code to localhost;
- `~/.kittypaw/accounts/<accountID>/secrets.json` contains `oauth-gmail`;
- mail digest can read and summarize recent messages;
- no Gmail tokens appear in logs, redirect URLs, or package config files.

## Plan Self-Review

- Spec coverage: same portal binary, Connect host, no rename, Gmail first,
  local token storage, restricted-scope risk, and extraction triggers are all
  represented in tasks.
- Placeholder scan: no placeholder markers or open-ended deferrals remain.
- Scope check: this is intentionally staged. Tasks 1-4 produce Connect host,
  discovery, code-store, and Gmail OAuth broker capability. Tasks 5-7 connect
  local token storage and package sources. Task 8 delivers the first Gmail
  workflow. Task 9 covers later MCP token injection. Task 10 covers operations.
- Risk note: if Google restricted-scope verification becomes a hard blocker,
  Tasks 1-7 still ship useful infrastructure and Task 8 can remain test-user
  gated.
