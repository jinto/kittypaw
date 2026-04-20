package engine

import (
	"sync"
	"time"
)

// Clock abstracts time so the Debouncer can be tested deterministically.
// Production uses RealClock; tests inject a fake clock.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a handle to a scheduled callback.
type Timer interface {
	Stop() bool
}

// RealClock delegates to time.Now and time.AfterFunc.
type RealClock struct{}

// Now returns the current wall-clock time.
func (RealClock) Now() time.Time { return time.Now() }

// AfterFunc schedules f to run after d.
func (RealClock) AfterFunc(d time.Duration, f func()) Timer {
	return &realTimer{t: time.AfterFunc(d, f)}
}

type realTimer struct {
	t *time.Timer
}

func (r *realTimer) Stop() bool { return r.t.Stop() }

// DebounceOp is the type of filesystem event the debouncer flushes.
type DebounceOp int

const (
	// DebounceIndex triggers a single-file re-index (Create or Write).
	DebounceIndex DebounceOp = iota
	// DebounceRemove triggers a single-file index removal.
	DebounceRemove
)

// Debouncer coalesces rapid-fire events on the same path into a single flush.
//
// Per-path independent timers. The first event on a path records firstAt; a
// cap timer (firstAt + cap) guarantees flush even if writes never pause.
// Events within interval reset the interval timer but not the cap.
//
// Latest op wins: Create → Write → Remove within the debounce window flushes
// as a single Remove.
type Debouncer struct {
	clock    Clock
	interval time.Duration
	capDur   time.Duration
	flush    func(path string, op DebounceOp)

	mu       sync.Mutex
	entries  map[string]*debounceEntry
	closed   bool
	inFlight sync.WaitGroup // flush callbacks currently executing
}

type debounceEntry struct {
	timer   Timer
	op      DebounceOp
	firstAt time.Time
	gen     int64 // bumps on each Schedule; fire drops if mismatched
}

// NewDebouncer creates a Debouncer. flush is called without holding the
// internal lock, so it may re-call Schedule freely. Pass nil clock for
// RealClock.
func NewDebouncer(clock Clock, interval, cap time.Duration, flush func(path string, op DebounceOp)) *Debouncer {
	if clock == nil {
		clock = RealClock{}
	}
	return &Debouncer{
		clock:    clock,
		interval: interval,
		capDur:   cap,
		flush:    flush,
		entries:  make(map[string]*debounceEntry),
	}
}

// Schedule records an event for path and resets the flush timer.
func (d *Debouncer) Schedule(path string, op DebounceOp) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return
	}

	now := d.clock.Now()
	e, ok := d.entries[path]
	if !ok {
		e = &debounceEntry{firstAt: now}
		d.entries[path] = e
	} else if e.timer != nil {
		e.timer.Stop()
	}
	e.op = op
	e.gen++
	gen := e.gen

	elapsed := now.Sub(e.firstAt)
	remaining := d.capDur - elapsed
	delay := d.interval
	if remaining < delay {
		delay = remaining
	}
	if delay < 0 {
		delay = 0
	}

	e.timer = d.clock.AfterFunc(delay, func() {
		d.fire(path, gen)
	})
}

// fire flushes path if its generation still matches.
func (d *Debouncer) fire(path string, gen int64) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	e, ok := d.entries[path]
	if !ok || e.gen != gen {
		d.mu.Unlock()
		return
	}
	op := e.op
	delete(d.entries, path)
	d.inFlight.Add(1)
	d.mu.Unlock()
	defer d.inFlight.Done()
	d.flush(path, op)
}

// Close stops pending timers, waits for in-flight flush callbacks to return,
// and drops pending events. Callers rely on Close returning only after the
// flush callback is quiet so downstream resources (e.g. DB store) can be torn
// down without racing an IndexFile still holding a transaction.
func (d *Debouncer) Close() {
	d.mu.Lock()
	d.closed = true
	for _, e := range d.entries {
		if e.timer != nil {
			e.timer.Stop()
		}
	}
	d.entries = nil
	d.mu.Unlock()
	d.inFlight.Wait()
}
