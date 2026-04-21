package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/engine"
	"github.com/jinto/kittypaw/llm"
	mcpreg "github.com/jinto/kittypaw/mcp"
	"github.com/jinto/kittypaw/sandbox"
	"github.com/jinto/kittypaw/store"
)

// TenantDeps is the per-tenant set of dependencies server.New needs to
// build an engine.Session for one tenant. The daemon entry point (CLI
// serve) opens these per-tenant resources (DB, LLM provider, sandbox)
// before handing the slice to server.New — the server package stays out
// of discovery/migration business.
//
// Fallback, McpRegistry, and LiveIndexer may be nil. Everything else is
// required. LiveIndexer is nil when [workspace] live_index = false or
// when the OS watcher could not be created (inotify limit, etc.) — the
// tenant is then in lazy-reindex mode.
type TenantDeps struct {
	Tenant      *core.Tenant
	Store       *store.Store
	Provider    llm.Provider
	Fallback    llm.Provider
	Sandbox     *sandbox.Sandbox
	McpRegistry *mcpreg.Registry
	PkgMgr      *core.PackageManager
	APITokenMgr *core.APITokenManager
	Secrets     *core.SecretsStore
	LiveIndexer *engine.LiveIndexer
}

// Close releases OS-owned resources: the LiveIndexer (fsnotify watchers),
// the SQLite store, and every connected MCP server session. LiveIndexer
// closes before the store so any in-flight IndexFile call on the indexer
// finishes against a live DB. Provider/Sandbox/PkgMgr hold no file
// handles or child processes, so they are left to GC. Safe to call once;
// subsequent calls on a store that is already closed return the
// underlying error.
func (td *TenantDeps) Close() error {
	if td == nil {
		return nil
	}
	if td.LiveIndexer != nil {
		if err := td.LiveIndexer.Close(); err != nil {
			slog.Warn("close live indexer", "tenant", td.Tenant.ID, "error", err)
		}
	}
	if td.McpRegistry != nil {
		td.McpRegistry.Shutdown()
	}
	if td.Store == nil {
		return nil
	}
	return td.Store.Close()
}

// OpenTenantDeps opens every per-tenant dependency needed to build an
// engine.Session: filesystem layout, SQLite store, LLM provider (plus
// optional fallback), sandbox, secrets store, package manager, API token
// manager, and — when [mcp] is declared in config — a connected MCP
// registry.
//
// Used by both the CLI daemon boot path (cli/main.go bootstrap) and the
// runtime tenant-add path (Server.AddTenant). Keeping the construction
// in one place ensures hot-added tenants are indistinguishable from
// tenants loaded at startup.
//
// On error, any resource already opened (notably the SQLite store) is
// closed before returning so callers never see a half-initialized
// TenantDeps. LoadSecretsFrom failures do NOT abort: pkgMgr is still
// constructed with a nil secrets store, preserving prior bootstrap
// behavior for tenants missing a secrets.json.
func OpenTenantDeps(t *core.Tenant) (*TenantDeps, error) {
	if t == nil || t.Config == nil {
		return nil, fmt.Errorf("open tenant deps: tenant or config is nil")
	}

	if err := t.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("ensure dirs for %s: %w", t.ID, err)
	}

	st, err := store.Open(t.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open store for %s: %w", t.ID, err)
	}

	provider, err := llm.NewProviderFromConfig(t.Config.LLM)
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("create llm provider for %s: %w", t.ID, err)
	}

	var fallback llm.Provider
	if m := t.Config.DefaultModel(); m != nil {
		fallback, _ = llm.NewProviderFromModelConfig(*m)
	}

	sbox := sandbox.New(t.Config.Sandbox)

	secrets, secretsErr := core.LoadSecretsFrom(t.SecretsPath())
	if secretsErr != nil {
		slog.Warn("failed to load secrets store, package config will be limited",
			"tenant", t.ID, "error", secretsErr)
	}
	pkgMgr := core.NewPackageManagerFrom(t.BaseDir, secrets)
	apiTokenMgr := core.NewAPITokenManager(t.BaseDir, secrets)

	var mcpReg *mcpreg.Registry
	if len(t.Config.MCPServers) > 0 {
		if err := mcpreg.ValidateConfig(t.Config.MCPServers); err != nil {
			_ = st.Close()
			return nil, fmt.Errorf("MCP config for %s: %w", t.ID, err)
		}
		mcpReg = mcpreg.NewRegistry(t.Config.MCPServers)
		connectCtx, connectCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if errs := mcpReg.ConnectAll(connectCtx); len(errs) > 0 {
			slog.Warn("some MCP servers failed to connect",
				"tenant", t.ID, "failures", len(errs))
		}
		connectCancel()
	}

	return &TenantDeps{
		Tenant:      t,
		Store:       st,
		Provider:    provider,
		Fallback:    fallback,
		Sandbox:     sbox,
		McpRegistry: mcpReg,
		PkgMgr:      pkgMgr,
		APITokenMgr: apiTokenMgr,
		Secrets:     secrets,
	}, nil
}

// buildTenantSession wires a single TenantDeps into a ready-to-dispatch
// engine.Session. Used both by server.New at boot and by Server.AddTenant
// at runtime so hot-added tenants are indistinguishable from those loaded
// at startup.
//
// Side effects (all best-effort, logged on failure — none abort):
//   - Seeds workspace_files from config.Sandbox.AllowedPaths.
//   - Populates Session.AllowedPaths via RefreshAllowedPaths.
//   - Spawns a background goroutine that runs the FTS5 indexer over every
//     registered workspace for this tenant.
//
// Family tenants receive a ChannelFanout wired to the shared eventCh;
// personal tenants leave sess.Fanout == nil so the sandbox hides the
// Fanout JS global (I5 — personal cannot reach personal).
func buildTenantSession(td *TenantDeps, registry *core.TenantRegistry, eventCh chan<- core.Event) *engine.Session {
	if len(td.Tenant.Config.Sandbox.AllowedPaths) > 0 {
		if err := td.Store.SeedWorkspacesFromConfig(td.Tenant.Config.Sandbox.AllowedPaths); err != nil {
			slog.Error("seed workspaces from config", "tenant", td.Tenant.ID, "error", err)
		}
	}

	sess := &engine.Session{
		Provider:         td.Provider,
		FallbackProvider: td.Fallback,
		Sandbox:          td.Sandbox,
		Store:            td.Store,
		Config:           td.Tenant.Config,
		McpRegistry:      td.McpRegistry,
		BaseDir:          td.Tenant.BaseDir,
		PackageManager:   td.PkgMgr,
		APITokenMgr:      td.APITokenMgr,
		TenantID:         td.Tenant.ID,
		TenantRegistry:   registry,
		Health:           core.NewHealthState(),
		SummaryFlight:    &singleflight.Group{},
	}
	if td.Tenant.Config.IsFamily {
		sess.Fanout = core.NewChannelFanout(eventCh, registry, td.Tenant.ID)
	}

	if err := sess.RefreshAllowedPaths(); err != nil {
		slog.Warn("startup: failed to load workspace paths, file access denied by default",
			"tenant", td.Tenant.ID, "error", err)
	}

	indexer := engine.NewFTS5Indexer(td.Store)
	sess.Indexer = indexer

	// Live indexing is opt-out via [workspace] live_index = false. Attempt
	// to open an fsnotify watcher eagerly; a failure (OS limit, etc.)
	// drops us into lazy mode — the bulk Index still runs, File.reindex
	// still works, just no automatic re-index on filesystem changes.
	var liveIdx *engine.LiveIndexer
	if td.Tenant.Config.Workspace.LiveIndex {
		li, err := engine.NewLiveIndexer(indexer, engine.DefaultLiveInterval, engine.DefaultLiveCap)
		if err != nil {
			slog.Warn("workspace entering lazy index mode",
				"tenant", td.Tenant.ID, "reason", "watcher init failed", "error", err)
		} else {
			liveIdx = li
			td.LiveIndexer = li
		}
	}

	go func(tenantID string, st *store.Store, idx engine.Indexer, live *engine.LiveIndexer) {
		wss, err := st.ListWorkspaces()
		if err != nil {
			slog.Warn("startup: failed to list workspaces for indexing",
				"tenant", tenantID, "error", err)
			return
		}
		// Watch BEFORE bulk index: a filesystem change during the initial
		// walk would otherwise land after the walker passed and before
		// fsnotify is listening, leaving FTS permanently out-of-sync.
		// IndexFile is idempotent, so overlap is safe.
		if live != nil {
			live.Start()
			for _, ws := range wss {
				if err := live.AddWorkspace(ws.ID, ws.RootPath); err != nil {
					slog.Warn("workspace entering lazy index mode",
						"tenant", tenantID, "workspace_id", ws.ID, "error", err)
				}
			}
		}
		for _, ws := range wss {
			if _, err := idx.Index(context.Background(), ws.ID, ws.RootPath); err != nil {
				slog.Warn("startup: workspace indexing failed",
					"tenant", tenantID, "workspace_id", ws.ID,
					"root_path", ws.RootPath, "error", err)
			}
		}
	}(td.Tenant.ID, td.Store, indexer, liveIdx)

	return sess
}
