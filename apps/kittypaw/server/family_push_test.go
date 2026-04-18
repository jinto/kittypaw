package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jinto/kittypaw/channel"
	"github.com/jinto/kittypaw/core"
)

// pushCall captures one SendResponse invocation on mockPushChannel.
type pushCall struct {
	ChatID   string
	Response string
}

// mockPushChannel is a channel.Channel stub used to observe dispatchLoop
// delivering EventFamilyPush without relying on a live Telegram/Slack/etc.
// backend. Start blocks on ctx so ChannelSpawner's lifecycle is satisfied.
type mockPushChannel struct {
	name string
	mu   sync.Mutex
	sent []pushCall
}

func (m *mockPushChannel) Start(ctx context.Context, _ chan<- core.Event) error {
	<-ctx.Done()
	return nil
}

func (m *mockPushChannel) SendResponse(_ context.Context, chatID, response string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, pushCall{ChatID: chatID, Response: response})
	return nil
}

func (m *mockPushChannel) Name() string { return m.name }

func (m *mockPushChannel) calls() []pushCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]pushCall, len(m.sent))
	copy(out, m.sent)
	return out
}

// Compile-time interface check — if channel.Channel gains a required method
// we want the test stub to fail to compile rather than pass vacuously.
var _ channel.Channel = (*mockPushChannel)(nil)

// waitForCalls polls until the mock has >= n recorded calls or the deadline
// fires. Returns the captured calls. Used instead of time.Sleep because the
// dispatch goroutine is asynchronous and test flakes on slow CI.
func waitForCalls(t *testing.T, m *mockPushChannel, n int, d time.Duration) []pushCall {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c := m.calls(); len(c) >= n {
			return c
		}
		time.Sleep(5 * time.Millisecond)
	}
	return m.calls()
}

// buildFamilyPushServer wires a Server + spawner with a family tenant and a
// personal tenant whose Config declares the supplied channels. Returns the
// server, a shutdown func, and the personal tenant's registered mock channels
// keyed by EventType for assertion access.
func buildFamilyPushServer(t *testing.T, personalCfg *core.Config, mocks map[core.EventType]*mockPushChannel) (*Server, context.CancelFunc) {
	t.Helper()
	root := t.TempDir()

	familyDeps := buildTenantDeps(t, root, "family", &core.Config{IsFamily: true})
	aliceDeps := buildTenantDeps(t, root, "alice", personalCfg)

	srv := New([]*TenantDeps{familyDeps, aliceDeps}, "test")

	ctx, cancel := context.WithCancel(context.Background())
	srv.spawner = NewChannelSpawner(ctx, srv.eventCh)

	// Register each mock under alice's tenant ID keyed by its Name() (which
	// must match the resolved EventType string). TrySpawn's running map key
	// is `spawnerKey{TenantID, ChannelType: ch.Name()}`.
	for evType, m := range mocks {
		if m.name == "" {
			m.name = string(evType)
		}
		if err := srv.spawner.TrySpawn("alice", m, core.ChannelConfig{ChannelType: core.ChannelType(evType)}); err != nil {
			cancel()
			t.Fatalf("TrySpawn %s: %v", evType, err)
		}
	}

	go srv.dispatchLoop(ctx)

	return srv, func() {
		cancel()
		srv.spawner.StopAll()
	}
}

func pushEvent(t *testing.T, target string, p core.FanoutPayload) core.Event {
	t.Helper()
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return core.Event{Type: core.EventFamilyPush, TenantID: target, Payload: body}
}

// TestDispatchLoop_FamilyPush_DeliversToTargetChannel is the happy path — a
// family fanout push to alice with one telegram channel configured lands on
// that telegram channel's SendResponse with alice's AdminChatIDs[0] as the
// chat ID. Critically, the agent loop must NOT run (payload.Text is a
// finished outbound message, not an inbound chat that needs LLM processing).
// The mock's backing Session has Provider=nil — if the dispatch loop ever
// routed through session.Run, the test would error or panic instead of
// passing cleanly.
func TestDispatchLoop_FamilyPush_DeliversToTargetChannel(t *testing.T) {
	tg := &mockPushChannel{}
	srv, shutdown := buildFamilyPushServer(t, &core.Config{
		Channels:     []core.ChannelConfig{{ChannelType: core.ChannelTelegram}},
		AdminChatIDs: []string{"99999"},
	}, map[core.EventType]*mockPushChannel{core.EventTelegram: tg})
	defer shutdown()

	srv.eventCh <- pushEvent(t, "alice", core.FanoutPayload{Text: "🍚 저녁 준비됐어!"})

	calls := waitForCalls(t, tg, 1, 2*time.Second)
	if len(calls) != 1 {
		t.Fatalf("expected 1 SendResponse, got %d", len(calls))
	}
	if calls[0].ChatID != "99999" {
		t.Errorf("expected chatID 99999 (alice AdminChatIDs[0]), got %q", calls[0].ChatID)
	}
	if calls[0].Response != "🍚 저녁 준비됐어!" {
		t.Errorf("expected push text, got %q", calls[0].Response)
	}
}

// TestDispatchLoop_FamilyPush_ChannelHintRoutesToSpecificChannel pins the
// ChannelHint semantics: when alice has both telegram and slack wired, a
// push with ChannelHint="slack" must land on slack and NOT on telegram.
// Without this, every family push would default to the first-configured
// channel regardless of intent.
func TestDispatchLoop_FamilyPush_ChannelHintRoutesToSpecificChannel(t *testing.T) {
	tg := &mockPushChannel{}
	sl := &mockPushChannel{}
	srv, shutdown := buildFamilyPushServer(t, &core.Config{
		Channels: []core.ChannelConfig{
			{ChannelType: core.ChannelTelegram},
			{ChannelType: core.ChannelSlack},
		},
		AdminChatIDs: []string{"99999"},
	}, map[core.EventType]*mockPushChannel{
		core.EventTelegram: tg,
		core.EventSlack:    sl,
	})
	defer shutdown()

	srv.eventCh <- pushEvent(t, "alice", core.FanoutPayload{
		Text:        "슬랙으로 보내",
		ChannelHint: "slack",
	})

	slCalls := waitForCalls(t, sl, 1, 2*time.Second)
	if len(slCalls) != 1 {
		t.Fatalf("slack expected 1 call, got %d", len(slCalls))
	}
	if slCalls[0].Response != "슬랙으로 보내" {
		t.Errorf("slack response = %q", slCalls[0].Response)
	}
	if tgCalls := tg.calls(); len(tgCalls) != 0 {
		t.Errorf("telegram must not receive push when hint=slack; got %d calls", len(tgCalls))
	}
}

// TestDispatchLoop_FamilyPush_NoChannel_Enqueues covers the hot-reload
// window: alice has a telegram channel in Config but the spawner has nothing
// running (simulating a reconcile-in-progress or post-restart race). The
// push must land in pending_responses so the retry loop picks it up — a
// drop would silently lose family messages.
func TestDispatchLoop_FamilyPush_NoChannel_Enqueues(t *testing.T) {
	// Pass an empty mocks map: Config declares a telegram channel but
	// spawner has none registered.
	srv, shutdown := buildFamilyPushServer(t, &core.Config{
		Channels:     []core.ChannelConfig{{ChannelType: core.ChannelTelegram}},
		AdminChatIDs: []string{"99999"},
	}, map[core.EventType]*mockPushChannel{})
	defer shutdown()

	srv.eventCh <- pushEvent(t, "alice", core.FanoutPayload{Text: "큐에 저장돼야 함"})

	// Poll pending_responses via Store until a row appears or timeout.
	deadline := time.Now().Add(2 * time.Second)
	var pending []interface{}
	for time.Now().Before(deadline) {
		rows, err := srv.store.DequeuePendingResponses(10)
		if err != nil {
			t.Fatalf("dequeue: %v", err)
		}
		if len(rows) > 0 {
			for _, r := range rows {
				pending = append(pending, r)
				if r.TenantID != "alice" {
					t.Errorf("queued row tenant = %q, want alice", r.TenantID)
				}
				if r.Response != "큐에 저장돼야 함" {
					t.Errorf("queued row response = %q", r.Response)
				}
				if r.ChatID != "99999" {
					t.Errorf("queued row chatID = %q, want 99999", r.ChatID)
				}
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pending) == 0 {
		t.Fatal("expected pending_responses row for undelivered family push; queue is empty")
	}
}
