package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jinto/gopaw/core"
)

// Scheduler runs scheduled skills at their configured intervals.
type Scheduler struct {
	session  *Session
	stop     chan struct{}
	stopOnce sync.Once
	inflight sync.Map      // skill name → struct{}: prevents concurrent runs of the same skill
	wg       sync.WaitGroup // tracks in-flight runSkill goroutines for graceful drain
}

// NewScheduler creates a scheduler that uses the given session for execution.
func NewScheduler(session *Session) *Scheduler {
	return &Scheduler{
		session: session,
		stop:    make(chan struct{}),
	}
}

// Start begins the scheduling loop, checking every minute for due skills.
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	slog.Info("scheduler started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			s.checkAndRun(ctx)
		}
	}
}

// Stop signals the scheduler to exit. Safe to call multiple times.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// Wait blocks until all in-flight skill executions complete.
func (s *Scheduler) Wait() {
	s.wg.Wait()
}

func (s *Scheduler) checkAndRun(ctx context.Context) {
	skills, err := core.LoadAllSkills()
	if err != nil {
		slog.Error("scheduler: load skills failed", "error", err)
		return
	}

	for _, sk := range skills {
		if !sk.Skill.Enabled {
			continue
		}
		if sk.Skill.Trigger.Type != "schedule" && sk.Skill.Trigger.Type != "once" {
			continue
		}

		if s.isDue(&sk.Skill) {
			if _, loaded := s.inflight.LoadOrStore(sk.Skill.Name, struct{}{}); loaded {
				slog.Debug("scheduler: skill still running, skipping", "name", sk.Skill.Name)
				continue
			}
			slog.Info("scheduler: running skill", "name", sk.Skill.Name, "trigger", sk.Skill.Trigger.Type)
			s.wg.Add(1)
			go func(sk core.SkillWithCode) {
				defer s.wg.Done()
				defer s.inflight.Delete(sk.Skill.Name)
				s.runSkill(ctx, &sk)
			}(sk)
		}
	}
}

func (s *Scheduler) isDue(skill *core.Skill) bool {
	lastRun, err := s.session.Store.GetLastRun(skill.Name)
	if err != nil {
		return false
	}

	// Check failure backoff
	failCount, _ := s.session.Store.GetFailureCount(skill.Name)
	if failCount > 0 {
		backoff := time.Duration(1<<min(failCount, 6)) * time.Minute
		if lastRun != nil && time.Since(*lastRun) < backoff {
			return false
		}
	}

	if skill.Trigger.Type == "once" {
		// One-shot: run if never run before and RunAt has passed
		if lastRun != nil {
			return false // Already ran
		}
		if skill.Trigger.RunAt != "" {
			runAt, err := time.Parse(time.RFC3339, skill.Trigger.RunAt)
			if err != nil {
				return false
			}
			return time.Now().After(runAt)
		}
		return true
	}

	// Schedule type: check cron expression
	interval := parseCronInterval(skill.Trigger.Cron)
	if interval == 0 {
		return false
	}

	if lastRun == nil {
		return true
	}
	return time.Since(*lastRun) >= interval
}

func (s *Scheduler) runSkill(ctx context.Context, sk *core.SkillWithCode) {
	if err := s.session.Store.SetLastRun(sk.Skill.Name, time.Now()); err != nil {
		slog.Error("scheduler: SetLastRun failed, aborting to prevent duplicate execution",
			"name", sk.Skill.Name, "trigger", sk.Skill.Trigger.Type, "error", err)
		return
	}

	// Create a synthetic event
	payload, _ := json.Marshal(core.ChatPayload{
		Text:   "skill:" + sk.Skill.Name,
		ChatID: "scheduler",
	})
	event := core.Event{
		Type:    core.EventDesktop,
		Payload: payload,
	}

	_, err := s.session.Run(ctx, event, nil)
	if err != nil {
		slog.Error("scheduler: skill execution failed", "name", sk.Skill.Name, "error", err)
		_ = s.session.Store.IncrementFailureCount(sk.Skill.Name)

		// Auto-delete one-shot skills even on failure
		if sk.Skill.Trigger.Type == "once" {
			if delErr := core.DeleteSkill(sk.Skill.Name); delErr != nil {
				slog.Error("scheduler: failed to delete one-shot skill after failure", "name", sk.Skill.Name, "error", delErr)
			}
		}
		return
	}

	_ = s.session.Store.ResetFailureCount(sk.Skill.Name)

	// Delete one-shot skills after successful execution
	if sk.Skill.Trigger.Type == "once" {
		if delErr := core.DeleteSkill(sk.Skill.Name); delErr != nil {
			slog.Error("scheduler: failed to delete one-shot skill after success", "name", sk.Skill.Name, "error", delErr)
		} else {
			slog.Info("scheduler: one-shot skill completed and deleted", "name", sk.Skill.Name)
		}
	}
}

// parseCronInterval converts simple cron expressions to durations.
// Supports: "every 10m", "every 2h", "every 1d", "*/10 * * * *"
func parseCronInterval(cron string) time.Duration {
	cron = strings.TrimSpace(cron)
	if cron == "" {
		return 0
	}

	// Simple "every Xm/h/d" format
	if strings.HasPrefix(cron, "every ") {
		spec := strings.TrimPrefix(cron, "every ")
		d, err := time.ParseDuration(spec)
		if err == nil {
			return d
		}
		// Try adding "h" or "m" suffix
		if strings.HasSuffix(spec, "d") {
			spec = strings.TrimSuffix(spec, "d")
			d, err = time.ParseDuration(spec + "h")
			if err == nil {
				return d * 24
			}
		}
	}

	// Simple cron: */N * * * * → every N minutes
	parts := strings.Fields(cron)
	if len(parts) >= 5 && strings.HasPrefix(parts[0], "*/") {
		n := 0
		fmt.Sscanf(parts[0], "*/%d", &n)
		if n > 0 {
			return time.Duration(n) * time.Minute
		}
	}

	return 0
}
