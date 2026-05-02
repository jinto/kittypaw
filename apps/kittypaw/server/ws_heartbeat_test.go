package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakePinger satisfies the pinger interface for unit tests. The
// signal channel pulses on every Ping call so tests can wait for a
// specific number of pings deterministically without leaning on
// wall-clock sleeps. err overrides the ping result on every call.
type fakePinger struct {
	calls  atomic.Int32
	signal chan struct{}
	err    error
}

func newFakePinger(buf int) *fakePinger {
	return &fakePinger{signal: make(chan struct{}, buf)}
}

func (f *fakePinger) Ping(ctx context.Context) error {
	f.calls.Add(1)
	select {
	case f.signal <- struct{}{}:
	default:
	}
	return f.err
}

func TestRunHeartbeat_FiresAtIntervals(t *testing.T) {
	// Wait for actual ping signals rather than time.Sleep — keeps
	// the test deterministic on slow CI.
	p := newFakePinger(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runHeartbeat(ctx, p, 5*time.Millisecond, 50*time.Millisecond, nil)

	for i := 0; i < 3; i++ {
		select {
		case <-p.signal:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("expected ping #%d, got %d total", i+1, p.calls.Load())
		}
	}
}

func TestRunHeartbeat_ExitsOnCtxCancel(t *testing.T) {
	p := newFakePinger(2)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		runHeartbeat(ctx, p, 100*time.Millisecond, 50*time.Millisecond, nil)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("heartbeat did not exit promptly on ctx cancel")
	}
}

func TestRunHeartbeat_ExitsOnPingError(t *testing.T) {
	// A ping failure means the conn is dead; the heartbeat should
	// exit immediately so the surrounding handler can release
	// resources without the goroutine continuing to fire.
	p := newFakePinger(2)
	p.err = errors.New("conn dead")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		runHeartbeat(ctx, p, 5*time.Millisecond, 2*time.Millisecond, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("heartbeat did not exit on ping error")
	}

	// Exactly one ping call should have been observed before exit.
	if got := p.calls.Load(); got != 1 {
		t.Errorf("expected single ping before error exit, got %d", got)
	}
}

func TestRunHeartbeat_OnFailCalledOnPingError(t *testing.T) {
	// A failing ping must invoke onFail so the handler ctx is
	// canceled, terminating the readPump and main loop too. Without
	// this, a dead client would keep model + tool cost burning
	// inside an in-flight RunTurn until wsMaxLifetime (30 min).
	p := newFakePinger(2)
	p.err = errors.New("conn dead")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	failed := make(chan struct{}, 1)
	onFail := func() { failed <- struct{}{} }

	done := make(chan struct{})
	go func() {
		runHeartbeat(ctx, p, 5*time.Millisecond, 2*time.Millisecond, onFail)
		close(done)
	}()

	select {
	case <-failed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("onFail was not invoked on ping error")
	}

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("heartbeat did not exit after onFail")
	}
}

func TestRunHeartbeat_NilOnFailIsSafe(t *testing.T) {
	// When the caller doesn't care to be notified (e.g. unit test
	// scenarios), nil onFail must be tolerated without panic.
	p := newFakePinger(2)
	p.err = errors.New("conn dead")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("nil onFail should not panic, got %v", r)
			}
			close(done)
		}()
		runHeartbeat(ctx, p, 5*time.Millisecond, 2*time.Millisecond, nil)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("heartbeat did not exit")
	}
}
