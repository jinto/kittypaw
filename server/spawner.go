package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jinto/kittypaw/channel"
	"github.com/jinto/kittypaw/core"
)

// stopTimeout is the maximum time to wait for a channel goroutine to exit
// after its context is canceled. This defends against buggy channel
// implementations that ignore context cancellation.
const stopTimeout = 10 * time.Second

// ErrChannelNotFound is returned when Stop is called for a channel that
// is not currently running.
var ErrChannelNotFound = errors.New("channel not found")

// runningChannel tracks a single active channel and the machinery needed
// to stop it cleanly.
type runningChannel struct {
	cancel func()             // cancels the context passed to Start
	ch     channel.Channel    // the live channel instance
	done   chan struct{}      // closed when the Start goroutine exits
	config core.ChannelConfig // config snapshot for Reconcile diff
}

// ChannelStatus is the API-facing representation of a running channel.
type ChannelStatus struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Running bool   `json:"running"`
}

// ChannelSpawner manages the lifecycle of messaging channels.
// It is safe for concurrent use.
type ChannelSpawner struct {
	mu          sync.RWMutex
	reconcileMu sync.Mutex // serializes Reconcile calls
	running     map[string]*runningChannel
	eventCh     chan<- core.Event
	baseCtx     context.Context // long-lived context for channel goroutines
}

// NewChannelSpawner creates a spawner that will pass eventCh to every
// channel it starts. baseCtx should be a long-lived context (e.g., from
// signal.NotifyContext) — all channel goroutines derive their contexts
// from it, regardless of the caller's context.
func NewChannelSpawner(baseCtx context.Context, eventCh chan<- core.Event) *ChannelSpawner {
	return &ChannelSpawner{
		running: make(map[string]*runningChannel),
		eventCh: eventCh,
		baseCtx: baseCtx,
	}
}

// TrySpawn starts a channel if one with the same name is not already running.
// It is idempotent: calling TrySpawn for an already-running channel returns nil.
// The channel goroutine's context is derived from the spawner's baseCtx, not
// the caller's context — so HTTP request contexts won't kill long-lived channels.
func (s *ChannelSpawner) TrySpawn(ch channel.Channel, cfg core.ChannelConfig) error {
	name := ch.Name()

	s.mu.Lock()
	if _, exists := s.running[name]; exists {
		s.mu.Unlock()
		return nil // idempotent
	}

	chCtx, cancel := context.WithCancel(s.baseCtx)
	done := make(chan struct{})
	rc := &runningChannel{
		cancel: cancel,
		ch:     ch,
		done:   done,
		config: cfg,
	}
	s.running[name] = rc
	s.mu.Unlock()

	slog.Info("channel spawned", "name", name)
	go func() {
		defer close(done)
		if err := ch.Start(chCtx, s.eventCh); err != nil && chCtx.Err() == nil {
			slog.Error("channel stopped unexpectedly", "name", name, "error", err)
		}
	}()

	return nil
}

// Stop cancels a running channel and waits for its goroutine to exit.
//
// Lock discipline: the write lock is released BEFORE blocking on <-done.
// This prevents deadlocking concurrent GetChannel/List callers.
func (s *ChannelSpawner) Stop(name string) error {
	s.mu.Lock()
	rc, ok := s.running[name]
	if !ok {
		s.mu.Unlock()
		return ErrChannelNotFound
	}
	delete(s.running, name)
	s.mu.Unlock()

	rc.cancel()
	select {
	case <-rc.done:
		slog.Info("channel stopped", "name", name)
	case <-time.After(stopTimeout):
		slog.Error("channel stop: timed out waiting for goroutine", "name", name)
	}
	return nil
}

// GetChannel returns the Channel for the given event type, or nil and false
// if no channel with that name is currently running.
func (s *ChannelSpawner) GetChannel(eventType core.EventType) (channel.Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rc, ok := s.running[string(eventType)]
	if !ok {
		return nil, false
	}
	return rc.ch, true
}

// ReplaceSpawn stops the existing channel with the same name (if any)
// and spawns a new one. Used when a channel's config has changed.
func (s *ChannelSpawner) ReplaceSpawn(ch channel.Channel, cfg core.ChannelConfig) error {
	name := ch.Name()
	// Stop is a no-op error if not found — ignore ErrChannelNotFound.
	if err := s.Stop(name); err != nil && !errors.Is(err, ErrChannelNotFound) {
		return fmt.Errorf("stop %s: %w", name, err)
	}
	return s.TrySpawn(ch, cfg)
}

// configEqual compares two ChannelConfig values.
func configEqual(a, b core.ChannelConfig) bool {
	return a.ChannelType == b.ChannelType &&
		a.Token == b.Token &&
		a.BindAddr == b.BindAddr &&
		a.KakaoWSURL == b.KakaoWSURL
}

// Reconcile compares the currently running channels against the desired
// configs and starts, stops, or replaces channels to match.
//
// Serialized via reconcileMu to prevent concurrent calls from corrupting
// state. Best-effort: individual failures are aggregated, not fatal.
// WebSocket channels are excluded (port-binding makes hot-reload unsafe).
func (s *ChannelSpawner) Reconcile(configs []core.ChannelConfig) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	// Inject Kakao relay WS URL from secrets into channel configs.
	core.InjectKakaoWSURL(configs)

	// Build desired map, filtering out WebSocket channels.
	desired := make(map[string]core.ChannelConfig)
	for _, cfg := range configs {
		if cfg.ChannelType == core.ChannelWeb {
			continue
		}
		name := string(cfg.ChannelType.ToEventType())
		desired[name] = cfg
	}

	var errs []error

	// Phase 1: Stop removed channels.
	s.mu.RLock()
	var toRemove []string
	for name := range s.running {
		if _, ok := desired[name]; !ok {
			toRemove = append(toRemove, name)
		}
	}
	s.mu.RUnlock()

	for _, name := range toRemove {
		if err := s.Stop(name); err != nil {
			slog.Warn("reconcile: stop removed channel failed", "name", name, "error", err)
		}
	}

	// Phase 2: Replace changed channels.
	s.mu.RLock()
	var toReplace []core.ChannelConfig
	for name, rc := range s.running {
		if dcfg, ok := desired[name]; ok && !configEqual(rc.config, dcfg) {
			toReplace = append(toReplace, dcfg)
		}
	}
	s.mu.RUnlock()

	for _, cfg := range toReplace {
		ch, err := channel.FromConfig(cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cfg.ChannelType, err))
			continue
		}
		if err := s.ReplaceSpawn(ch, cfg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cfg.ChannelType, err))
		}
	}

	// Phase 3: Spawn new channels.
	s.mu.RLock()
	var toAdd []core.ChannelConfig
	for name, cfg := range desired {
		if _, ok := s.running[name]; !ok {
			toAdd = append(toAdd, cfg)
		}
	}
	s.mu.RUnlock()

	for _, cfg := range toAdd {
		ch, err := channel.FromConfig(cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cfg.ChannelType, err))
			continue
		}
		if err := s.TrySpawn(ch, cfg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cfg.ChannelType, err))
		}
	}

	return errors.Join(errs...)
}

// StopAll cancels all running channels in parallel and waits for them
// to exit with a single shared deadline. Used during graceful shutdown.
func (s *ChannelSpawner) StopAll() {
	s.mu.Lock()
	snapshot := make(map[string]*runningChannel, len(s.running))
	for k, v := range s.running {
		snapshot[k] = v
	}
	// Clear the map under lock so GetChannel returns false immediately.
	for k := range s.running {
		delete(s.running, k)
	}
	s.mu.Unlock()

	if len(snapshot) == 0 {
		return
	}

	// Cancel all contexts first (immediate signal to all goroutines).
	for _, rc := range snapshot {
		rc.cancel()
	}

	// Wait for all goroutines to exit with a single shared deadline.
	deadline := time.After(stopTimeout)
	for name, rc := range snapshot {
		select {
		case <-rc.done:
			slog.Info("channel stopped", "name", name)
		case <-deadline:
			slog.Error("channel stopAll: shared deadline exceeded", "name", name)
			return // remaining channels will be abandoned
		}
	}
}

// List returns the status of all running channels.
func (s *ChannelSpawner) List() []ChannelStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make([]ChannelStatus, 0, len(s.running))
	for name, rc := range s.running {
		statuses = append(statuses, ChannelStatus{
			Name:    name,
			Type:    string(rc.config.ChannelType),
			Running: true,
		})
	}
	return statuses
}
