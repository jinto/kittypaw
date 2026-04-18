package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

// fakeFanout captures Send/Broadcast arguments for assertion. We don't
// reach for the real ChannelFanout here because engine-level tests should
// exercise the wiring, not the eventCh publishing behavior (that lives in
// core/fanout_test.go).
type fakeFanout struct {
	sentTo   string
	payload  core.FanoutPayload
	sendErr  error
	bcastErr error
	bcasts   int
}

func (f *fakeFanout) Send(_ context.Context, tenantID string, p core.FanoutPayload) error {
	f.sentTo = tenantID
	f.payload = p
	return f.sendErr
}

func (f *fakeFanout) Broadcast(_ context.Context, p core.FanoutPayload) error {
	f.bcasts++
	f.payload = p
	return f.bcastErr
}

func fanoutCall(t *testing.T, method string, args ...any) core.SkillCall {
	t.Helper()
	raw := make([]json.RawMessage, len(args))
	for i, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal arg: %v", err)
		}
		raw[i] = b
	}
	return core.SkillCall{SkillName: "Fanout", Method: method, Args: raw}
}

// TestFanout_SendRoutesToInterface confirms the executor plumbs skill
// arguments through to Session.Fanout. Without this, the JS binding is
// dead — skill code would call Fanout.send but nothing would reach the
// event channel.
func TestFanout_SendRoutesToInterface(t *testing.T) {
	f := &fakeFanout{}
	sess := &Session{Fanout: f}

	out, err := executeFanout(context.Background(), fanoutCall(t, "send", "alice", map[string]any{"text": "안녕"}), sess)
	if err != nil {
		t.Fatalf("executeFanout: %v", err)
	}
	var resp map[string]any
	_ = json.Unmarshal([]byte(out), &resp)
	if ok, _ := resp["success"].(bool); !ok {
		t.Errorf("expected success=true, got %v", resp)
	}
	if f.sentTo != "alice" || f.payload.Text != "안녕" {
		t.Errorf("fanout received wrong args: to=%q text=%q", f.sentTo, f.payload.Text)
	}
}

// TestFanout_NoFanoutConfigured is the belt-and-braces check for the
// "personal tenant tries to fanout" scenario — even if sandbox-level
// blocking is somehow bypassed (e.g. a test harness wires the stub
// anyway), executor must reject. Defense in depth.
func TestFanout_NoFanoutConfigured(t *testing.T) {
	sess := &Session{} // Fanout nil — personal session

	out, _ := executeFanout(context.Background(), fanoutCall(t, "send", "alice", map[string]any{"text": "x"}), sess)
	var resp map[string]string
	_ = json.Unmarshal([]byte(out), &resp)
	if !strings.Contains(resp["error"], "not available") && !strings.Contains(resp["error"], "unavailable") {
		t.Errorf("expected unavailable error, got %q", resp["error"])
	}
}

// TestFanout_PropagatesError surfaces core.Fanout sentinels so skill
// authors see "fanout: unknown target tenant" instead of a generic error.
func TestFanout_PropagatesError(t *testing.T) {
	f := &fakeFanout{sendErr: core.ErrFanoutUnknownTenant}
	sess := &Session{Fanout: f}

	out, _ := executeFanout(context.Background(), fanoutCall(t, "send", "ghost", map[string]any{"text": "x"}), sess)
	var resp map[string]string
	_ = json.Unmarshal([]byte(out), &resp)
	if !strings.Contains(resp["error"], "unknown target tenant") {
		t.Errorf("expected fanout error in response, got %q", resp["error"])
	}
}

// TestFanout_Broadcast covers the second method surface. The payload
// shape is identical to Send; only the routing differs, so mainly we're
// checking the method dispatch is wired.
func TestFanout_Broadcast(t *testing.T) {
	f := &fakeFanout{}
	sess := &Session{Fanout: f}

	out, err := executeFanout(context.Background(), fanoutCall(t, "broadcast", map[string]any{"text": "all hands"}), sess)
	if err != nil {
		t.Fatalf("executeFanout broadcast: %v", err)
	}
	var resp map[string]any
	_ = json.Unmarshal([]byte(out), &resp)
	if ok, _ := resp["success"].(bool); !ok {
		t.Errorf("expected success=true for broadcast, got %v", resp)
	}
	if f.bcasts != 1 || f.payload.Text != "all hands" {
		t.Errorf("broadcast wiring wrong: bcasts=%d text=%q", f.bcasts, f.payload.Text)
	}
}
