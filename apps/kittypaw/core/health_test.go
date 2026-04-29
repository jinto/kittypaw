package core

import (
	"sync"
	"testing"
	"time"
)

func TestAccountHealthString(t *testing.T) {
	cases := []struct {
		in   AccountHealth
		want string
	}{
		{AccountHealthReady, "Ready"},
		{AccountHealthDegraded, "Degraded"},
		{AccountHealthStopped, "Stopped"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("AccountHealth(%d).String() = %q, want %q", c.in, got, c.want)
		}
	}
	// Unknown value should not panic and should carry the numeric tag so
	// structured logs stay parseable.
	if got := AccountHealth(42).String(); got == "" {
		t.Errorf("AccountHealth(42).String() returned empty, want descriptive")
	}
}

func TestHealthState_InitialReady(t *testing.T) {
	s := NewHealthState()
	if got := s.Load(); got != AccountHealthReady {
		t.Errorf("new HealthState = %v, want Ready", got)
	}
	if !s.LastPanic().IsZero() {
		t.Errorf("new HealthState LastPanic should be zero, got %v", s.LastPanic())
	}
}

func TestHealthState_MarkDegradedRecordsTimestamp(t *testing.T) {
	s := NewHealthState()
	now := time.Now()
	s.MarkDegraded(now)
	if got := s.Load(); got != AccountHealthDegraded {
		t.Errorf("after MarkDegraded = %v, want Degraded", got)
	}
	if got := s.LastPanic(); !got.Equal(now) {
		t.Errorf("LastPanic = %v, want %v", got, now)
	}
}

func TestHealthState_MarkReadyAfterDegraded(t *testing.T) {
	s := NewHealthState()
	s.MarkDegraded(time.Now())
	s.MarkReady()
	if got := s.Load(); got != AccountHealthReady {
		t.Errorf("after MarkReady = %v, want Ready", got)
	}
	// LastPanic timestamp is kept even after recovery — it is a history
	// record, not a live flag.
	if s.LastPanic().IsZero() {
		t.Errorf("LastPanic cleared on MarkReady; should persist as audit trail")
	}
}

func TestHealthState_StoppedIsTerminal(t *testing.T) {
	s := NewHealthState()
	s.MarkStopped()
	// Attempts to leave Stopped must be ignored — a shutting-down daemon
	// should never appear to resume from a stale goroutine tick.
	s.MarkReady()
	if got := s.Load(); got != AccountHealthStopped {
		t.Errorf("MarkReady after Stopped changed state to %v, want Stopped", got)
	}
	s.MarkDegraded(time.Now())
	if got := s.Load(); got != AccountHealthStopped {
		t.Errorf("MarkDegraded after Stopped changed state to %v, want Stopped", got)
	}
}

func TestHealthState_ConcurrentMarks(t *testing.T) {
	s := NewHealthState()

	const workers = 32
	const iter = 200

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iter; j++ {
				if (id+j)%2 == 0 {
					s.MarkDegraded(time.Now())
				} else {
					s.MarkReady()
				}
			}
		}(i)
	}
	wg.Wait()

	// Final state is non-deterministic; what matters is that no race
	// triggers under -race and that Load returns a valid enum value.
	switch s.Load() {
	case AccountHealthReady, AccountHealthDegraded:
		// OK — either is valid after the race.
	default:
		t.Errorf("unexpected post-race state: %v", s.Load())
	}
}
