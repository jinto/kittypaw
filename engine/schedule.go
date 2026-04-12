package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
)

// Scheduler runs scheduled skills at their configured intervals.
type Scheduler struct {
	session *Session
	stop    chan struct{}
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

// Stop signals the scheduler to exit.
func (s *Scheduler) Stop() {
	close(s.stop)
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
			slog.Info("scheduler: running skill", "name", sk.Skill.Name, "trigger", sk.Skill.Trigger.Type)
			s.runSkill(ctx, &sk)
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
	_ = s.session.Store.SetLastRun(sk.Skill.Name, time.Now())

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
			_ = core.DeleteSkill(sk.Skill.Name)
		}
		return
	}

	_ = s.session.Store.ResetFailureCount(sk.Skill.Name)

	// Delete one-shot skills after successful execution
	if sk.Skill.Trigger.Type == "once" {
		_ = core.DeleteSkill(sk.Skill.Name)
		slog.Info("scheduler: one-shot skill completed and deleted", "name", sk.Skill.Name)
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
