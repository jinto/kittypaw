package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/engine"
)

// emittingStub is a test Channel that emits a single tagged Event on
// request and then blocks until ctx is canceled. It mirrors what a real
// channel does (Telegram, Slack, …) but without the network I/O, so we
// can verify the full event→router→session dispatch path.
type emittingStub struct {
	name     string
	tenantID string
	fire     chan core.Event
}

func newEmittingStub(name, tenantID string) *emittingStub {
	return &emittingStub{
		name:     name,
		tenantID: tenantID,
		fire:     make(chan core.Event, 1),
	}
}

func (e *emittingStub) Start(ctx context.Context, eventCh chan<- core.Event) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-e.fire:
			select {
			case eventCh <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (e *emittingStub) SendResponse(_ context.Context, _, _ string) error { return nil }
func (e *emittingStub) Name() string                                      { return e.name }

// emit tells the stub to produce an Event tagged with the stub's tenantID.
func (e *emittingStub) emit(text string) {
	payload, _ := json.Marshal(core.ChatPayload{ChatID: "c1", Text: text})
	e.fire <- core.Event{
		Type:     core.EventType(e.name),
		TenantID: e.tenantID,
		Payload:  payload,
	}
}

// TestTenantIsolation_EndToEnd enforces AC-T3: a message that enters via
// alice's channel lands on alice's session and never on bob's. A regression
// here would be a cross-tenant leak — the primary privacy risk the
// TenantRouter is designed to prevent.
func TestTenantIsolation_EndToEnd(t *testing.T) {
	aliceSess := &engine.Session{BaseDir: "/tmp/alice"}
	bobSess := &engine.Session{BaseDir: "/tmp/bob"}

	router := NewTenantRouter()
	router.Register("alice", aliceSess)
	router.Register("bob", bobSess)

	// Alice's event hits alice only.
	alicePayload, _ := json.Marshal(core.ChatPayload{Text: "alice msg"})
	got := router.Route(core.Event{
		Type:     core.EventTelegram,
		TenantID: "alice",
		Payload:  alicePayload,
	})
	if got != aliceSess {
		t.Errorf("alice event routed to %p, want aliceSess %p", got, aliceSess)
	}

	// Bob's event hits bob only.
	got = router.Route(core.Event{
		Type:     core.EventTelegram,
		TenantID: "bob",
	})
	if got != bobSess {
		t.Errorf("bob event routed to %p, want bobSess %p", got, bobSess)
	}

	// Unknown tenant drops — no fallback to alice even though alice was
	// registered first.
	if got := router.Route(core.Event{TenantID: "charlie"}); got != nil {
		t.Error("unknown tenant must drop (C1 no-fallback)")
	}
}

// TestTenantIsolation_ChannelSpawner_SameTypeTwoTenants enforces AC-T3
// from the spawner angle: two tenants can have telegram bots whose tokens
// differ, and each routes back to its owner's channel for SendResponse.
// Without composite-key isolation, bob's TrySpawn would silently skip
// because "telegram" is already registered under alice.
func TestTenantIsolation_ChannelSpawner_SameTypeTwoTenants(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	alice := newEmittingStub("telegram", "alice")
	bob := newEmittingStub("telegram", "bob")

	if err := sp.TrySpawn("alice", alice, core.ChannelConfig{
		ChannelType: core.ChannelTelegram, Token: "alice-tok",
	}); err != nil {
		t.Fatalf("alice TrySpawn: %v", err)
	}
	if err := sp.TrySpawn("bob", bob, core.ChannelConfig{
		ChannelType: core.ChannelTelegram, Token: "bob-tok",
	}); err != nil {
		t.Fatalf("bob TrySpawn: %v", err)
	}

	if ch, ok := sp.GetChannel("alice", core.EventTelegram); !ok || ch != alice {
		t.Errorf("alice GetChannel mismatch: got %v", ch)
	}
	if ch, ok := sp.GetChannel("bob", core.EventTelegram); !ok || ch != bob {
		t.Errorf("bob GetChannel mismatch: got %v", ch)
	}

	// Verify events emitted by each channel carry the right TenantID.
	alice.emit("from alice")
	bob.emit("from bob")

	got := map[string]string{}
	timeout := time.After(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case ev := <-eventCh:
			got[ev.TenantID] = string(ev.Payload)
		case <-timeout:
			t.Fatalf("timed out after %d events", i)
		}
	}
	if _, ok := got["alice"]; !ok {
		t.Error("alice's event never arrived on eventCh")
	}
	if _, ok := got["bob"]; !ok {
		t.Error("bob's event never arrived on eventCh")
	}

	sp.StopAll()
}

// TestTenantIsolation_DuplicateTokenRejected enforces C3: two tenants
// declaring the same Telegram bot token must be flagged at config
// validation, not after both bots are started and racing on getUpdates.
func TestTenantIsolation_DuplicateTokenRejected(t *testing.T) {
	tenantChannels := map[string][]core.ChannelConfig{
		"alice": {{ChannelType: core.ChannelTelegram, Token: "shared"}},
		"bob":   {{ChannelType: core.ChannelTelegram, Token: "shared"}},
	}
	if err := core.ValidateTenantChannels(tenantChannels); err == nil {
		t.Error("duplicate bot_token across tenants should have been rejected")
	}
}
