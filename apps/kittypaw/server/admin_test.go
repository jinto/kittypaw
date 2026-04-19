package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
