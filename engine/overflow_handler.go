package engine

import (
	"log/slog"
	"sync"
	"time"
)

// overflowHandler coalesces fsnotify overflow signals into at-most-one
// recovery invocation within a debounce window, with a minimum interval
// between successive invocations to prevent CPU/IO spirals when the kernel
// queue keeps overflowing. The run callback is invoked in a goroutine tracked
// by inFlight so Close can wait for it to finish before the caller tears down
// downstream resources (e.g. Indexer / Store).
type overflowHandler struct {
	clock       Clock
	delay       time.Duration
	minInterval time.Duration
	run         func()

	mu       sync.Mutex
	pending  bool
	timer    Timer
	lastRun  time.Time
	closed   bool
	inFlight sync.WaitGroup
}

// newOverflowHandler builds a handler. delay coalesces bursts; minInterval
// is the backoff between successive runs. Pass nil clock for RealClock.
func newOverflowHandler(clock Clock, delay, minInterval time.Duration, run func()) *overflowHandler {
	if clock == nil {
		clock = RealClock{}
	}
	return &overflowHandler{
		clock:       clock,
		delay:       delay,
		minInterval: minInterval,
		run:         run,
	}
}

// Signal records an overflow event. Multiple Signals within delay collapse
// into one run. Signals within minInterval of the last run are dropped.
func (h *overflowHandler) Signal() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.pending {
		return
	}
	if !h.lastRun.IsZero() && h.clock.Now().Sub(h.lastRun) < h.minInterval {
		slog.Debug("overflow handler: within backoff window, skipping signal")
		return
	}
	h.pending = true
	h.timer = h.clock.AfterFunc(h.delay, h.fire)
}

// fire is invoked by the timer when the debounce window elapses. Spawns run
// in a goroutine so Signal/Close are not blocked by a long-running callback.
func (h *overflowHandler) fire() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.pending = false
	h.lastRun = h.clock.Now()
	h.inFlight.Add(1)
	fn := h.run
	h.mu.Unlock()

	go func() {
		defer h.inFlight.Done()
		if fn != nil {
			fn()
		}
	}()
}

// Close stops any pending timer and waits for in-flight run callbacks. After
// Close, Signal becomes a no-op.
func (h *overflowHandler) Close() {
	h.mu.Lock()
	h.closed = true
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	h.pending = false
	h.mu.Unlock()
	h.inFlight.Wait()
}
