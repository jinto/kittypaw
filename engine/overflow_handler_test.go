package engine

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestOverflowHandler_SingleSignal_Fires(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int64
	h := newOverflowHandler(clock, 500*time.Millisecond, 30*time.Second, func() {
		calls.Add(1)
	})
	defer h.Close()

	h.Signal()
	clock.Advance(499 * time.Millisecond)
	h.inFlight.Wait()
	if got := calls.Load(); got != 0 {
		t.Fatalf("fired before delay elapsed: calls=%d", got)
	}

	clock.Advance(2 * time.Millisecond)
	h.inFlight.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 run, got %d", got)
	}
}

func TestOverflowHandler_RapidSignals_Coalesce(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int64
	h := newOverflowHandler(clock, 500*time.Millisecond, 30*time.Second, func() {
		calls.Add(1)
	})
	defer h.Close()

	h.Signal()
	h.Signal()
	h.Signal()

	clock.Advance(501 * time.Millisecond)
	h.inFlight.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("3 signals coalesced expected 1 run, got %d", got)
	}
}

func TestOverflowHandler_WithinBackoff_Skipped(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int64
	h := newOverflowHandler(clock, 500*time.Millisecond, 30*time.Second, func() {
		calls.Add(1)
	})
	defer h.Close()

	// First run
	h.Signal()
	clock.Advance(501 * time.Millisecond)
	h.inFlight.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("first run failed: calls=%d", got)
	}

	// Within backoff — skipped
	h.Signal()
	clock.Advance(501 * time.Millisecond)
	h.inFlight.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("signal within backoff fired: calls=%d", got)
	}
}

func TestOverflowHandler_AfterBackoff_FiresAgain(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int64
	h := newOverflowHandler(clock, 500*time.Millisecond, 30*time.Second, func() {
		calls.Add(1)
	})
	defer h.Close()

	h.Signal()
	clock.Advance(501 * time.Millisecond)
	h.inFlight.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("first run failed: calls=%d", got)
	}

	// Advance past backoff (30s).
	clock.Advance(30 * time.Second)
	h.Signal()
	clock.Advance(501 * time.Millisecond)
	h.inFlight.Wait()
	if got := calls.Load(); got != 2 {
		t.Fatalf("post-backoff run failed: calls=%d", got)
	}
}

func TestOverflowHandler_Close_WaitsInFlight(t *testing.T) {
	clock := newFakeClock()
	started := make(chan struct{})
	release := make(chan struct{})
	h := newOverflowHandler(clock, 500*time.Millisecond, 30*time.Second, func() {
		close(started)
		<-release
	})

	h.Signal()
	clock.Advance(501 * time.Millisecond)
	// Wait for the run goroutine to actually start.
	<-started

	closeDone := make(chan struct{})
	go func() {
		h.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		t.Fatalf("Close returned before in-flight run completed")
	case <-time.After(50 * time.Millisecond):
		// expected: Close is blocked on inFlight.Wait
	}

	close(release)
	select {
	case <-closeDone:
		// good
	case <-time.After(2 * time.Second):
		t.Fatalf("Close did not return after release")
	}
}

func TestOverflowHandler_SignalAfterClose_NoOp(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int64
	h := newOverflowHandler(clock, 500*time.Millisecond, 30*time.Second, func() {
		calls.Add(1)
	})
	h.Close()

	h.Signal()
	clock.Advance(time.Second)
	if got := calls.Load(); got != 0 {
		t.Errorf("Signal after Close fired: calls=%d", got)
	}
}
