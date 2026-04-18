package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

// newShareFixture stands up a two-tenant topology on disk (family owner +
// alice reader) with a weather.json that only alice is cleared to read.
// The fixture returns a Session wired as alice so each test exercises
// the same execution path the sandbox uses at runtime — the exported
// executeShare is the only thing under test; everything else is plumbing.
func newShareFixture(t *testing.T) (sess *Session, familyDir string) {
	t.Helper()
	root := t.TempDir()

	familyDir = filepath.Join(root, "tenants", "family")
	aliceDir := filepath.Join(root, "tenants", "alice")
	if err := os.MkdirAll(filepath.Join(familyDir, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir family: %v", err)
	}
	if err := os.MkdirAll(aliceDir, 0o755); err != nil {
		t.Fatalf("mkdir alice: %v", err)
	}
	if err := os.WriteFile(filepath.Join(familyDir, "memory", "weather.json"), []byte(`{"t":18}`), 0o644); err != nil {
		t.Fatalf("write weather: %v", err)
	}

	reg := core.NewTenantRegistry(filepath.Join(root, "tenants"), "family")
	reg.Register(&core.Tenant{
		ID:      "family",
		BaseDir: familyDir,
		Config: &core.Config{
			IsFamily: true,
			Share:    map[string]core.ShareConfig{"alice": {Read: []string{"memory/weather.json"}}},
		},
	})
	reg.Register(&core.Tenant{ID: "alice", BaseDir: aliceDir, Config: &core.Config{}})

	sess = &Session{
		Config:         &core.Config{},
		TenantID:       "alice",
		TenantRegistry: reg,
	}
	return sess, familyDir
}

func mustCall(t *testing.T, tenantID, path string) core.SkillCall {
	t.Helper()
	tid, _ := json.Marshal(tenantID)
	p, _ := json.Marshal(path)
	return core.SkillCall{SkillName: "Share", Method: "read", Args: []json.RawMessage{tid, p}}
}

// TestShareRead_Success pins the happy path — alice asking for an
// allowlisted path returns the file body. The pair (Session.TenantID,
// target Tenant from registry) is what makes the cross-tenant check
// meaningful; without the session field wired up the whole surface is
// dead code.
func TestShareRead_Success(t *testing.T) {
	sess, _ := newShareFixture(t)

	out, err := executeShare(context.Background(), mustCall(t, "family", "memory/weather.json"), sess)
	if err != nil {
		t.Fatalf("executeShare: %v", err)
	}
	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["content"] != `{"t":18}` {
		t.Errorf("content = %q", resp["content"])
	}
}

// TestShareRead_AllowlistMiss checks that a path outside the owner's
// Read allowlist surfaces as a JS-level error, not a filesystem leak.
// The reject path is the critical one: it's where a hostile or sloppy
// skill would try to escalate, so the failure must be explicit.
func TestShareRead_AllowlistMiss(t *testing.T) {
	sess, familyDir := newShareFixture(t)
	// Put an unlisted file on disk; the allowlist miss must fire before
	// any filesystem operation.
	if err := os.WriteFile(filepath.Join(familyDir, "memory", "private.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write private: %v", err)
	}

	out, err := executeShare(context.Background(), mustCall(t, "family", "memory/private.json"), sess)
	if err != nil {
		t.Fatalf("executeShare: %v", err)
	}
	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp["error"], "allowlist") {
		t.Errorf("expected allowlist error, got %q", resp["error"])
	}
}

// TestShareRead_UnknownTenant rejects typos at the API boundary — a
// skill asking to read from "grandma" when no such tenant exists must
// NOT fall through to some default tenant lookup. The whole value of
// TenantRouter's strict routing vanishes if share reads silently
// rewrite unknown targets.
func TestShareRead_UnknownTenant(t *testing.T) {
	sess, _ := newShareFixture(t)

	out, _ := executeShare(context.Background(), mustCall(t, "grandma", "memory/weather.json"), sess)
	var resp map[string]string
	_ = json.Unmarshal([]byte(out), &resp)
	if !strings.Contains(resp["error"], "unknown tenant") {
		t.Errorf("expected unknown tenant error, got %q", resp["error"])
	}
}

// TestShareRead_AuditLog verifies the operational contract — every successful
// cross-tenant read emits a structured slog record with {from, to, path,
// bytes}. Silent success would make data-flow auditing impossible when a
// deployment goes sideways.
func TestShareRead_AuditLog(t *testing.T) {
	sess, _ := newShareFixture(t)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if _, err := executeShare(context.Background(), mustCall(t, "family", "memory/weather.json"), sess); err != nil {
		t.Fatalf("executeShare: %v", err)
	}

	log := buf.String()
	if !strings.Contains(log, `"msg":"cross_tenant_read"`) {
		t.Errorf("audit record missing: %s", log)
	}
	if !strings.Contains(log, `"from":"alice"`) || !strings.Contains(log, `"to":"family"`) {
		t.Errorf("audit record missing tenant labels: %s", log)
	}
}

// TestShareRead_NoRegistry protects against the "Session wired without
// tenant context" case — e.g. a legacy single-tenant daemon or a test
// setup that forgot to inject the registry. Rather than panic on nil,
// surface a clear "unavailable" error so skill authors see what's
// missing instead of debugging a segfault.
func TestShareRead_NoRegistry(t *testing.T) {
	sess := &Session{Config: &core.Config{}} // TenantID="", TenantRegistry=nil

	out, _ := executeShare(context.Background(), mustCall(t, "family", "x"), sess)
	var resp map[string]string
	_ = json.Unmarshal([]byte(out), &resp)
	if !strings.Contains(resp["error"], "unavailable") {
		t.Errorf("expected unavailable error, got %q", resp["error"])
	}
}
