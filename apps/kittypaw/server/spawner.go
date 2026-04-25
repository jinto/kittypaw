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

// spawnerKey composites tenant ID and channel type into the lookup key.
// Each tenant hosts at most one channel per type.
type spawnerKey struct {
	TenantID    string
	ChannelType string
}

// runningChannel tracks a single active channel and the machinery needed
// to stop it cleanly. The owning tenant is encoded in the spawnerKey used
// to look up this struct, so it is not repeated here.
type runningChannel struct {
	cancel func()             // cancels the context passed to Start
	ch     channel.Channel    // the live channel instance
	done   chan struct{}      // closed when the Start goroutine exits
	config core.ChannelConfig // config snapshot for Reconcile diff
}

// ChannelStatus is the API-facing representation of a running channel.
type ChannelStatus struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Running  bool   `json:"running"`
}

// ChannelSpawner manages the lifecycle of messaging channels.
// It is safe for concurrent use.
type ChannelSpawner struct {
	mu          sync.RWMutex
	reconcileMu sync.Mutex // serializes Reconcile calls
	running     map[spawnerKey]*runningChannel
	eventCh     chan<- core.Event
	baseCtx     context.Context // long-lived context for channel goroutines
}

// NewChannelSpawner creates a spawner that will pass eventCh to every
// channel it starts. baseCtx should be a long-lived context (e.g., from
// signal.NotifyContext) — all channel goroutines derive their contexts
// from it, regardless of the caller's context.
func NewChannelSpawner(baseCtx context.Context, eventCh chan<- core.Event) *ChannelSpawner {
	return &ChannelSpawner{
		running: make(map[spawnerKey]*runningChannel),
		eventCh: eventCh,
		baseCtx: baseCtx,
	}
}

// TrySpawn starts a channel for tenantID if one with the same
// (tenant, type) is not already running. Idempotent — returns nil if
// already running. The channel goroutine's context is derived from the
// spawner's baseCtx, not the caller's context, so HTTP request contexts
// won't kill long-lived channels.
func (s *ChannelSpawner) TrySpawn(tenantID string, ch channel.Channel, cfg core.ChannelConfig) error {
	key := spawnerKey{TenantID: tenantID, ChannelType: ch.Name()}

	s.mu.Lock()
	if _, exists := s.running[key]; exists {
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
	s.running[key] = rc
	s.mu.Unlock()

	slog.Info("channel spawned",
		"tenant", tenantID, "name", key.ChannelType)
	go func() {
		defer close(done)
		if err := ch.Start(chCtx, s.eventCh); err != nil && chCtx.Err() == nil {
			slog.Error("channel stopped unexpectedly",
				"tenant", tenantID, "name", key.ChannelType, "error", err)
		}
	}()

	return nil
}

// Stop cancels a running channel for (tenantID, channelType) and waits
// for its goroutine to exit.
//
// Lock discipline: the write lock is released BEFORE blocking on <-done.
// This prevents deadlocking concurrent GetChannel/List callers.
func (s *ChannelSpawner) Stop(tenantID, channelType string) error {
	key := spawnerKey{TenantID: tenantID, ChannelType: channelType}
	s.mu.Lock()
	rc, ok := s.running[key]
	if !ok {
		s.mu.Unlock()
		return ErrChannelNotFound
	}
	delete(s.running, key)
	s.mu.Unlock()

	rc.cancel()
	select {
	case <-rc.done:
		slog.Info("channel stopped",
			"tenant", tenantID, "name", channelType)
	case <-time.After(stopTimeout):
		slog.Error("channel stop: timed out waiting for goroutine",
			"tenant", tenantID, "name", channelType)
	}
	return nil
}

// GetChannel returns the Channel for (tenantID, eventType), or nil and
// false if not running. Tenants are isolated: a channel registered under
// tenant A cannot be reached by passing tenant B's ID.
func (s *ChannelSpawner) GetChannel(tenantID string, eventType core.EventType) (channel.Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rc, ok := s.running[spawnerKey{TenantID: tenantID, ChannelType: string(eventType)}]
	if !ok {
		return nil, false
	}
	return rc.ch, true
}

// ReplaceSpawn stops the existing channel for (tenantID, ch.Name()) if
// any and spawns a new one. Used when a channel's config has changed.
func (s *ChannelSpawner) ReplaceSpawn(tenantID string, ch channel.Channel, cfg core.ChannelConfig) error {
	name := ch.Name()
	if err := s.Stop(tenantID, name); err != nil && !errors.Is(err, ErrChannelNotFound) {
		return fmt.Errorf("stop %s/%s: %w", tenantID, name, err)
	}
	return s.TrySpawn(tenantID, ch, cfg)
}

// configEqual compares two ChannelConfig values for diff purposes.
func configEqual(a, b core.ChannelConfig) bool {
	return a.ChannelType == b.ChannelType &&
		a.Token == b.Token &&
		a.BindAddr == b.BindAddr &&
		a.KakaoWSURL == b.KakaoWSURL
}

// Reconcile compares the currently running channels for tenantID against
// the desired configs and starts, stops, or replaces channels to match.
// Channels owned by other tenants are untouched.
//
// Serialized via reconcileMu to prevent concurrent calls from corrupting
// state. Best-effort: individual failures are aggregated, not fatal.
// WebSocket channels are excluded (port-binding makes hot-reload unsafe).
func (s *ChannelSpawner) Reconcile(tenantID string, configs []core.ChannelConfig) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	// Inject Kakao relay WS URL from the tenant's per-tenant secrets into channel configs.
	core.InjectKakaoWSURL(tenantID, configs)

	// Build desired map keyed by channel type, filtering out WebSocket
	// channels.
	desired := make(map[string]core.ChannelConfig)
	for _, cfg := range configs {
		if cfg.ChannelType == core.ChannelWeb {
			continue
		}
		name := string(cfg.ChannelType.ToEventType())
		desired[name] = cfg
	}

	var errs []error

	// Phase 1: Stop removed channels (only within this tenant).
	s.mu.RLock()
	var toRemove []spawnerKey
	for key := range s.running {
		if key.TenantID != tenantID {
			continue
		}
		if _, ok := desired[key.ChannelType]; !ok {
			toRemove = append(toRemove, key)
		}
	}
	s.mu.RUnlock()

	for _, key := range toRemove {
		if err := s.Stop(tenantID, key.ChannelType); err != nil {
			slog.Warn("reconcile: stop removed channel failed",
				"tenant", tenantID, "name", key.ChannelType, "error", err)
		}
	}

	// Phase 2: Replace changed channels (only within this tenant).
	s.mu.RLock()
	var toReplace []core.ChannelConfig
	for key, rc := range s.running {
		if key.TenantID != tenantID {
			continue
		}
		if dcfg, ok := desired[key.ChannelType]; ok && !configEqual(rc.config, dcfg) {
			toReplace = append(toReplace, dcfg)
		}
	}
	s.mu.RUnlock()

	for _, cfg := range toReplace {
		ch, err := channel.FromConfig(tenantID, cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cfg.ChannelType, err))
			continue
		}
		if err := s.ReplaceSpawn(tenantID, ch, cfg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cfg.ChannelType, err))
		}
	}

	// Phase 3: Spawn new channels (only within this tenant).
	s.mu.RLock()
	var toAdd []core.ChannelConfig
	for name, cfg := range desired {
		if _, ok := s.running[spawnerKey{TenantID: tenantID, ChannelType: name}]; !ok {
			toAdd = append(toAdd, cfg)
		}
	}
	s.mu.RUnlock()

	for _, cfg := range toAdd {
		ch, err := channel.FromConfig(tenantID, cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cfg.ChannelType, err))
			continue
		}
		if err := s.TrySpawn(tenantID, ch, cfg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cfg.ChannelType, err))
		}
	}

	return errors.Join(errs...)
}

// StopAll cancels all running channels across all tenants in parallel and
// waits for them to exit with a single shared deadline. Used during
// graceful shutdown.
func (s *ChannelSpawner) StopAll() {
	s.mu.Lock()
	snapshot := s.running
	s.running = make(map[spawnerKey]*runningChannel)
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
	for key, rc := range snapshot {
		select {
		case <-rc.done:
			slog.Info("channel stopped",
				"tenant", key.TenantID, "name", key.ChannelType)
		case <-deadline:
			slog.Error("channel stopAll: shared deadline exceeded",
				"tenant", key.TenantID, "name", key.ChannelType)
			return // remaining channels will be abandoned
		}
	}
}

// List returns the status of every running channel across all tenants.
func (s *ChannelSpawner) List() []ChannelStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make([]ChannelStatus, 0, len(s.running))
	for key, rc := range s.running {
		statuses = append(statuses, ChannelStatus{
			TenantID: key.TenantID,
			Name:     key.ChannelType,
			Type:     string(rc.config.ChannelType),
			Running:  true,
		})
	}
	return statuses
}
