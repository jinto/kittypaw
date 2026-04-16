package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
)

// stubChannel is a minimal Channel implementation for testing.
// It blocks in Start until ctx is canceled.
type stubChannel struct {
	name    string
	started chan struct{} // closed when Start begins
	mu      sync.Mutex
	sends   []string // records SendResponse calls
}

func newStub(name string) *stubChannel {
	return &stubChannel{name: name, started: make(chan struct{})}
}

func (s *stubChannel) Start(ctx context.Context, eventCh chan<- core.Event) error {
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

func (s *stubChannel) SendResponse(_ context.Context, chatID, response string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends = append(s.sends, chatID+":"+response)
	return nil
}

func (s *stubChannel) Name() string { return s.name }

// waitStarted blocks until the stub's Start method has been entered.
func (s *stubChannel) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-s.started:
	case <-time.After(2 * time.Second):
		t.Fatal("stub channel did not start in time")
	}
}

// --- Tests ---

func TestTrySpawn_StartsChannel(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stub := newStub("telegram")
	cfg := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "tok"}

	if err := sp.TrySpawn(stub, cfg); err != nil {
		t.Fatalf("TrySpawn: %v", err)
	}
	stub.waitStarted(t)

	// Verify it appears in List and GetChannel.
	ch, ok := sp.GetChannel(core.EventTelegram)
	if !ok || ch == nil {
		t.Fatal("GetChannel returned false after TrySpawn")
	}

	statuses := sp.List()
	if len(statuses) != 1 {
		t.Fatalf("List: got %d, want 1", len(statuses))
	}
	if statuses[0].Name != "telegram" || statuses[0].Type != "telegram" || !statuses[0].Running {
		t.Errorf("List: unexpected status %+v", statuses[0])
	}

	// Cleanup.
	sp.Stop("telegram")
}

func TestTrySpawn_Idempotent(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stub1 := newStub("telegram")
	stub2 := newStub("telegram")
	cfg := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "tok"}

	sp.TrySpawn(stub1, cfg)
	stub1.waitStarted(t)

	// Second TrySpawn with same name should be a no-op.
	if err := sp.TrySpawn(stub2, cfg); err != nil {
		t.Fatalf("second TrySpawn: %v", err)
	}

	// Original stub should still be the one returned.
	ch, _ := sp.GetChannel(core.EventTelegram)
	if ch != stub1 {
		t.Error("TrySpawn replaced existing channel — should be idempotent")
	}

	sp.Stop("telegram")
}

func TestStop_CloseDone(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stub := newStub("slack")
	cfg := core.ChannelConfig{ChannelType: core.ChannelSlack, Token: "tok"}
	sp.TrySpawn(stub, cfg)
	stub.waitStarted(t)

	if err := sp.Stop("slack"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// After Stop, GetChannel should return false.
	_, ok := sp.GetChannel(core.EventSlack)
	if ok {
		t.Error("GetChannel returned true after Stop")
	}
}

func TestStop_NotFound(t *testing.T) {
	sp := NewChannelSpawner(context.Background(), make(chan core.Event, 1))
	if err := sp.Stop("nonexistent"); err != ErrChannelNotFound {
		t.Errorf("Stop nonexistent: got %v, want ErrChannelNotFound", err)
	}
}

func TestGetChannel_EmptySpawner(t *testing.T) {
	sp := NewChannelSpawner(context.Background(), make(chan core.Event, 1))
	ch, ok := sp.GetChannel(core.EventTelegram)
	if ok || ch != nil {
		t.Error("GetChannel on empty spawner should return nil, false")
	}
}

func TestList_Empty(t *testing.T) {
	sp := NewChannelSpawner(context.Background(), make(chan core.Event, 1))
	statuses := sp.List()
	if len(statuses) != 0 {
		t.Errorf("List on empty spawner: got %d, want 0", len(statuses))
	}
}

func TestList_MultipleChannels(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stubs := []*stubChannel{newStub("telegram"), newStub("slack")}
	cfgs := []core.ChannelConfig{
		{ChannelType: core.ChannelTelegram, Token: "t1"},
		{ChannelType: core.ChannelSlack, Token: "t2"},
	}

	for i, stub := range stubs {
		sp.TrySpawn(stub, cfgs[i])
		stub.waitStarted(t)
	}

	statuses := sp.List()
	if len(statuses) != 2 {
		t.Fatalf("List: got %d, want 2", len(statuses))
	}

	// Cleanup.
	sp.Stop("telegram")
	sp.Stop("slack")
}

// --- Reconcile / ReplaceSpawn / StopAll tests ---

func TestReplaceSpawn(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stub1 := newStub("telegram")
	cfg1 := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "old"}
	sp.TrySpawn(stub1, cfg1)
	stub1.waitStarted(t)

	// Replace with new stub.
	stub2 := newStub("telegram")
	cfg2 := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "new"}
	if err := sp.ReplaceSpawn(stub2, cfg2); err != nil {
		t.Fatalf("ReplaceSpawn: %v", err)
	}
	stub2.waitStarted(t)

	// Verify new channel is returned.
	ch, ok := sp.GetChannel(core.EventTelegram)
	if !ok || ch != stub2 {
		t.Error("GetChannel should return the replacement channel")
	}

	sp.Stop("telegram")
}

func TestReconcile_AddNewChannel(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	configs := []core.ChannelConfig{
		{ChannelType: core.ChannelTelegram, Token: "tok"},
	}
	if err := sp.Reconcile(configs); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Give goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	ch, ok := sp.GetChannel(core.EventTelegram)
	if !ok || ch == nil {
		t.Error("Reconcile should have spawned telegram channel")
	}

	sp.StopAll()
}

func TestReconcile_RemoveChannel(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stub := newStub("telegram")
	cfg := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "tok"}
	sp.TrySpawn(stub, cfg)
	stub.waitStarted(t)

	// Reconcile with empty config → should stop telegram.
	if err := sp.Reconcile(nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	_, ok := sp.GetChannel(core.EventTelegram)
	if ok {
		t.Error("telegram should be removed after Reconcile with empty config")
	}
}

func TestReconcile_ReplaceChanged(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stub := newStub("telegram")
	cfgOld := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "old-token"}
	sp.TrySpawn(stub, cfgOld)
	stub.waitStarted(t)

	// Reconcile with changed token → should replace.
	cfgNew := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "new-token"}
	if err := sp.Reconcile([]core.ChannelConfig{cfgNew}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	ch, ok := sp.GetChannel(core.EventTelegram)
	if !ok || ch == nil {
		t.Error("Reconcile should have spawned replacement channel")
	}
	// Original stub should no longer be the channel.
	if ch == stub {
		t.Error("Reconcile did not replace the channel despite config change")
	}

	sp.StopAll()
}

func TestReconcile_SkipUnchanged(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stub := newStub("telegram")
	cfg := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "tok"}
	sp.TrySpawn(stub, cfg)
	stub.waitStarted(t)

	// Reconcile with same config → should keep existing channel.
	if err := sp.Reconcile([]core.ChannelConfig{cfg}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	ch, _ := sp.GetChannel(core.EventTelegram)
	if ch != stub {
		t.Error("Reconcile replaced a channel whose config did not change")
	}

	sp.Stop("telegram")
}

func TestReconcile_SkipsWebSocket(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	configs := []core.ChannelConfig{
		{ChannelType: core.ChannelWeb, BindAddr: ":8080"},
	}
	sp.Reconcile(configs)

	_, ok := sp.GetChannel(core.EventWebChat)
	if ok {
		t.Error("Reconcile should skip WebSocket channels")
	}
}

func TestReconcile_BestEffort(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	// Telegram with valid token + Slack with empty token (will fail FromConfig).
	configs := []core.ChannelConfig{
		{ChannelType: core.ChannelTelegram, Token: "tok"},
		{ChannelType: core.ChannelSlack, Token: ""},
	}
	err := sp.Reconcile(configs)
	if err == nil {
		t.Fatal("Reconcile should return error for invalid slack config")
	}

	time.Sleep(50 * time.Millisecond)

	// Telegram should still have been spawned despite Slack failure.
	_, ok := sp.GetChannel(core.EventTelegram)
	if !ok {
		t.Error("Telegram should be running even though Slack failed")
	}

	sp.StopAll()
}

func TestStopAll_Parallel(t *testing.T) {
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stubs := []*stubChannel{newStub("telegram"), newStub("slack"), newStub("discord")}
	cfgs := []core.ChannelConfig{
		{ChannelType: core.ChannelTelegram, Token: "t1"},
		{ChannelType: core.ChannelSlack, Token: "t2"},
		{ChannelType: core.ChannelDiscord, Token: "t3"},
	}

	for i, stub := range stubs {
		sp.TrySpawn(stub, cfgs[i])
		stub.waitStarted(t)
	}

	start := time.Now()
	sp.StopAll()
	elapsed := time.Since(start)

	// All channels stopped.
	if len(sp.List()) != 0 {
		t.Error("StopAll should clear all channels")
	}

	// Parallel stop should complete quickly (all three in parallel, not 3x sequential).
	if elapsed > 2*time.Second {
		t.Errorf("StopAll took %v — expected parallel stop to be fast", elapsed)
	}
}

func TestConfigEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b core.ChannelConfig
		want bool
	}{
		{"identical", core.ChannelConfig{ChannelType: "t", Token: "x"}, core.ChannelConfig{ChannelType: "t", Token: "x"}, true},
		{"token differs", core.ChannelConfig{Token: "a"}, core.ChannelConfig{Token: "b"}, false},
		{"both kakao nil", core.ChannelConfig{}, core.ChannelConfig{}, true},
		{"one kakao nil", core.ChannelConfig{Kakao: &core.KakaoChannelConfig{RelayURL: "x"}}, core.ChannelConfig{}, false},
		{"kakao equal", core.ChannelConfig{Kakao: &core.KakaoChannelConfig{RelayURL: "x"}}, core.ChannelConfig{Kakao: &core.KakaoChannelConfig{RelayURL: "x"}}, true},
		{"kakao differ", core.ChannelConfig{Kakao: &core.KakaoChannelConfig{RelayURL: "x"}}, core.ChannelConfig{Kakao: &core.KakaoChannelConfig{RelayURL: "y"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("configEqual = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStop_ConcurrentGetChannel(t *testing.T) {
	// Verify that Stop does not deadlock with concurrent GetChannel calls.
	eventCh := make(chan core.Event, 8)
	sp := NewChannelSpawner(context.Background(), eventCh)

	stub := newStub("telegram")
	cfg := core.ChannelConfig{ChannelType: core.ChannelTelegram, Token: "tok"}
	sp.TrySpawn(stub, cfg)
	stub.waitStarted(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Rapidly call GetChannel while Stop is in progress.
		for i := 0; i < 100; i++ {
			sp.GetChannel(core.EventTelegram)
		}
	}()

	sp.Stop("telegram")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: GetChannel blocked during Stop")
	}
}
