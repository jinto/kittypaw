package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
)

// newServerForAdminTest builds a one-tenant Server ("default") wired end-to-end
// so setupRoutes() and AddTenant paths are both exercisable. The default
// tenant intentionally has no channels so peers can be added without
// colliding on bot tokens unless the test explicitly sets one.
func newServerForAdminTest(t *testing.T, tenantsRoot string, defaultCh []core.ChannelConfig) *Server {
	t.Helper()
	cfg := &core.Config{}
	cfg.Channels = defaultCh
	deps := buildTenantDeps(t, tenantsRoot, DefaultTenantID, cfg)
	return New([]*TenantDeps{deps}, "test-admin")
}

// stageTenantOnDisk writes a minimum-viable config.toml under tenantsRoot/id
// without InitTenant (which also enforces allow-list rules we'd fight in
// tests). AddTenant → OpenTenantDeps reads this config unchanged.
func stageTenantOnDisk(t *testing.T, tenantsRoot, id string, isFamily bool, channels []core.ChannelConfig) {
	t.Helper()
	dir := filepath.Join(tenantsRoot, id)
	tt := &core.Tenant{ID: id, BaseDir: dir}
	if err := tt.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs %s: %v", id, err)
	}
	cfg := core.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "test-dummy"
	cfg.LLM.Model = "claude-test"
	cfg.IsFamily = isFamily
	cfg.Channels = channels
	if err := core.WriteConfigAtomic(&cfg, filepath.Join(dir, "config.toml")); err != nil {
		t.Fatalf("write config %s: %v", id, err)
	}
}

// tenantForDirectAdd constructs a *core.Tenant whose Config is already
// populated — lets unit tests call AddTenant without a round-trip through
// config.toml on disk. EnsureDirs is deferred to OpenTenantDeps.
func tenantForDirectAdd(tenantsRoot, id string, isFamily bool, channels []core.ChannelConfig) *core.Tenant {
	cfg := core.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "test-dummy"
	cfg.LLM.Model = "claude-test"
	cfg.IsFamily = isFamily
	cfg.Channels = channels
	return &core.Tenant{
		ID:      id,
		BaseDir: filepath.Join(tenantsRoot, id),
		Config:  &cfg,
	}
}

// --- Server.AddTenant (unit) ---

func TestAddTenant_RegistersOnAllThreeStores(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	alice := tenantForDirectAdd(root, "alice", true, nil)
	if err := srv.AddTenant(alice); err != nil {
		t.Fatalf("AddTenant: %v", err)
	}

	if srv.tenants.Session("alice") == nil {
		t.Error("alice missing from TenantRouter after AddTenant")
	}
	if srv.tenantRegistry.Get("alice") == nil {
		t.Error("alice missing from TenantRegistry after AddTenant")
	}
	found := false
	for _, peer := range srv.tenantList {
		if peer != nil && peer.ID == "alice" {
			found = true
		}
	}
	if !found {
		t.Error("alice missing from tenantList after AddTenant")
	}
}

func TestAddTenant_DuplicateReturnsSentinel(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	first := tenantForDirectAdd(root, "alice", true, nil)
	if err := srv.AddTenant(first); err != nil {
		t.Fatalf("first AddTenant: %v", err)
	}
	second := tenantForDirectAdd(root, "alice", true, nil)
	err := srv.AddTenant(second)
	if !errors.Is(err, ErrTenantAlreadyActive) {
		t.Fatalf("want ErrTenantAlreadyActive, got %v", err)
	}
}

// TestAddTenant_StoresDeps guards the close-target wiring: AddTenant must
// retain the *TenantDeps so RemoveTenant can close the SQLite store and
// shut down the MCP registry symmetrically. Without this, every hot-added
// tenant would leak its deps on removal.
func TestAddTenant_StoresDeps(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	alice := tenantForDirectAdd(root, "alice", true, nil)
	if err := srv.AddTenant(alice); err != nil {
		t.Fatalf("AddTenant: %v", err)
	}

	srv.tenantMu.Lock()
	td, ok := srv.tenantDeps["alice"]
	srv.tenantMu.Unlock()
	if !ok || td == nil {
		t.Fatalf("tenantDeps[alice] missing after AddTenant: ok=%v td=%v", ok, td)
	}
	if td.Tenant == nil || td.Tenant.ID != "alice" {
		t.Errorf("stored td points at wrong tenant: %+v", td.Tenant)
	}
	if td.Store == nil {
		t.Error("stored td has nil Store — Close would no-op")
	}
}

// TestAddTenant_RollbackRemovesDeps: if AddTenant fails after storing deps,
// the map entry must be cleaned up. Use a family-with-channels failure which
// rejects AFTER ValidateTenantID but BEFORE tenantDeps insertion — we just
// need to ensure the map never ends up with a dangling entry.
func TestAddTenant_RollbackPreservesDepsMap(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	// family with channels is rejected by ValidateFamilyTenants BEFORE OpenTenantDeps
	fam := tenantForDirectAdd(root, "family", true, []core.ChannelConfig{
		{ChannelType: core.ChannelTelegram, Token: "f"},
	})
	_ = srv.AddTenant(fam)

	srv.tenantMu.Lock()
	_, ok := srv.tenantDeps["family"]
	srv.tenantMu.Unlock()
	if ok {
		t.Error("tenantDeps[family] leaked after rejected AddTenant")
	}
}

func TestAddTenant_NilInputs(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	if err := srv.AddTenant(nil); err == nil {
		t.Error("AddTenant(nil): want error, got nil")
	}
	if err := srv.AddTenant(&core.Tenant{ID: "alice"}); err == nil {
		t.Error("AddTenant with nil Config: want error, got nil")
	}
}

func TestAddTenant_InvalidIDRejected(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	bad := tenantForDirectAdd(root, "../escape", true, nil)
	if err := srv.AddTenant(bad); err == nil {
		t.Error("AddTenant(../escape): want error, got nil")
	}
}

// TestAddTenant_ChannelCollision exercises the pre-spawn validation path:
// if the would-be tenant declares a bot token already claimed by a live
// peer, AddTenant must reject and leave every registry untouched.
func TestAddTenant_ChannelCollision(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, []core.ChannelConfig{
		{ChannelType: core.ChannelTelegram, Token: "shared-tok"},
	})

	// "bob" is not family, declares the same telegram token → must fail.
	bob := tenantForDirectAdd(root, "bob", false, []core.ChannelConfig{
		{ChannelType: core.ChannelTelegram, Token: "shared-tok"},
	})
	if err := srv.AddTenant(bob); err == nil {
		t.Fatal("expected channel-collision rejection, got nil")
	}

	// Side-effect assertion — no leak across registries.
	if srv.tenants.Session("bob") != nil {
		t.Error("bob leaked into TenantRouter after rejected AddTenant")
	}
	if srv.tenantRegistry.Get("bob") != nil {
		t.Error("bob leaked into TenantRegistry after rejected AddTenant")
	}
	for _, peer := range srv.tenantList {
		if peer != nil && peer.ID == "bob" {
			t.Fatal("bob leaked into tenantList after rejected AddTenant")
		}
	}
}

// TestAddTenant_FamilyWithChannelsRejected guards the family invariant:
// a hot-added family tenant must never declare channels.
func TestAddTenant_FamilyWithChannelsRejected(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	fam := tenantForDirectAdd(root, "family", true, []core.ChannelConfig{
		{ChannelType: core.ChannelTelegram, Token: "f"},
	})
	if err := srv.AddTenant(fam); err == nil {
		t.Error("family tenant with channels: want error, got nil")
	}
	if srv.tenantRegistry.Get("family") != nil {
		t.Error("family leaked into registry despite validation failure")
	}
}

// --- Server.RemoveTenant (unit) ---

// TestRemoveTenant_HappyPath mirrors AddTenant end-to-end: after remove, none
// of the five registries (router, list, registry, deps map, session cache)
// retain the tenant. Closes AC-RM1 a/b at the server layer (channel-less).
func TestRemoveTenant_HappyPath(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	alice := tenantForDirectAdd(root, "alice", true, nil)
	if err := srv.AddTenant(alice); err != nil {
		t.Fatalf("AddTenant: %v", err)
	}

	if err := srv.RemoveTenant("alice"); err != nil {
		t.Fatalf("RemoveTenant: %v", err)
	}

	if sess := srv.tenants.Session("alice"); sess != nil {
		t.Error("alice still in TenantRouter after RemoveTenant")
	}
	if srv.tenantRegistry.Get("alice") != nil {
		t.Error("alice still in TenantRegistry after RemoveTenant")
	}
	for _, peer := range srv.tenantList {
		if peer != nil && peer.ID == "alice" {
			t.Fatal("alice still in tenantList after RemoveTenant")
		}
	}
	srv.tenantMu.Lock()
	_, ok := srv.tenantDeps["alice"]
	srv.tenantMu.Unlock()
	if ok {
		t.Error("alice still in tenantDeps after RemoveTenant")
	}
}

// TestRemoveTenant_NotActive returns a distinct sentinel so the HTTP layer
// can map it to 404 (not 500). AC-RM3 server-side piece — CLI layer handles
// the user-facing message.
func TestRemoveTenant_NotActive(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	err := srv.RemoveTenant("zzz")
	if !errors.Is(err, ErrTenantNotActive) {
		t.Fatalf("want ErrTenantNotActive, got %v", err)
	}
}

// TestRemoveTenant_InvalidIDRejectedStateUnchanged guards AC-RM5's spirit:
// any pre-reconcile rejection (here: malformed ID) must leave every registry
// untouched so a retry picks up clean state. Using ValidateTenantID as the
// failure lever because Reconcile's channel-stop path aggregates errors
// internally (slog) rather than propagating them — the "abort before mutate"
// invariant is what matters for AC-RM5.
func TestRemoveTenant_InvalidIDRejectedStateUnchanged(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	alice := tenantForDirectAdd(root, "alice", true, nil)
	if err := srv.AddTenant(alice); err != nil {
		t.Fatalf("AddTenant: %v", err)
	}

	// Malformed ID rejected at entry — alice untouched.
	if err := srv.RemoveTenant("../escape"); err == nil {
		t.Error("want validation error, got nil")
	}

	if srv.tenants.Session("alice") == nil {
		t.Error("alice dropped from router after rejected RemoveTenant")
	}
	if srv.tenantRegistry.Get("alice") == nil {
		t.Error("alice dropped from registry after rejected RemoveTenant")
	}
	srv.tenantMu.Lock()
	_, ok := srv.tenantDeps["alice"]
	srv.tenantMu.Unlock()
	if !ok {
		t.Error("alice dropped from tenantDeps after rejected RemoveTenant")
	}
}

// --- HTTP handler ---

func postAdminTenant(t *testing.T, srv *Server, tenantID, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"tenant_id": tenantID})
	req := httptest.NewRequest("POST", "/api/v1/admin/tenants", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

// deleteAdminTenant posts to POST /api/v1/admin/tenants/{id}/delete so we
// can assert the full Chi route chain (localhost gate + handler + RemoveTenant).
func deleteAdminTenant(t *testing.T, srv *Server, tenantID, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/admin/tenants/"+tenantID+"/delete", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	return w
}

func TestHandleAdminTenantRemove_Success(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)
	stageTenantOnDisk(t, root, "alice", true, nil)
	if w := postAdminTenant(t, srv, "alice", "127.0.0.1:1"); w.Code != http.StatusOK {
		t.Fatalf("setup: want 200, got %d: %s", w.Code, w.Body.String())
	}

	w := deleteAdminTenant(t, srv, "alice", "127.0.0.1:1")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "deactivated" {
		t.Errorf("status = %v, want \"deactivated\"", resp["status"])
	}
	if resp["tenant_id"] != "alice" {
		t.Errorf("tenant_id = %v, want \"alice\"", resp["tenant_id"])
	}
	if srv.tenants.Session("alice") != nil {
		t.Error("alice still registered after 200 deactivation")
	}
}

func TestHandleAdminTenantRemove_NotActive404(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	w := deleteAdminTenant(t, srv, "ghost", "127.0.0.1:1")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAdminTenantRemove_InvalidID400(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	w := deleteAdminTenant(t, srv, "../escape", "127.0.0.1:1")
	// Chi's URL parameter contains "../escape" URL-decoded — either 400 or 404
	// is acceptable (invalid vs not found). What matters: not 200, not 500.
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Fatalf("want 400 or 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAdminTenantRemove_NonLocalhostRejected(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)
	stageTenantOnDisk(t, root, "alice", true, nil)
	if w := postAdminTenant(t, srv, "alice", "127.0.0.1:1"); w.Code != http.StatusOK {
		t.Fatalf("setup: want 200, got %d", w.Code)
	}

	w := deleteAdminTenant(t, srv, "alice", "10.0.0.5:44444")
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if srv.tenants.Session("alice") == nil {
		t.Error("alice dropped despite localhost-gate rejection")
	}
}

func TestHandleAdminTenantAdd_Success(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)
	stageTenantOnDisk(t, root, "alice", true, nil)

	w := postAdminTenant(t, srv, "alice", "127.0.0.1:54321")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "activated" {
		t.Errorf("status = %v, want \"activated\"", resp["status"])
	}
	if resp["tenant_id"] != "alice" {
		t.Errorf("tenant_id = %v, want \"alice\"", resp["tenant_id"])
	}
	if srv.tenants.Session("alice") == nil {
		t.Error("alice not registered after 200")
	}
}

func TestHandleAdminTenantAdd_NotFoundOnDisk(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	w := postAdminTenant(t, srv, "ghost", "127.0.0.1:1")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAdminTenantAdd_BlankTenantID(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	w := postAdminTenant(t, srv, "", "127.0.0.1:1")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAdminTenantAdd_InvalidID(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)

	w := postAdminTenant(t, srv, "../escape", "127.0.0.1:1")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAdminTenantAdd_DuplicateReturns409(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)
	stageTenantOnDisk(t, root, "alice", true, nil)

	if w := postAdminTenant(t, srv, "alice", "127.0.0.1:1"); w.Code != http.StatusOK {
		t.Fatalf("first: want 200, got %d: %s", w.Code, w.Body.String())
	}
	w := postAdminTenant(t, srv, "alice", "127.0.0.1:1")
	if w.Code != http.StatusConflict {
		t.Fatalf("second: want 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandleAdminTenantAdd_NonLocalhostRejected guards the localhost-only
// gate that sits atop the standard /api/v1 API-key check. A request from a
// non-loopback address must be rejected BEFORE AddTenant runs — otherwise a
// stolen API key would give remote tenant provisioning.
func TestHandleAdminTenantAdd_NonLocalhostRejected(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)
	stageTenantOnDisk(t, root, "alice", true, nil)

	w := postAdminTenant(t, srv, "alice", "10.0.0.5:44444")
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
	if srv.tenants.Session("alice") != nil {
		t.Error("alice registered despite localhost-gate rejection")
	}
}

// TestHandleAdminTenantAdd_HotReloadRouterReflectsImmediately is the AC-U3
// end-to-end guard: once POST /api/v1/admin/tenants returns 200, the
// dispatch path must see the new tenant *without* a daemon restart. A
// regression here would push every new family member through a kill-9 +
// relaunch, which is the exact pain AC-U3 exists to eliminate. The 30s
// budget comes directly from the spec; in practice AddTenant is synchronous
// and completes in milliseconds, so the bounded wait also guards against a
// regression where hot-add silently defers work to a background goroutine.
func TestHandleAdminTenantAdd_HotReloadRouterReflectsImmediately(t *testing.T) {
	root := t.TempDir()
	srv := newServerForAdminTest(t, root, nil)
	stageTenantOnDisk(t, root, "charlie", true, nil)

	// Pre-add: charlie is unknown — Route() must drop with no fallback.
	preDrop := srv.tenants.DropCount()
	if got := srv.tenants.Route(core.Event{
		Type:     core.EventTelegram,
		TenantID: "charlie",
	}); got != nil {
		t.Fatal("pre-add: charlie should drop (no fallback) but Route returned a session")
	}
	if got := srv.tenants.DropCount(); got != preDrop+1 {
		t.Errorf("pre-add DropCount = %d, want %d", got, preDrop+1)
	}

	// Hot-add — enforce the 30s AC-U3 budget on the HTTP round-trip. The
	// goroutine + select pattern also catches the regression where AddTenant
	// blocks indefinitely (e.g. a bad channel spawn waiting on a network
	// dial) instead of returning an error promptly.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postAdminTenant(t, srv, "charlie", "127.0.0.1:4242")
	}()
	var w *httptest.ResponseRecorder
	select {
	case w = <-done:
	case <-ctx.Done():
		t.Fatal("AddTenant exceeded 30s AC-U3 budget")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("AddTenant HTTP status = %d, want 200: %s", w.Code, w.Body.String())
	}
	t.Logf("AddTenant completed in %v (AC-U3 budget: 30s)", time.Since(start))

	// Post-add: charlie is immediately in the router and fresh events route
	// cleanly without bumping the drop counter.
	if got := srv.tenants.Session("charlie"); got == nil {
		t.Fatal("post-add: charlie missing from router — hot-reload failed")
	}
	dropBefore := srv.tenants.DropCount()
	if got := srv.tenants.Route(core.Event{
		Type:     core.EventTelegram,
		TenantID: "charlie",
	}); got == nil {
		t.Fatal("post-add: charlie event dropped — hot-reload did not reach dispatch path")
	}
	if got := srv.tenants.DropCount(); got != dropBefore {
		t.Errorf("post-add: DropCount advanced (%d → %d) — legitimate traffic is being dropped", dropBefore, got)
	}
}
