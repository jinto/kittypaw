package engine

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a deterministic Clock for testing. Only safe for sequential
// test code — AfterFunc callbacks run synchronously inside Advance.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clock   *fakeClock
	fireAt  time.Time
	fn      func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, fn func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, fireAt: c.now.Add(d), fn: fn}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves time forward and fires due timers in registration order.
// Fire callbacks may schedule new timers — those won't fire this call
// unless Advance is called again.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	kept := c.timers[:0]
	for _, t := range c.timers {
		if t.stopped {
			continue
		}
		if !t.fireAt.After(c.now) {
			due = append(due, t)
			t.stopped = true
		} else {
			kept = append(kept, t)
		}
	}
	c.timers = kept
	c.mu.Unlock()

	// Fire outside the lock — callback may call AfterFunc again.
	for _, t := range due {
		t.fn()
	}
}

func TestDebouncer_FlushesAfterInterval(t *testing.T) {
	clock := newFakeClock()
	var got []string
	d := NewDebouncer(clock, 500*time.Millisecond, 2*time.Second, func(path string, _ DebounceOp) {
		got = append(got, path)
	})
	defer d.Close()

	d.Schedule("/a", DebounceIndex)

	clock.Advance(499 * time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("fired early: %v", got)
	}

	clock.Advance(2 * time.Millisecond)
	if len(got) != 1 || got[0] != "/a" {
		t.Fatalf("expected [/a], got %v", got)
	}
}

func TestDebouncer_CoalescesWithinInterval(t *testing.T) {
	clock := newFakeClock()
	var calls int
	var lastOp DebounceOp
	d := NewDebouncer(clock, 500*time.Millisecond, 2*time.Second, func(_ string, op DebounceOp) {
		calls++
		lastOp = op
	})
	defer d.Close()

	d.Schedule("/a", DebounceIndex)
	clock.Advance(200 * time.Millisecond)
	d.Schedule("/a", DebounceIndex)
	clock.Advance(200 * time.Millisecond)
	d.Schedule("/a", DebounceRemove) // latest op wins
	clock.Advance(499 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("fired before debounce settled: %d", calls)
	}
	clock.Advance(2 * time.Millisecond)
	if calls != 1 {
		t.Fatalf("expected 1 flush, got %d", calls)
	}
	if lastOp != DebounceRemove {
		t.Errorf("expected DebounceRemove, got %v", lastOp)
	}
}

func TestDebouncer_CapForcesFlushUnderContinuousWrites(t *testing.T) {
	clock := newFakeClock()
	var calls int
	d := NewDebouncer(clock, 500*time.Millisecond, 2*time.Second, func(_ string, _ DebounceOp) {
		calls++
	})
	defer d.Close()

	d.Schedule("/a", DebounceIndex)
	// Continuous writes every 200ms for 2s total. Without cap, timer resets
	// would never flush. Cap at firstAt+2s forces at least one flush.
	for range 10 {
		clock.Advance(200 * time.Millisecond)
		d.Schedule("/a", DebounceIndex)
	}

	if calls < 1 {
		t.Fatalf("cap did not force flush: calls=%d", calls)
	}
}

func TestDebouncer_IndependentPaths(t *testing.T) {
	clock := newFakeClock()
	got := map[string]int{}
	d := NewDebouncer(clock, 500*time.Millisecond, 2*time.Second, func(path string, _ DebounceOp) {
		got[path]++
	})
	defer d.Close()

	d.Schedule("/a", DebounceIndex)
	clock.Advance(300 * time.Millisecond)
	d.Schedule("/b", DebounceIndex)
	clock.Advance(300 * time.Millisecond) // t=600: /a flushed, /b pending
	if got["/a"] != 1 {
		t.Errorf("/a flush count: got %d, want 1", got["/a"])
	}
	if got["/b"] != 0 {
		t.Errorf("/b flushed early: %d", got["/b"])
	}
	clock.Advance(300 * time.Millisecond) // t=900: /b now flushed
	if got["/b"] != 1 {
		t.Errorf("/b flush count: got %d, want 1", got["/b"])
	}
}

func TestDebouncer_CloseStopsPending(t *testing.T) {
	clock := newFakeClock()
	var calls int
	d := NewDebouncer(clock, 500*time.Millisecond, 2*time.Second, func(_ string, _ DebounceOp) {
		calls++
	})

	d.Schedule("/a", DebounceIndex)
	d.Close()
	clock.Advance(600 * time.Millisecond)

	if calls != 0 {
		t.Errorf("fired after Close: %d", calls)
	}
}

func TestDebouncer_ScheduleAfterCloseNoop(t *testing.T) {
	clock := newFakeClock()
	var calls int
	d := NewDebouncer(clock, 500*time.Millisecond, 2*time.Second, func(_ string, _ DebounceOp) {
		calls++
	})
	d.Close()
	d.Schedule("/a", DebounceIndex)
	clock.Advance(600 * time.Millisecond)

	if calls != 0 {
		t.Errorf("Schedule-after-Close fired: %d", calls)
	}
}

func TestDebouncer_CoalescedRemoveThenIndex(t *testing.T) {
	clock := newFakeClock()
	var lastOp DebounceOp
	var calls int
	d := NewDebouncer(clock, 500*time.Millisecond, 2*time.Second, func(_ string, op DebounceOp) {
		calls++
		lastOp = op
	})
	defer d.Close()

	// Filesystem: Remove then Create (e.g. editor atomic rename).
	d.Schedule("/a", DebounceRemove)
	clock.Advance(50 * time.Millisecond)
	d.Schedule("/a", DebounceIndex)
	clock.Advance(501 * time.Millisecond)

	if calls != 1 {
		t.Fatalf("expected 1 flush, got %d", calls)
	}
	if lastOp != DebounceIndex {
		t.Errorf("expected DebounceIndex (latest), got %v", lastOp)
	}
}
