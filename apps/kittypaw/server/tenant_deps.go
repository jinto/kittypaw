package server

import (
	"github.com/jinto/kittypaw/core"
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
// Fallback and McpRegistry may be nil. Everything else is required.
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
}
