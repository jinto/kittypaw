package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/jinto/kittypaw/core"
)

// ErrTenantAlreadyActive is returned by AddTenant when a tenant with
// the same ID is already registered. HTTP callers translate this to
// 409 Conflict — not 500 — because retrying won't help.
var ErrTenantAlreadyActive = errors.New("tenant already active")

// AddTenant registers a tenant that already exists on disk with the
// live daemon: opens its deps, builds a session, hot-wires it into
// the TenantRegistry / TenantRouter / ChannelSpawner, and spawns its
// channels — all without a daemon restart. This powers AC-U3 (30-second
// add of a new family member).
//
// Invariants (enforced under addTenantMu so two concurrent admin calls
// can't corrupt state):
//   - Tenant ID must be fresh (not already in TenantRouter).
//   - Bot-token / Kakao-URL collisions against live tenants are
//     rejected via ValidateTenantChannels BEFORE any channel spawn —
//     otherwise the new channel would silently steal updates.
//   - Family tenants must not declare channels (ValidateFamilyTenants).
//   - Any failure after a side-effect (registry, router, tenantList,
//     spawner) unwinds every earlier side-effect in reverse order.
//     Deps opened by OpenTenantDeps are also closed on failure.
//
// Scheduler is NOT wired here — the default-tenant scheduler stays as
// the sole tick source until a follow-up commit makes scheduling
// tenant-aware. Channel dispatch (the AC-U3 acceptance criterion) is
// fully live after a successful AddTenant call.
func (s *Server) AddTenant(t *core.Tenant) error {
	if t == nil || t.Config == nil {
		return fmt.Errorf("add tenant: tenant or config is nil")
	}
	if err := core.ValidateTenantID(t.ID); err != nil {
		return err
	}

	s.addTenantMu.Lock()
	defer s.addTenantMu.Unlock()

	if existing := s.tenants.Session(t.ID); existing != nil {
		return fmt.Errorf("%w: %q", ErrTenantAlreadyActive, t.ID)
	}

	// Build the would-be-final channel map so ValidateTenantChannels sees
	// the incoming tenant alongside every live one. Without the snapshot
	// a duplicate telegram bot_token would silently spawn a second
	// long-poller that races the original for updates.
	snapshot := make(map[string][]core.ChannelConfig, len(s.tenantList)+1)
	for _, peer := range s.tenantList {
		if peer == nil || peer.Config == nil {
			continue
		}
		snapshot[peer.ID] = peer.Config.Channels
	}
	snapshot[t.ID] = t.Config.Channels
	if err := core.ValidateTenantChannels(snapshot); err != nil {
		return fmt.Errorf("channel validation: %w", err)
	}
	if err := core.ValidateFamilyTenants(append(append([]*core.Tenant(nil), s.tenantList...), t)); err != nil {
		return fmt.Errorf("family validation: %w", err)
	}

	td, err := OpenTenantDeps(t)
	if err != nil {
		return fmt.Errorf("open deps: %w", err)
	}

	// Rollback stack: undo side effects in LIFO order if any later step
	// fails. Each closure is responsible for exactly one revert.
	var rollback []func()
	rollbackAll := func() {
		for i := len(rollback) - 1; i >= 0; i-- {
			rollback[i]()
		}
	}

	rollback = append(rollback, func() { _ = td.Close() })

	sess := buildTenantSession(td, s.tenantRegistry, s.eventCh)

	s.tenantRegistry.Register(t)
	rollback = append(rollback, func() { s.tenantRegistry.Unregister(t.ID) })

	s.tenants.Register(t.ID, sess)
	rollback = append(rollback, func() { s.tenants.Remove(t.ID) })

	// tenantList is read under addTenantMu by future AddTenant calls for
	// their validation snapshot; StartChannels reads it only at boot (single
	// goroutine) and dispatchLoop does not read it. Append is safe here.
	s.tenantList = append(s.tenantList, t)
	rollback = append(rollback, func() {
		for i, peer := range s.tenantList {
			if peer != nil && peer.ID == t.ID {
				s.tenantList = append(s.tenantList[:i], s.tenantList[i+1:]...)
				return
			}
		}
	})

	if s.spawner != nil && len(t.Config.Channels) > 0 {
		// Reconcile is best-effort: a Phase-2/3 failure leaves earlier phases'
		// channels spawned. Register the cleanup BEFORE the call so a partial
		// success unwinds via Reconcile(_, nil), which drives the same Phase-1
		// path that deletes every (tenantID, *) entry.
		rollback = append(rollback, func() {
			if err := s.spawner.Reconcile(t.ID, nil); err != nil {
				slog.Warn("tenant_add_rollback_spawner_cleanup_failed",
					"tenant", t.ID, "error", err)
			}
		})
		if err := s.spawner.Reconcile(t.ID, t.Config.Channels); err != nil {
			slog.Error("tenant_activate_reconcile_failed",
				"tenant", t.ID, "error", err)
			rollbackAll()
			return fmt.Errorf("reconcile channels: %w", err)
		}
	}

	slog.Info("tenant_activated",
		"tenant", t.ID,
		"is_family", t.Config.IsFamily,
		"channels", len(t.Config.Channels),
	)
	return nil
}

// handleAdminTenantAdd activates a tenant that already exists on disk.
// Request body: {"tenant_id": "charlie"}.
//
// The tenant directory must already be provisioned (typically by
// `kittypaw tenant add <name>`) — this handler does not create files,
// only reads the on-disk config and calls AddTenant. That split keeps
// the HTTP surface narrow: no bot tokens or LLM secrets in the request
// body, nothing for an attacker to inject beyond a tenant ID.
func (s *Server) handleAdminTenantAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TenantID string `json:"tenant_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.TenantID == "" {
		writeError(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	if err := core.ValidateTenantID(body.TenantID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tenantsRoot := s.tenantRegistry.BaseDir()
	tenantDir := filepath.Join(tenantsRoot, body.TenantID)
	cfgPath := filepath.Join(tenantDir, "config.toml")
	cfg, err := core.LoadConfig(cfgPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "tenant not found on disk: "+err.Error())
		return
	}
	t := &core.Tenant{
		ID:      body.TenantID,
		BaseDir: tenantDir,
		Config:  cfg,
	}

	if err := s.AddTenant(t); err != nil {
		switch {
		case errors.Is(err, ErrTenantAlreadyActive):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "activated",
		"tenant_id": body.TenantID,
		"is_family": cfg.IsFamily,
		"channels":  len(cfg.Channels),
	})
}
