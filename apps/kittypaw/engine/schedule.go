package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jinto/kittypaw/core"
	"github.com/robfig/cron/v3"
)

// Scheduler runs scheduled skills at their configured intervals.
type Scheduler struct {
	session    *Session
	budget     *SharedTokenBudget
	pkgManager *core.PackageManager
	stop       chan struct{}
	stopOnce   sync.Once
	inflight   sync.Map      // skill name → struct{}: prevents concurrent runs of the same skill
	wg         sync.WaitGroup // tracks in-flight runSkill goroutines for graceful drain
}

// NewScheduler creates a scheduler that uses the given session for execution.
// The budget is shared across auto-fix, delegation, and reflection.
// pkgManager may be nil if packages are not configured.
func NewScheduler(session *Session, budget *SharedTokenBudget, pkgManager *core.PackageManager) *Scheduler {
	return &Scheduler{
		session:    session,
		budget:     budget,
		pkgManager: pkgManager,
		stop:       make(chan struct{}),
	}
}

// Start begins the scheduling loop, checking every minute for due skills.
// Also starts a separate goroutine for the daily reflection cycle.
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Start reflection cron in background.
	go s.runReflectionLoop(ctx)

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

// runReflectionLoop checks once per hour whether the daily reflection cycle
// should run. Default schedule: 03:00 daily.
func (s *Scheduler) runReflectionLoop(ctx context.Context) {
	if !s.session.Config.Reflection.Enabled {
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			if !s.isReflectionDue() {
				continue
			}
			slog.Info("scheduler: running reflection cycle")
			if err := RunReflectionCycle(ctx, s.session, &s.session.Config.Reflection); err != nil {
				slog.Error("scheduler: reflection cycle failed", "error", err)
			}

			// After reflection, check evolution trigger conditions.
			if s.session.Config.Evolution.Enabled {
				profiles, err := s.session.Store.ListActiveProfiles()
				if err == nil {
					for _, p := range profiles {
						_ = TriggerEvolution(ctx, p.ID, s.session, &s.session.Config.Evolution)
					}
				}
			}

			// Record last run.
			_ = s.session.Store.SetLastRun("__reflection__", time.Now())
		}
	}
}

// isReflectionDue returns true if the reflection cycle should run now.
// Checks: has it been at least 23 hours since last run, and is the current
// hour within the configured window (default: 3am).
func (s *Scheduler) isReflectionDue() bool {
	lastRun, _ := s.session.Store.GetLastRun("__reflection__")
	if lastRun != nil && time.Since(*lastRun) < 23*time.Hour {
		return false
	}

	// Default: run at 3am.
	targetHour := 3
	// TODO: parse config.Reflection.Cron for target hour
	return time.Now().Hour() == targetHour
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
	skills, err := core.LoadAllSkillsFrom(s.session.BaseDir)
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

	// Check installed packages with cron schedules.
	s.checkPackages(ctx)
}

// checkPackages iterates installed packages and runs those with due cron schedules.
func (s *Scheduler) checkPackages(ctx context.Context) {
	if s.pkgManager == nil {
		return
	}

	packages, err := s.pkgManager.ListInstalled()
	if err != nil {
		slog.Error("scheduler: list packages failed", "error", err)
		return
	}

	for _, pkg := range packages {
		if pkg.Meta.Cron == "" {
			continue
		}

		schedName := "pkg:" + pkg.Meta.ID
		lastRun, _ := s.session.Store.GetLastRun(schedName)
		if !cronIsDue(pkg.Meta.Cron, lastRun) {
			continue
		}

		if _, loaded := s.inflight.LoadOrStore(schedName, struct{}{}); loaded {
			continue
		}

		slog.Info("scheduler: running package", "id", pkg.Meta.ID)
		s.wg.Add(1)
		go func(pkg core.SkillPackage) {
			defer s.wg.Done()
			defer s.inflight.Delete("pkg:" + pkg.Meta.ID)
			s.runPackage(ctx, &pkg)
		}(pkg)
	}
}

// runPackage executes a package's main.js, then runs any chain steps sequentially.
func (s *Scheduler) runPackage(ctx context.Context, pkg *core.SkillPackage) {
	schedName := "pkg:" + pkg.Meta.ID
	if err := s.session.Store.SetLastRun(schedName, time.Now()); err != nil {
		slog.Error("scheduler: SetLastRun failed for package", "id", pkg.Meta.ID, "error", err)
		return
	}

	// Load the main package code.
	_, code, err := s.pkgManager.LoadPackage(pkg.Meta.ID)
	if err != nil {
		slog.Error("scheduler: load package failed", "id", pkg.Meta.ID, "error", err)
		return
	}

	// Execute main.js (package-level model override applies).
	output, err := s.executePackageCode(ctx, pkg, code, "", pkg.Meta.Model)
	if err != nil {
		slog.Error("scheduler: package execution failed", "id", pkg.Meta.ID, "error", err)
		_ = s.session.Store.IncrementFailureCount(schedName)
		return
	}

	_ = s.session.Store.ResetFailureCount(schedName)

	// Execute chain steps if defined.
	if len(pkg.Chain) > 0 {
		if chainErr := s.executeChainSteps(ctx, pkg, output); chainErr != nil {
			slog.Error("scheduler: chain execution failed", "id", pkg.Meta.ID, "error", chainErr)
		}
	}
}

// executeChainSteps runs chain steps sequentially, passing each step's output
// as prev_output to the next.
func (s *Scheduler) executeChainSteps(ctx context.Context, pkg *core.SkillPackage, initialOutput string) error {
	chain, err := s.pkgManager.LoadChain(pkg)
	if err != nil {
		return fmt.Errorf("load chain: %w", err)
	}

	prevOutput := initialOutput
	for _, step := range chain {
		// Model priority: chain step > chain package meta > session default.
		model := step.Model
		if model == "" {
			model = step.Package.Meta.Model
		}
		output, err := s.executePackageCode(ctx, &step.Package, step.Code, prevOutput, model)
		if err != nil {
			return fmt.Errorf("chain step %q failed: %w", step.Package.Meta.ID, err)
		}
		prevOutput = output
	}
	return nil
}

// executePackageCode runs a package's JavaScript code through the engine.
// prevOutput is injected as context for chain step execution.
// model overrides the session's default LLM model when non-empty.
func (s *Scheduler) executePackageCode(ctx context.Context, pkg *core.SkillPackage, code, prevOutput, model string) (string, error) {
	text := "skill:pkg:" + pkg.Meta.ID
	if prevOutput != "" {
		text += "\nprev_output:" + prevOutput
	}

	payload, _ := json.Marshal(core.ChatPayload{
		Text:   text,
		ChatID: "scheduler",
	})
	event := core.Event{
		Type:    core.EventDesktop,
		Payload: payload,
	}

	var opts *RunOptions
	if model != "" {
		opts = &RunOptions{ModelOverride: model}
	}
	return s.session.Run(ctx, event, opts)
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

	// Schedule type: use parsed cron schedule for accurate due check.
	return cronIsDue(skill.Trigger.Cron, lastRun)
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

		// Auto-delete one-shot skills even on failure.
		if sk.Skill.Trigger.Type == "once" {
			if delErr := core.DeleteSkillFrom(s.session.BaseDir, sk.Skill.Name); delErr != nil {
				slog.Error("scheduler: failed to delete one-shot skill after failure", "name", sk.Skill.Name, "error", delErr)
			}
			return
		}

		// Auto-fix trigger: 2 consecutive failures, not readonly, not a package skill.
		s.tryAutoFix(ctx, sk, err.Error())
		return
	}

	_ = s.session.Store.ResetFailureCount(sk.Skill.Name)
	_ = s.session.Store.ResetFixAttempts(sk.Skill.Name)

	// Delete one-shot skills after successful execution
	if sk.Skill.Trigger.Type == "once" {
		if delErr := core.DeleteSkillFrom(s.session.BaseDir, sk.Skill.Name); delErr != nil {
			slog.Error("scheduler: failed to delete one-shot skill after success", "name", sk.Skill.Name, "error", delErr)
		} else {
			slog.Info("scheduler: one-shot skill completed and deleted", "name", sk.Skill.Name)
		}
	}
}

// tryAutoFix checks whether a failed skill should trigger auto-fix and, if so,
// generates and applies a code fix.
func (s *Scheduler) tryAutoFix(ctx context.Context, sk *core.SkillWithCode, errMsg string) {
	if s.session.Config.AutonomyLevel == core.AutonomyReadonly {
		return
	}

	failCount, _ := s.session.Store.GetFailureCount(sk.Skill.Name)
	if failCount < 2 {
		return
	}

	// Atomic claim: only one goroutine proceeds with the fix.
	claimed, claimErr := s.session.Store.ClaimFixAttempt(sk.Skill.Name, maxFixAttempts)
	if claimErr != nil {
		slog.Error("auto-fix: claim failed", "skill", sk.Skill.Name, "error", claimErr)
		return
	}
	if !claimed {
		// Either another goroutine claimed it, or max attempts reached.
		fixAttempts, _ := s.session.Store.GetFixAttempts(sk.Skill.Name)
		if fixAttempts >= maxFixAttempts {
			slog.Warn("auto-fix: max attempts reached, disabling skill", "skill", sk.Skill.Name)
			if disableErr := core.DisableSkillFrom(s.session.BaseDir, sk.Skill.Name); disableErr != nil {
				slog.Error("auto-fix: disable failed", "skill", sk.Skill.Name, "error", disableErr)
			}
			_ = s.session.Store.RecordAudit("auto_fix_exhausted",
				fmt.Sprintf("skill %q disabled after %d fix attempts", sk.Skill.Name, maxFixAttempts), "warning")
		}
		return
	}

	slog.Info("auto-fix: attempting fix", "skill", sk.Skill.Name, "error", errMsg)

	result, genErr := AttemptAutoFix(ctx, sk.Skill.Name, errMsg, s.session, s.budget)
	if genErr != nil {
		slog.Error("auto-fix: generation failed", "skill", sk.Skill.Name, "error", genErr)
		// LLM failure: we already incremented fix_attempts via ClaimFixAttempt.
		// Check if this was the last attempt and disable if so.
		fixAttempts, _ := s.session.Store.GetFixAttempts(sk.Skill.Name)
		if fixAttempts >= maxFixAttempts {
			_ = core.DisableSkillFrom(s.session.BaseDir, sk.Skill.Name)
			_ = s.session.Store.RecordAudit("auto_fix_exhausted",
				fmt.Sprintf("skill %q disabled after %d fix attempts (generation failed: %v)", sk.Skill.Name, maxFixAttempts, genErr), "warning")
		}
		return
	}

	if applyErr := ApplyAutoFix(s.session, sk.Skill.Name, result, errMsg, sk.Code); applyErr != nil {
		slog.Error("auto-fix: apply failed", "skill", sk.Skill.Name, "error", applyErr)
	}
}

// parseCronInterval converts cron expressions to durations.
// Supports: "every 10m", "every 2h", "every 1d", and standard 5-field cron
// expressions (parsed via robfig/cron/v3 to compute next-fire interval).
func parseCronInterval(expr string) time.Duration {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return 0
	}

	// Simple "every Xm/h/d" format — kept for backward compatibility.
	if strings.HasPrefix(expr, "every ") {
		spec := strings.TrimPrefix(expr, "every ")
		d, err := time.ParseDuration(spec)
		if err == nil {
			return d
		}
		if strings.HasSuffix(spec, "d") {
			spec = strings.TrimSuffix(spec, "d")
			d, err = time.ParseDuration(spec + "h")
			if err == nil {
				return d * 24
			}
		}
	}

	// Standard 5-field cron: use robfig/cron/v3 to compute interval
	// between next two fires.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expr)
	if err != nil {
		return 0
	}
	now := time.Now()
	next1 := schedule.Next(now)
	next2 := schedule.Next(next1)
	return next2.Sub(next1)
}

// cronIsDue returns true if the cron expression is due for execution.
// For simple "every" expressions it uses duration comparison.
// For standard 5-field cron it uses schedule.Next(lastRun) directly, which
// correctly handles non-uniform schedules (monthly, weekday-only, DST).
func cronIsDue(expr string, lastRun *time.Time) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}

	if lastRun == nil {
		return true
	}

	// Simple "every" expressions — uniform interval, duration comparison is correct.
	if strings.HasPrefix(expr, "every ") {
		interval := parseCronInterval(expr)
		return interval > 0 && time.Since(*lastRun) >= interval
	}

	// Standard cron: compute the next fire time after lastRun and check if it's past.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(expr)
	if err != nil {
		return false
	}
	nextFire := schedule.Next(*lastRun)
	return time.Now().After(nextFire)
}
