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
	"github.com/jinto/kittypaw/sandbox"
)

// TestFamily_ShareReadE2E drives the full cross-tenant read stack through the
// JS sandbox: alice's skill calls `Share.read("family", ...)`, the call
// crosses the resolver into executeShare, passes ValidateSharedReadPath
// against family's `[share.alice]` allowlist, and returns the file content.
// bob — not allowlisted — gets a denial, and the audit log captures both
// outcomes. Without this end-to-end, the unit tests green-light pieces the
// JS layer can't actually reach.
func TestFamily_ShareReadE2E(t *testing.T) {
	root := t.TempDir()

	// --- tenant layout ---
	family := makeTenant(t, root, "family", &core.Config{
		IsFamily: true,
		Share: map[string]core.ShareConfig{
			"alice": {Read: []string{"memory/weather.json"}},
		},
	})
	alice := makeTenant(t, root, "alice", &core.Config{})
	bob := makeTenant(t, root, "bob", &core.Config{})

	// Drop a file family wants to share.
	writeTenantFile(t, filepath.Join(family.BaseDir, "memory", "weather.json"),
		`{"today":"sunny","high":22}`)

	registry := core.NewTenantRegistry(root, "alice")
	registry.Register(family)
	registry.Register(alice)
	registry.Register(bob)

	// Capture slog to verify the cross_tenant_read audit record fires.
	var logBuf bytes.Buffer
	origLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	sbox := sandbox.New(core.SandboxConfig{TimeoutSecs: 5})

	// --- alice: allowlisted reader → success ---
	aliceSess := &Session{
		Sandbox:        sbox,
		Config:         alice.Config,
		TenantID:       alice.ID,
		TenantRegistry: registry,
	}
	resolver := func(ctx context.Context, call core.SkillCall) (string, error) {
		return resolveSkillCall(ctx, call, aliceSess, nil)
	}
	code := `
		var r = Share.read("family", "memory/weather.json");
		return r.content;
	`
	result, err := sbox.ExecuteWithResolver(context.Background(), code, nil, resolver)
	if err != nil {
		t.Fatalf("alice sandbox: %v", err)
	}
	if !result.Success {
		t.Fatalf("alice sandbox errored: %s", result.Error)
	}
	if !strings.Contains(result.Output, `"today":"sunny"`) {
		t.Errorf("alice did not receive file content: %q", result.Output)
	}

	if !strings.Contains(logBuf.String(), `"cross_tenant_read"`) {
		t.Errorf("missing cross_tenant_read audit log; got: %s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), `"from":"alice"`) {
		t.Errorf("audit log missing reader identity; got: %s", logBuf.String())
	}

	// --- bob: not allowlisted → denied ---
	logBuf.Reset()
	bobSess := &Session{
		Sandbox:        sbox,
		Config:         bob.Config,
		TenantID:       bob.ID,
		TenantRegistry: registry,
	}
	bobResolver := func(ctx context.Context, call core.SkillCall) (string, error) {
		return resolveSkillCall(ctx, call, bobSess, nil)
	}
	denyCode := `
		var r = Share.read("family", "memory/weather.json");
		return JSON.stringify(r);
	`
	result, err = sbox.ExecuteWithResolver(context.Background(), denyCode, nil, bobResolver)
	if err != nil {
		t.Fatalf("bob sandbox: %v", err)
	}
	if !result.Success {
		t.Fatalf("bob sandbox errored: %s", result.Error)
	}
	// Denial is returned as an `error` field, not a thrown exception — skill
	// code is expected to branch on response shape.
	if !strings.Contains(result.Output, `"error"`) {
		t.Errorf("bob should see error field, got: %q", result.Output)
	}
	if !strings.Contains(logBuf.String(), "cross_tenant_read_rejected") {
		t.Errorf("missing rejection audit for bob; got: %s", logBuf.String())
	}
}

// TestFamily_FanoutE2E proves the family → personal push path end-to-end.
// A family skill calls Fanout.send("alice", …) through the actual
// Sandbox, the event lands on eventCh as EventFamilyPush with the target
// tenantID, and alice never sees the Fanout global at all (defense in
// depth — a personal skill probing `typeof Fanout` hits undefined).
func TestFamily_FanoutE2E(t *testing.T) {
	root := t.TempDir()
	family := makeTenant(t, root, "family", &core.Config{IsFamily: true})
	alice := makeTenant(t, root, "alice", &core.Config{})

	registry := core.NewTenantRegistry(root, "alice")
	registry.Register(family)
	registry.Register(alice)

	eventCh := make(chan core.Event, 4)
	fanout := core.NewChannelFanout(eventCh, registry, "family")

	sbox := sandbox.New(core.SandboxConfig{TimeoutSecs: 5})

	// --- family: Fanout wired → push succeeds ---
	famSess := &Session{
		Sandbox:        sbox,
		Config:         family.Config,
		TenantID:       family.ID,
		TenantRegistry: registry,
		Fanout:         fanout,
	}
	famResolver := func(ctx context.Context, call core.SkillCall) (string, error) {
		return resolveSkillCall(ctx, call, famSess, nil)
	}
	code := `
		if (typeof Fanout !== "object") return "missing:" + typeof Fanout;
		var r = Fanout.send("alice", {text: "🍚 저녁 준비됐어!"});
		return JSON.stringify(r);
	`
	result, err := sbox.ExecuteWithResolverOpts(context.Background(), code, nil, famResolver,
		sandbox.Options{ExposeFanout: famSess.Fanout != nil})
	if err != nil {
		t.Fatalf("family sandbox: %v", err)
	}
	if !result.Success {
		t.Fatalf("family sandbox errored: %s", result.Error)
	}
	if !strings.Contains(result.Output, `"success":true`) {
		t.Errorf("family expected success, got %q", result.Output)
	}

	select {
	case ev := <-eventCh:
		if ev.Type != core.EventFamilyPush {
			t.Errorf("expected EventFamilyPush, got %q", ev.Type)
		}
		if ev.TenantID != "alice" {
			t.Errorf("expected target=alice, got %q", ev.TenantID)
		}
		var body core.FanoutPayload
		if err := json.Unmarshal(ev.Payload, &body); err != nil {
			t.Fatalf("payload unmarshal: %v", err)
		}
		if !strings.Contains(body.Text, "저녁 준비됐어") {
			t.Errorf("payload text wrong: %q", body.Text)
		}
	default:
		t.Fatal("expected EventFamilyPush on channel; nothing published")
	}

	// --- alice: no Fanout wired → JS global hidden ---
	aliceSess := &Session{
		Sandbox:        sbox,
		Config:         alice.Config,
		TenantID:       alice.ID,
		TenantRegistry: registry,
	}
	aliceResolver := func(ctx context.Context, call core.SkillCall) (string, error) {
		return resolveSkillCall(ctx, call, aliceSess, nil)
	}
	probeCode := `return typeof Fanout;`
	probe, err := sbox.ExecuteWithResolverOpts(context.Background(), probeCode, nil, aliceResolver,
		sandbox.Options{ExposeFanout: aliceSess.Fanout != nil})
	if err != nil {
		t.Fatalf("alice probe: %v", err)
	}
	if probe.Output != "undefined" {
		t.Errorf("personal tenant must not see Fanout; got %q", probe.Output)
	}
}

// --- helpers ---

func makeTenant(t *testing.T, root, id string, cfg *core.Config) *core.Tenant {
	t.Helper()
	baseDir := filepath.Join(root, id)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", baseDir, err)
	}
	return &core.Tenant{ID: id, BaseDir: baseDir, Config: cfg}
}

func writeTenantFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
