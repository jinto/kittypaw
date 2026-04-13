package engine

import (
	"testing"
	"time"

	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/store"
)

// ---------------------------------------------------------------------------
// parseCronInterval
// ---------------------------------------------------------------------------

func TestParseCronInterval(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"every 10m", 10 * time.Minute},
		{"every 2h", 2 * time.Hour},
		{"every 1d", 24 * time.Hour},
		{"every 30s", 30 * time.Second},
		{"*/5 * * * *", 5 * time.Minute},
		{"*/30 * * * *", 30 * time.Minute},
		{"", 0},
		{"  ", 0},
		{"invalid", 0},
		{"0 9 * * *", 24 * time.Hour}, // Daily at 9am → 24h interval
	}
	for _, tt := range tests {
		got := parseCronInterval(tt.input)
		if got != tt.want {
			t.Errorf("parseCronInterval(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Stop() safety
// ---------------------------------------------------------------------------

func TestSchedulerStopMultipleCalls(t *testing.T) {
	sched := NewScheduler(&Session{}, nil, nil)
	sched.Stop()
	sched.Stop() // must not panic
	sched.Stop()
}

// ---------------------------------------------------------------------------
// isDue — requires a real store for GetLastRun/GetFailureCount
// ---------------------------------------------------------------------------

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newTestScheduler(t *testing.T) (*Scheduler, *store.Store) {
	t.Helper()
	st := newTestStore(t)
	session := &Session{Store: st, Config: &core.Config{}}
	return NewScheduler(session, nil, nil), st
}

func TestIsDue_ScheduleFirstRun(t *testing.T) {
	sched, _ := newTestScheduler(t)
	skill := &core.Skill{
		Name:    "test-sched",
		Trigger: core.SkillTrigger{Type: "schedule", Cron: "every 5m"},
	}
	if !sched.isDue(skill) {
		t.Error("first run should be due")
	}
}

func TestIsDue_ScheduleNotYetDue(t *testing.T) {
	sched, st := newTestScheduler(t)
	_ = st.SetLastRun("test-sched", time.Now().Add(-2*time.Minute))

	skill := &core.Skill{
		Name:    "test-sched",
		Trigger: core.SkillTrigger{Type: "schedule", Cron: "every 5m"},
	}
	if sched.isDue(skill) {
		t.Error("should not be due: only 2m elapsed of 5m interval")
	}
}

func TestIsDue_ScheduleElapsed(t *testing.T) {
	sched, st := newTestScheduler(t)
	_ = st.SetLastRun("test-sched", time.Now().Add(-6*time.Minute))

	skill := &core.Skill{
		Name:    "test-sched",
		Trigger: core.SkillTrigger{Type: "schedule", Cron: "every 5m"},
	}
	if !sched.isDue(skill) {
		t.Error("should be due: 6m elapsed > 5m interval")
	}
}

func TestIsDue_OnceNeverRun(t *testing.T) {
	sched, _ := newTestScheduler(t)
	skill := &core.Skill{
		Name:    "test-once",
		Trigger: core.SkillTrigger{Type: "once"},
	}
	if !sched.isDue(skill) {
		t.Error("once trigger with no prior run should be due")
	}
}

func TestIsDue_OnceAlreadyRan(t *testing.T) {
	sched, st := newTestScheduler(t)
	_ = st.SetLastRun("test-once", time.Now())

	skill := &core.Skill{
		Name:    "test-once",
		Trigger: core.SkillTrigger{Type: "once"},
	}
	if sched.isDue(skill) {
		t.Error("once trigger should not be due after it has run")
	}
}

func TestIsDue_OnceRunAtFuture(t *testing.T) {
	sched, _ := newTestScheduler(t)
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	skill := &core.Skill{
		Name:    "test-once-future",
		Trigger: core.SkillTrigger{Type: "once", RunAt: future},
	}
	if sched.isDue(skill) {
		t.Error("once trigger with RunAt in the future should not be due")
	}
}

func TestIsDue_OnceRunAtPast(t *testing.T) {
	sched, _ := newTestScheduler(t)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	skill := &core.Skill{
		Name:    "test-once-past",
		Trigger: core.SkillTrigger{Type: "once", RunAt: past},
	}
	if !sched.isDue(skill) {
		t.Error("once trigger with RunAt in the past should be due")
	}
}

func TestIsDue_FailureBackoff(t *testing.T) {
	sched, st := newTestScheduler(t)

	// 3 consecutive failures → backoff = 2^3 = 8 minutes
	_ = st.SetLastRun("test-backoff", time.Now().Add(-5*time.Minute))
	_ = st.IncrementFailureCount("test-backoff")
	_ = st.IncrementFailureCount("test-backoff")
	_ = st.IncrementFailureCount("test-backoff")

	skill := &core.Skill{
		Name:    "test-backoff",
		Trigger: core.SkillTrigger{Type: "schedule", Cron: "every 1m"},
	}
	if sched.isDue(skill) {
		t.Error("should not be due: 5m elapsed < 8m backoff (2^3)")
	}

	// After enough time passes, it should be due again.
	_ = st.SetLastRun("test-backoff", time.Now().Add(-10*time.Minute))
	if !sched.isDue(skill) {
		t.Error("should be due: 10m elapsed > 8m backoff")
	}
}

// ---------------------------------------------------------------------------
// In-flight guard
// ---------------------------------------------------------------------------

func TestInflightGuard(t *testing.T) {
	sched, _ := newTestScheduler(t)

	// Simulate a skill already in flight.
	sched.inflight.Store("running-skill", struct{}{})

	_, loaded := sched.inflight.LoadOrStore("running-skill", struct{}{})
	if !loaded {
		t.Error("expected inflight guard to detect already-running skill")
	}

	// After clearing, it should allow again.
	sched.inflight.Delete("running-skill")
	_, loaded = sched.inflight.LoadOrStore("running-skill", struct{}{})
	if loaded {
		t.Error("expected inflight guard to allow skill after clearing")
	}
}
