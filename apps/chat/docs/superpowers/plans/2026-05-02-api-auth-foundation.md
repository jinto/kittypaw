# API Auth Foundation Implementation Plan

> Historical plan snapshot. This document records an app-local implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and the app README/DEPLOY docs for the current live shape.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gate KittyChat relay access through an portal-issued credential verifier boundary and stabilize daemon requests around versioned operations.

**Architecture:** Add `internal/identity` as the verifier layer. The first verifier is env-seeded memory data, but the public interface is `CredentialVerifier` returning claims with `audiences`, `version`, and `scopes` so JWT/JWKS or introspection can replace it later. Add protocol v1 `operation`, `protocol_version`, and `capabilities` fields so daemon contracts are not coupled only to HTTP paths.

**Tech Stack:** Go 1.25, chi, coder/websocket, existing `broker`, `openai`, and `daemonws` packages.

---

## File Structure

- Create `internal/identity/verifier.go`: scope constants, claims types, `CredentialVerifier`, `MemoryCredentialVerifier`, validation, and `ErrUnauthorized`.
- Create `internal/identity/verifier_test.go`: tests for seeded API/device claims, invalid claims, unknown tokens, and defensive copying.
- Create `internal/identity/authenticator.go`: HTTP token extraction and verifier-backed authenticators.
- Create `internal/identity/authenticator_test.go`: tests for bearer, `x-api-key`, `x-device-token`, and nil verifier behavior.
- Modify `internal/protocol/frame.go`: add operation constants, protocol version, daemon version, capabilities, and operation-to-HTTP mapping.
- Modify `internal/protocol/frame_test.go`: verify hello v1 and request operation validation.
- Modify `internal/broker/broker.go`: carry operation through request frames.
- Modify relay tests under `internal/broker`, `internal/openai`, and `internal/daemonws`: assert operation values.
- Modify `cmd/kittychat/main.go`: seed `MemoryCredentialVerifier` from config and use identity authenticators in runtime wiring.
- Modify `cmd/kittychat/main_test.go`: account for `newRouter` returning an error and prove seeded credentials gate access.
- Modify `README.md`: replace "static-token MVP auth" wording with env-seeded credential verifier wording.

---

### Task 1: Add Credential Verifier and Claims

**Files:**
- Create: `internal/identity/verifier_test.go`
- Create: `internal/identity/verifier.go`

- [ ] **Step 1: Write the failing verifier tests**

Create `internal/identity/verifier_test.go` with tests that cover:

- `NewMemoryCredentialVerifier().AddAPIClient("api_secret", claims)` then `VerifyAPIClient(..., "api_secret")` returns claims containing:
  - `Subject: "user_1"`
  - `Audiences: []string{AudienceKittyChat}`
  - `Version: CredentialVersion1`
  - `Scopes: []Scope{ScopeChatRelay, ScopeModelsRead}`
  - `UserID: "user_1"`, `DeviceID: "dev_1"`, `AccountID: "alice"`
- `AddDevice("dev_secret", claims)` then `VerifyDevice(..., "dev_secret")` returns claims containing:
  - `Subject: "device:dev_1"`
  - `Audiences: []string{AudienceKittyChat}`
  - `Version: CredentialVersion1`
  - `Scopes: []Scope{ScopeDaemonConnect}`
  - `UserID: "user_1"`, `DeviceID: "dev_1"`, `LocalAccountIDs: []string{"alice", "bob"}`
- missing/unknown API and device tokens return `ErrUnauthorized`.
- invalid API claims are rejected for empty token, missing kittychat audience, wrong version, missing scope, unknown scope, and missing account id.
- invalid device claims are rejected for empty token, missing kittychat audience, wrong version, missing scope, unknown scope, and missing local accounts.
- returned device claims cannot mutate stored local accounts or scopes.

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
go test ./internal/identity -count=1
```

Expected: fail because `internal/identity` does not exist yet.

- [ ] **Step 3: Implement verifier**

Create `internal/identity/verifier.go` with:

- `const AudienceKittyChat = "https://chat.kittypaw.app"`
- `const CredentialVersion1 = 1`
- `type Scope string`
- `ScopeChatRelay`, `ScopeModelsRead`, `ScopeDaemonConnect`
- `var ErrUnauthorized = errors.New("unauthorized")`
- `type APIClientClaims struct { Subject, UserID, DeviceID, AccountID string; Audiences []string; Scopes []Scope; Version int }`
- `type DeviceClaims struct { Subject, UserID, DeviceID string; Audiences []string; LocalAccountIDs []string; Scopes []Scope; Version int }`
- `type CredentialVerifier interface { VerifyAPIClient(context.Context, string) (APIClientClaims, error); VerifyDevice(context.Context, string) (DeviceClaims, error) }`
- `type MemoryCredentialVerifier` with `AddAPIClient`, `AddDevice`, `VerifyAPIClient`, and `VerifyDevice`.
- `func (c APIClientClaims) Principal() openai.Principal`.
- `func (c DeviceClaims) Principal() broker.DevicePrincipal`.
- validation that requires audiences include `https://chat.kittypaw.app`, `version == 1`, non-empty subject/user/device/account fields, non-empty local accounts for device claims, and only known scope values.
- defensive copying for all slices on insert and return.

- [ ] **Step 4: Run test and verify GREEN**

Run:

```bash
go test ./internal/identity -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/identity/verifier.go internal/identity/verifier_test.go
git commit -m "feat: add credential verifier"
```

---

### Task 2: Add Operation-based Protocol Contract

**Files:**
- Modify: `internal/protocol/frame_test.go`
- Modify: `internal/protocol/frame.go`
- Modify: `internal/broker/broker_test.go`
- Modify: `internal/broker/broker.go`
- Modify: `internal/openai/handler_test.go`
- Modify: `internal/openai/handler.go`
- Modify: `internal/daemonws/handler_test.go`

- [ ] **Step 1: Write failing protocol and relay tests**

Update protocol tests so:

- request round-trip includes `Operation: OperationOpenAIChatCompletions`.
- hello validation requires `ProtocolVersion: ProtocolVersion1` and non-empty
  `Capabilities`.
- hello validation rejects unknown capabilities.
- request validation accepts `OperationOpenAIModels` and
  `OperationOpenAIChatCompletions`.
- request validation rejects an unknown operation.

Update broker/openai/daemon tests so request frames and broker requests assert:

- models route uses `OperationOpenAIModels`.
- chat completions route uses `OperationOpenAIChatCompletions`.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
go test ./internal/protocol ./internal/broker ./internal/openai ./internal/daemonws -count=1
```

Expected: fail because operation/protocol fields are not implemented yet.

- [ ] **Step 3: Implement operation protocol**

Implement:

- `type Operation string`
- `OperationOpenAIModels = "openai.models"`
- `OperationOpenAIChatCompletions = "openai.chat_completions"`
- `ProtocolVersion1 = "1"`
- `Frame.Operation`, `Frame.DaemonVersion`, `Frame.ProtocolVersion`, and
  `Frame.Capabilities`.
- `AllowedOperation(operation Operation) bool`.
- `HTTPForOperation(operation Operation) (method string, path string, ok bool)`.
- request validation that requires a valid operation. `method` and `path` are
  optional compatibility fields, but if either is present they must match the
  operation mapping.
- hello validation that requires `protocol_version == "1"` and at least one
  known capability.
- broker request struct and frame construction to include operation.
- OpenAI handler mapping from HTTP route to operation.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run:

```bash
go test ./internal/protocol ./internal/broker ./internal/openai ./internal/daemonws -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/protocol internal/broker internal/openai internal/daemonws
git commit -m "feat: add operation based relay protocol"
```

---

### Task 3: Add Verifier-backed Authenticators

**Files:**
- Create: `internal/identity/authenticator_test.go`
- Create: `internal/identity/authenticator.go`

- [ ] **Step 1: Write the failing authenticator tests**

Create `internal/identity/authenticator_test.go` with tests that cover:

- `APIAuthenticator{Verifier: verifier}` accepts `Authorization: Bearer api_secret` and returns `openai.Principal{UserID: "user_1", DeviceID: "dev_1", AccountID: "alice"}`.
- `APIAuthenticator{Verifier: verifier}` accepts `x-api-key: api_secret`.
- `DeviceAuthenticator{Verifier: verifier}` accepts `Authorization: Bearer dev_secret` and returns `broker.DevicePrincipal{UserID: "user_1", DeviceID: "dev_1", LocalAccountIDs: []string{"alice"}}`.
- `DeviceAuthenticator{Verifier: verifier}` accepts `x-device-token: dev_secret`.
- missing verifier, missing token, and unknown token return `ErrUnauthorized`.

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
go test ./internal/identity -count=1
```

Expected: fail because the authenticators do not exist yet.

- [ ] **Step 3: Implement authenticators**

Create `internal/identity/authenticator.go` with:

- `type APIAuthenticator struct { Verifier CredentialVerifier }`
- `func (a APIAuthenticator) Authenticate(r *http.Request) (openai.Principal, error)` that extracts bearer or `x-api-key`, verifies API claims, and returns `claims.Principal()`.
- `type DeviceAuthenticator struct { Verifier CredentialVerifier }`
- `func (a DeviceAuthenticator) Authenticate(r *http.Request) (broker.DevicePrincipal, error)` that extracts bearer or `x-device-token`, verifies device claims, and returns `claims.Principal()`.
- `requestToken(r, fallbackHeader)` helper that prefers `Authorization: Bearer ...` over the fallback header.

- [ ] **Step 4: Run test and verify GREEN**

Run:

```bash
go test ./internal/identity -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/identity/authenticator.go internal/identity/authenticator_test.go
git commit -m "feat: add credential authenticators"
```

---

### Task 4: Wire Runtime Through Credential Verifier

**Files:**
- Modify: `cmd/kittychat/main.go`
- Modify: `cmd/kittychat/main_test.go`

- [ ] **Step 1: Write the failing runtime wiring tests**

Modify `cmd/kittychat/main_test.go` so:

- `newRouter(cfg)` returns `(http.Handler, error)`.
- `TestNewServerBuildsRunnableRouter` checks `/health`.
- `TestNewServerUsesSeededCredentialVerifier` checks wrong API token returns `401` and valid API token reaches the broker enough to return `503` while the daemon is offline.
- `TestNewServerRejectsInvalidCredentialSeed` clears `cfg.APIToken` and expects `newRouter` to return an error.

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
go test ./cmd/kittychat -count=1
```

Expected: fail because `newRouter` still returns only `http.Handler`.

- [ ] **Step 3: Update runtime wiring**

Modify `cmd/kittychat/main.go` so:

- `main()` calls `newRouter(cfg)` and exits on router creation errors.
- `newRouter(cfg)` creates a `MemoryCredentialVerifier`, seeds API and device claims from config, and wires:
  - `daemonws.NewHandler(identity.DeviceAuthenticator{Verifier: verifier}, b)`
  - `openai.NewHandler(identity.APIAuthenticator{Verifier: verifier}, b)`
- API seed claims:
  - `Subject: cfg.UserID`
  - `Audiences: []string{identity.AudienceKittyChat}`
  - `Version: identity.CredentialVersion1`
  - `Scopes: []identity.Scope{identity.ScopeChatRelay, identity.ScopeModelsRead}`
  - `UserID`, `DeviceID`, `AccountID` from config
- Device seed claims:
  - `Subject: "device:" + cfg.DeviceID`
  - `Audiences: []string{identity.AudienceKittyChat}`
  - `Version: identity.CredentialVersion1`
  - `Scopes: []identity.Scope{identity.ScopeDaemonConnect}`
  - `UserID`, `DeviceID`, `LocalAccountIDs` from config

- [ ] **Step 4: Run focused tests and verify GREEN**

Run:

```bash
go test ./cmd/kittychat ./internal/identity -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/kittychat/main.go cmd/kittychat/main_test.go
git commit -m "feat: wire kittychat through credential verifier"
```

---

### Task 5: Documentation and Full Verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README wording**

Modify the MVP scope bullet in `README.md`:

```markdown
- env-seeded MVP credential verifier for one device/account
- operation-based daemon protocol v1 for OpenAI-compatible relay requests
```

Keep the environment variable table unchanged because the MVP seed variables are
still the supported local configuration.

- [ ] **Step 2: Run full verification**

Run:

```bash
make test
make lint
make build
make clean
```

Expected:

- `make test` passes all packages, including `internal/identity`.
- `make lint` prints `0 issues.`
- `make build` exits 0 and creates `kittychat`.
- `make clean` removes the build artifact.

- [ ] **Step 3: Commit docs**

```bash
git add README.md
git commit -m "docs: describe env seeded credential verifier"
```

- [ ] **Step 4: Final push after verification**

Run:

```bash
git status --short --branch
git log --oneline -8
git push origin main
```

Expected: push succeeds to `https://github.com/kittypaw-app/kittychat.git`.

---

## Coverage Checklist

- `internal/identity` exposes `CredentialVerifier`, claims, scope constants, and version constants.
- Daemon request frames use stable `operation` values.
- Daemon hello frames require protocol v1 and known capabilities.
- Runtime wiring no longer directly uses static token authenticators.
- Current MVP env configuration still works.
- API client access remains limited by resolved `device_id` and `account_id`.
- Daemon access remains limited by resolved `device_id` and local accounts.
- Full test/lint/build verification runs after implementation.
