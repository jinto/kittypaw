# API Auth Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gate KittyChat relay access through an identity boundary that can later verify KittyPaw API-server-issued credentials.

**Architecture:** Add `internal/identity` as the credential resolution layer. The first implementation is an env-seeded in-memory store preserving current MVP behavior, while runtime wiring moves from direct static token authenticators to store-backed authenticators.

**Tech Stack:** Go 1.25, chi, coder/websocket, existing `broker`, `openai`, and `daemonws` packages.

---

## File Structure

- Create `internal/identity/store.go`: `Store` interface, `MemoryStore`, seed methods, validation, and `ErrUnauthorized`.
- Create `internal/identity/store_test.go`: focused tests for API and device credential resolution.
- Create `internal/identity/authenticator.go`: HTTP token extraction and store-backed authenticators for API clients and daemon devices.
- Create `internal/identity/authenticator_test.go`: tests for bearer, `x-api-key`, `x-device-token`, and nil-store behavior.
- Modify `cmd/kittychat/main.go`: seed `MemoryStore` from config and use identity authenticators in runtime wiring.
- Modify `cmd/kittychat/main_test.go`: account for `newRouter` returning an error and prove auth is store-backed.
- Modify `README.md`: replace "static-token MVP auth" wording with env-seeded identity store wording.

---

### Task 1: Add Identity Memory Store

**Files:**
- Create: `internal/identity/store_test.go`
- Create: `internal/identity/store.go`

- [ ] **Step 1: Write failing store tests**

Create `internal/identity/store_test.go`:

```go
package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/openai"
)

func TestMemoryStoreResolvesSeededAPIClient(t *testing.T) {
	store := NewMemoryStore()
	want := openai.Principal{UserID: "user_1", DeviceID: "dev_1", AccountID: "alice"}
	if err := store.AddAPIClient("api_secret", want); err != nil {
		t.Fatalf("AddAPIClient() error = %v", err)
	}

	got, err := store.ResolveAPIClient(context.Background(), "api_secret")
	if err != nil {
		t.Fatalf("ResolveAPIClient() error = %v", err)
	}
	if got != want {
		t.Fatalf("principal = %+v, want %+v", got, want)
	}
}

func TestMemoryStoreResolvesSeededDevice(t *testing.T) {
	store := NewMemoryStore()
	want := broker.DevicePrincipal{
		UserID:          "user_1",
		DeviceID:        "dev_1",
		LocalAccountIDs: []string{"alice", "bob"},
	}
	if err := store.AddDevice("dev_secret", want); err != nil {
		t.Fatalf("AddDevice() error = %v", err)
	}

	got, err := store.ResolveDevice(context.Background(), "dev_secret")
	if err != nil {
		t.Fatalf("ResolveDevice() error = %v", err)
	}
	if got.UserID != want.UserID || got.DeviceID != want.DeviceID {
		t.Fatalf("principal = %+v, want %+v", got, want)
	}
	if len(got.LocalAccountIDs) != 2 || got.LocalAccountIDs[0] != "alice" || got.LocalAccountIDs[1] != "bob" {
		t.Fatalf("local accounts = %+v, want %+v", got.LocalAccountIDs, want.LocalAccountIDs)
	}
}

func TestMemoryStoreRejectsUnknownTokens(t *testing.T) {
	store := NewMemoryStore()

	if _, err := store.ResolveAPIClient(context.Background(), "missing"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ResolveAPIClient() error = %v, want ErrUnauthorized", err)
	}
	if _, err := store.ResolveDevice(context.Background(), "missing"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ResolveDevice() error = %v, want ErrUnauthorized", err)
	}
}

func TestMemoryStoreValidatesSeeds(t *testing.T) {
	store := NewMemoryStore()

	if err := store.AddAPIClient("", openai.Principal{UserID: "user_1", DeviceID: "dev_1", AccountID: "alice"}); err == nil {
		t.Fatal("AddAPIClient() error = nil, want token validation error")
	}
	if err := store.AddAPIClient("api_secret", openai.Principal{UserID: "user_1", DeviceID: "dev_1"}); err == nil {
		t.Fatal("AddAPIClient() error = nil, want principal validation error")
	}
	if err := store.AddDevice("", broker.DevicePrincipal{UserID: "user_1", DeviceID: "dev_1", LocalAccountIDs: []string{"alice"}}); err == nil {
		t.Fatal("AddDevice() error = nil, want token validation error")
	}
	if err := store.AddDevice("dev_secret", broker.DevicePrincipal{UserID: "user_1", DeviceID: "dev_1"}); err == nil {
		t.Fatal("AddDevice() error = nil, want principal validation error")
	}
}
```

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
go test ./internal/identity -count=1
```

Expected: fail because `internal/identity` has no implementation yet.

- [ ] **Step 3: Implement memory store**

Create `internal/identity/store.go`:

```go
package identity

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/openai"
)

var ErrUnauthorized = errors.New("unauthorized")

type Store interface {
	ResolveAPIClient(ctx context.Context, token string) (openai.Principal, error)
	ResolveDevice(ctx context.Context, token string) (broker.DevicePrincipal, error)
}

type MemoryStore struct {
	mu      sync.RWMutex
	api     map[string]openai.Principal
	devices map[string]broker.DevicePrincipal
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		api:     make(map[string]openai.Principal),
		devices: make(map[string]broker.DevicePrincipal),
	}
}

func (s *MemoryStore) AddAPIClient(token string, principal openai.Principal) error {
	if token == "" {
		return fmt.Errorf("api token is required")
	}
	if err := principal.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.api[token] = principal
	return nil
}

func (s *MemoryStore) AddDevice(token string, principal broker.DevicePrincipal) error {
	if token == "" {
		return fmt.Errorf("device token is required")
	}
	if err := validateDevicePrincipal(principal); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[token] = cloneDevicePrincipal(principal)
	return nil
}

func (s *MemoryStore) ResolveAPIClient(ctx context.Context, token string) (openai.Principal, error) {
	if err := ctx.Err(); err != nil {
		return openai.Principal{}, err
	}
	if token == "" {
		return openai.Principal{}, ErrUnauthorized
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	principal, ok := s.api[token]
	if !ok {
		return openai.Principal{}, ErrUnauthorized
	}
	return principal, nil
}

func (s *MemoryStore) ResolveDevice(ctx context.Context, token string) (broker.DevicePrincipal, error) {
	if err := ctx.Err(); err != nil {
		return broker.DevicePrincipal{}, err
	}
	if token == "" {
		return broker.DevicePrincipal{}, ErrUnauthorized
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	principal, ok := s.devices[token]
	if !ok {
		return broker.DevicePrincipal{}, ErrUnauthorized
	}
	return cloneDevicePrincipal(principal), nil
}

func validateDevicePrincipal(principal broker.DevicePrincipal) error {
	if principal.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	if principal.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if len(principal.LocalAccountIDs) == 0 {
		return fmt.Errorf("at least one local account is required")
	}
	for _, accountID := range principal.LocalAccountIDs {
		if accountID == "" {
			return fmt.Errorf("local account id is required")
		}
	}
	return nil
}

func cloneDevicePrincipal(principal broker.DevicePrincipal) broker.DevicePrincipal {
	return broker.DevicePrincipal{
		UserID:          principal.UserID,
		DeviceID:        principal.DeviceID,
		LocalAccountIDs: append([]string(nil), principal.LocalAccountIDs...),
	}
}
```

- [ ] **Step 4: Run test and verify GREEN**

Run:

```bash
go test ./internal/identity -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/identity/store.go internal/identity/store_test.go
git commit -m "feat: add identity memory store"
```

---

### Task 2: Add Store-backed Authenticators

**Files:**
- Create: `internal/identity/authenticator_test.go`
- Create: `internal/identity/authenticator.go`

- [ ] **Step 1: Write failing authenticator tests**

Create `internal/identity/authenticator_test.go`:

```go
package identity

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/openai"
)

func TestAPIAuthenticatorAcceptsBearerToken(t *testing.T) {
	store := NewMemoryStore()
	want := openai.Principal{UserID: "user_1", DeviceID: "dev_1", AccountID: "alice"}
	if err := store.AddAPIClient("api_secret", want); err != nil {
		t.Fatalf("AddAPIClient() error = %v", err)
	}
	auth := APIAuthenticator{Store: store}
	req := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	req.Header.Set("Authorization", "Bearer api_secret")

	got, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got != want {
		t.Fatalf("principal = %+v, want %+v", got, want)
	}
}

func TestAPIAuthenticatorAcceptsXAPIKey(t *testing.T) {
	store := NewMemoryStore()
	want := openai.Principal{UserID: "user_1", DeviceID: "dev_1", AccountID: "alice"}
	if err := store.AddAPIClient("api_secret", want); err != nil {
		t.Fatalf("AddAPIClient() error = %v", err)
	}
	auth := APIAuthenticator{Store: store}
	req := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	req.Header.Set("x-api-key", "api_secret")

	got, err := auth.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got != want {
		t.Fatalf("principal = %+v, want %+v", got, want)
	}
}

func TestDeviceAuthenticatorAcceptsBearerAndDeviceTokenHeader(t *testing.T) {
	store := NewMemoryStore()
	want := broker.DevicePrincipal{UserID: "user_1", DeviceID: "dev_1", LocalAccountIDs: []string{"alice"}}
	if err := store.AddDevice("dev_secret", want); err != nil {
		t.Fatalf("AddDevice() error = %v", err)
	}
	auth := DeviceAuthenticator{Store: store}

	for _, header := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "bearer", key: "Authorization", value: "Bearer dev_secret"},
		{name: "device header", key: "x-device-token", value: "dev_secret"},
	} {
		t.Run(header.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/daemon/connect", nil)
			req.Header.Set(header.key, header.value)
			got, err := auth.Authenticate(req)
			if err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
			if got.UserID != want.UserID || got.DeviceID != want.DeviceID || len(got.LocalAccountIDs) != 1 || got.LocalAccountIDs[0] != "alice" {
				t.Fatalf("principal = %+v, want %+v", got, want)
			}
		})
	}
}

func TestAuthenticatorsRejectMissingStoreOrToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	if _, err := (APIAuthenticator{}).Authenticate(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("API Authenticate() error = %v, want ErrUnauthorized", err)
	}
	if _, err := (DeviceAuthenticator{}).Authenticate(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Device Authenticate() error = %v, want ErrUnauthorized", err)
	}

	store := NewMemoryStore()
	if _, err := (APIAuthenticator{Store: store}).Authenticate(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("API missing token error = %v, want ErrUnauthorized", err)
	}
	if _, err := (DeviceAuthenticator{Store: store}).Authenticate(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Device missing token error = %v, want ErrUnauthorized", err)
	}
}
```

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
go test ./internal/identity -count=1
```

Expected: fail because `APIAuthenticator` and `DeviceAuthenticator` are undefined.

- [ ] **Step 3: Implement authenticators**

Create `internal/identity/authenticator.go`:

```go
package identity

import (
	"net/http"
	"strings"

	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/openai"
)

type APIAuthenticator struct {
	Store Store
}

func (a APIAuthenticator) Authenticate(r *http.Request) (openai.Principal, error) {
	if a.Store == nil {
		return openai.Principal{}, ErrUnauthorized
	}
	return a.Store.ResolveAPIClient(r.Context(), requestToken(r, "x-api-key"))
}

type DeviceAuthenticator struct {
	Store Store
}

func (a DeviceAuthenticator) Authenticate(r *http.Request) (broker.DevicePrincipal, error) {
	if a.Store == nil {
		return broker.DevicePrincipal{}, ErrUnauthorized
	}
	return a.Store.ResolveDevice(r.Context(), requestToken(r, "x-device-token"))
}

func requestToken(r *http.Request, fallbackHeader string) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if token := r.Header.Get(fallbackHeader); token != "" {
		return token
	}
	return ""
}
```

- [ ] **Step 4: Run test and verify GREEN**

Run:

```bash
go test ./internal/identity -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/identity/authenticator.go internal/identity/authenticator_test.go
git commit -m "feat: add identity authenticators"
```

---

### Task 3: Wire Runtime Through Identity Store

**Files:**
- Modify: `cmd/kittychat/main.go`
- Modify: `cmd/kittychat/main_test.go`

- [ ] **Step 1: Write failing runtime wiring tests**

Modify `cmd/kittychat/main_test.go` to use the new `newRouter` return value and
prove the router uses the seeded identity store:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kittypaw-app/kittychat/internal/config"
)

func TestNewServerBuildsRunnableRouter(t *testing.T) {
	cfg := testConfig()
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestNewServerUsesSeededIdentityStore(t *testing.T) {
	cfg := testConfig()
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}

	wrongReq := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	wrongReq.Header.Set("Authorization", "Bearer wrong")
	wrongRR := httptest.NewRecorder()
	router.ServeHTTP(wrongRR, wrongReq)

	if wrongRR.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401; body=%s", wrongRR.Code, wrongRR.Body.String())
	}

	validReq := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	validReq.Header.Set("Authorization", "Bearer api_secret")
	validRR := httptest.NewRecorder()
	router.ServeHTTP(validRR, validReq)

	if validRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("valid token status = %d, want 503 offline; body=%s", validRR.Code, validRR.Body.String())
	}
}

func TestNewServerRejectsInvalidIdentitySeed(t *testing.T) {
	cfg := testConfig()
	cfg.APIToken = ""

	if _, err := newRouter(cfg); err == nil {
		t.Fatal("newRouter() error = nil, want invalid identity seed error")
	}
}

func testConfig() config.Config {
	return config.Config{
		BindAddr:       ":0",
		APIToken:       "api_secret",
		DeviceToken:    "dev_secret",
		UserID:         "user_1",
		DeviceID:       "dev_1",
		LocalAccountID: "alice",
		Version:        "test",
	}
}
```

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
go test ./cmd/kittychat -count=1
```

Expected: fail because `newRouter` still returns only `http.Handler`.

- [ ] **Step 3: Update runtime wiring**

Modify `cmd/kittychat/main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/config"
	"github.com/kittypaw-app/kittychat/internal/daemonws"
	"github.com/kittypaw-app/kittychat/internal/identity"
	"github.com/kittypaw-app/kittychat/internal/openai"
	"github.com/kittypaw-app/kittychat/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	router, err := newRouter(cfg)
	if err != nil {
		log.Fatalf("router: %v", err)
	}

	log.Printf("listening on %s", cfg.BindAddr)
	if err := http.ListenAndServe(cfg.BindAddr, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func newRouter(cfg config.Config) (http.Handler, error) {
	identityStore, err := newIdentityStore(cfg)
	if err != nil {
		return nil, err
	}
	b := broker.New(broker.Config{})

	return server.NewRouter(server.Config{
		Version:       cfg.Version,
		DaemonHandler: daemonws.NewHandler(identity.DeviceAuthenticator{Store: identityStore}, b),
		OpenAIHandler: openai.NewHandler(identity.APIAuthenticator{Store: identityStore}, b),
	}), nil
}

func newIdentityStore(cfg config.Config) (*identity.MemoryStore, error) {
	store := identity.NewMemoryStore()
	if err := store.AddAPIClient(cfg.APIToken, openai.Principal{
		UserID:    cfg.UserID,
		DeviceID:  cfg.DeviceID,
		AccountID: cfg.LocalAccountID,
	}); err != nil {
		return nil, fmt.Errorf("seed api client: %w", err)
	}
	if err := store.AddDevice(cfg.DeviceToken, broker.DevicePrincipal{
		UserID:          cfg.UserID,
		DeviceID:        cfg.DeviceID,
		LocalAccountIDs: []string{cfg.LocalAccountID},
	}); err != nil {
		return nil, fmt.Errorf("seed device: %w", err)
	}
	return store, nil
}
```

- [ ] **Step 4: Run focused tests and verify GREEN**

Run:

```bash
go test ./cmd/kittychat ./internal/identity -count=1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/kittychat/main.go cmd/kittychat/main_test.go
git commit -m "feat: wire kittychat through identity store"
```

---

### Task 4: Documentation and Full Verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README wording**

Modify the MVP scope bullet in `README.md`:

```markdown
- env-seeded MVP identity store for one device/account
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

- [ ] **Step 3: Confirm repo status**

Run:

```bash
git status --short --branch
```

Expected before commit: only README and implementation files modified or added.

- [ ] **Step 4: Commit docs and any final cleanup**

```bash
git add README.md
git commit -m "docs: describe env seeded identity store"
```

- [ ] **Step 5: Final push after verification**

Run:

```bash
git status --short --branch
git log --oneline -8
git push origin main
```

Expected:

- branch is ahead by the implementation commits before push.
- push succeeds to `https://github.com/kittypaw-app/kittychat.git`.

---

## Coverage Checklist

- The identity boundary exists in `internal/identity`.
- Runtime wiring no longer directly uses static token authenticators.
- Current MVP env configuration still works.
- API client access remains limited by resolved `device_id` and `account_id`.
- Daemon access remains limited by resolved `device_id` and local accounts.
- Full test/lint/build verification runs after implementation.
