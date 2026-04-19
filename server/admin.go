package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/jinto/kittypaw/core"
)

// ErrTenantAlreadyActive is returned by AddTenant when a tenant with
// the same ID is already registered. HTTP callers translate this to
// 409 Conflict — not 500 — because retrying won't help.
var ErrTenantAlreadyActive = errors.New("tenant already active")

// ErrTenantNotActive is returned by RemoveTenant when the target tenant
// isn't registered on this daemon. HTTP callers translate this to 404.
var ErrTenantNotActive = errors.New("tenant not active")

// AddTenant registers a tenant that already exists on disk with the
// live daemon: opens its deps, builds a session, hot-wires it into
// the TenantRegistry / TenantRouter / ChannelSpawner, and spawns its
// channels — all without a daemon restart. This powers AC-U3 (30-second
// add of a new family member).
//
// Invariants (enforced under tenantMu so two concurrent admin calls
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

	s.tenantMu.Lock()
	defer s.tenantMu.Unlock()

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

	if s.tenantDeps == nil {
		s.tenantDeps = make(map[string]*TenantDeps)
	}
	s.tenantDeps[t.ID] = td
	rollback = append(rollback, func() { delete(s.tenantDeps, t.ID) })

	// tenantList is read under tenantMu by future AddTenant calls for
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

// RemoveTenant is the inverse of AddTenant — it drains the tenant's
// channels, tears down every registry entry in LIFO order (mirroring
// AddTenant's build order), and finally closes the SQLite store + MCP
// registry held in tenantDeps. The filesystem layout is NOT touched; the
// CLI that invoked this RPC owns the disk-side move-to-trash step so the
// daemon's trust boundary stays clean.
//
// Caller contract: ID must be a live tenant — unknown IDs return
// ErrTenantNotActive so HTTP callers can respond 404 without retry.
//
// If the channel drain fails, RemoveTenant aborts BEFORE touching any
// registry — the tenant stays runnable so the admin can retry after
// investigating (AC-RM5). Reconcile is idempotent, so a subsequent call
// picks up where the first left off.
func (s *Server) RemoveTenant(id string) error {
	if err := core.ValidateTenantID(id); err != nil {
		return err
	}

	s.tenantMu.Lock()
	defer s.tenantMu.Unlock()

	if s.tenants == nil || s.tenants.Session(id) == nil {
		return fmt.Errorf("%w: %q", ErrTenantNotActive, id)
	}

	if s.spawner != nil {
		if err := s.spawner.Reconcile(id, nil); err != nil {
			slog.Error("tenant_remove_reconcile_failed",
				"tenant", id, "error", err)
			return fmt.Errorf("drain channels: %w", err)
		}
	}

	td := s.tenantDeps[id]
	s.tenants.Remove(id)
	for i, peer := range s.tenantList {
		if peer != nil && peer.ID == id {
			s.tenantList = append(s.tenantList[:i], s.tenantList[i+1:]...)
			break
		}
	}
	s.tenantRegistry.Unregister(id)
	delete(s.tenantDeps, id)

	if td != nil {
		if err := td.Close(); err != nil {
			slog.Warn("tenant_remove_close_partial",
				"tenant", id, "error", err)
		}
	}

	slog.Info("tenant_deactivated", "tenant", id)
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

// handleAdminTenantRemove deactivates a live tenant. The tenant ID comes
// from the URL path (Chi param {id}) — no request body is needed and none
// is accepted. This symmetry with handleAdminTenantAdd keeps the admin
// surface narrow: no JSON attacks, no token leakage through request body.
//
// The daemon does NOT touch the filesystem — that's the CLI's job after a
// 200 response. Status mapping: 200 on success, 404 if not active, 400 on
// malformed ID, 500 on reconcile-drain failure (AC-RM5: CLI aborts before
// touching family config or disk).
func (s *Server) handleAdminTenantRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "tenant id is required")
		return
	}
	if err := core.ValidateTenantID(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.RemoveTenant(id); err != nil {
		switch {
		case errors.Is(err, ErrTenantNotActive):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "deactivated",
		"tenant_id": id,
	})
}
