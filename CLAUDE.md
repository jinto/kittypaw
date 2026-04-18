# KittyPaw

AI agent framework with JavaScript sandbox execution, multi-channel messaging, and skill learning.

## Architecture

```
cli/           CLI binary (Cobra)
core/          Types, config, skill management, persona profiles/presets, tenant isolation, WebSocket protocol, setup wizard shared logic
llm/           LLM provider abstraction (Claude, OpenAI, Ollama)
mcp/           MCP client registry (external tool server connections)
sandbox/       JavaScript execution sandbox (in-process goja VM, Agent.observe interrupts)
store/         SQLite persistence with 17 migrations (WAL mode)
engine/        Agent loop (observe + retry), skill executor, HTML-to-Markdown, SearchBackend, compaction, scheduling
channel/       Messaging channels (Telegram, Slack, Discord, Kakao, WebSocket)
server/        HTTP API (Chi) + WebSocket streaming + ChannelSpawner (hot-reload)
client/        REST/WS client + DaemonConn (thin client: auto daemon discovery/start)
relay/         KakaoTalk relay (Rust, axum + SQLite, self-hosted single binary)
```

## Key Design Decisions (vs Rust original)

- **No CGO**: Uses `modernc.org/sqlite` (pure Go) instead of sqlite3
- **In-process sandbox**: Uses `goja` (pure Go JS engine) instead of fork+Seatbelt+QuickJS
- **Official SDKs**: Raw HTTP for Anthropic/OpenAI APIs (with SSE streaming)
- **Goroutines**: Replace tokio async with goroutines + channels
- **Chi router**: Replaces Axum for HTTP routing
- **Cobra CLI**: Replaces Clap for command-line parsing
- **Multi-tenant BaseDir**: All filesystem operations use `Session.BaseDir` via `*From(baseDir, ...)` function variants, enabling per-tenant data isolation without engine/handler changes
- **Tenant routing**: Single daemon serves N personal tenants + optional `family` tenant. `TenantRouter` fans inbound events to the right `Session` by `Event.TenantID`; `ChannelSpawner` keys by `(tenantID, channel, alias)` so the same bot token can't be duplicated across tenants. See `core/tenant.go`, `server/tenant_router.go`.

## Family Tenant (cross-tenant read + fanout)

`Config.IsFamily = true` marks a tenant as the family-only shared space. Two JS skills are conditionally exposed to it:

- **`Share.read(tenantID, path)`** — reads a file from another tenant's BaseDir if the owner's `[share.<reader>]` allowlist includes the requested path (exact match, no globs). Defense in depth: (1) `executeShare` rejects any target whose `Config.IsFamily != true` before consulting the allowlist (closes I5 — a personal tenant's `[share.<peer>]` config cannot be weaponized into personal↔personal access); (2) `sandbox.Options.ExposeShare = !Config.IsFamily` — the family tenant itself never sees the `Share` JS global (`typeof Share === "undefined"`), so a compromised family skill can't loop back to read other personal tenants. Every call emits a `cross_tenant_read` / `cross_tenant_read_rejected` audit log; the externally-visible error string is unified across "unknown tenant" and "not family" branches to prevent tenant ID enumeration via error oracle. `core.ValidateSharedReadPath` blocks `..`, absolute paths, symlinks, and hardlink escapes; `ValidateTenantID` rejects hostile IDs before any log/registry touch. Size-capped at `maxFileReadSize` (10 MB).
- **`Fanout.send(tenantID, {text, channel_hint})` / `Fanout.broadcast({...})`** — emits `Event{Type: EventFamilyPush, TenantID: target}` onto the shared eventCh. `Server.dispatchLoop` branches on `EventFamilyPush` *before* `event.ParsePayload()` and routes via `deliverFamilyPush` — the message is already-composed outbound text, so the agent loop is bypassed (re-invoking the LLM would paraphrase or drop it). Channel selection: `ChannelHint` exact match wins, otherwise the first non-`web_chat` channel (`web_chat` is per-WebSocket, no durable destination); destination is the target tenant's `AdminChatIDs[0]`. If the channel isn't currently running (hot-reload window), the push lands in `pending_responses` for the retry loop — except Kakao, whose action IDs are ephemeral. Personal tenants never see the `Fanout` JS global (`typeof Fanout === "undefined"`), gated by `sandbox.Options.ExposeFanout` threaded through `ExecuteWithResolverOpts`/`ExecutePackageOpts`. Both flags are produced by `Session.sandboxOptions()` so callsites stay consistent.

Personal tenants cannot invoke `Share.read` against each other — only family's `[share.<personal>]` entries grant access, and only family's Session has a non-nil `Fanout`. Channel configs on the family tenant are rejected at config load (`ValidateFamilyTenants`).

## Skill Install Internals

Supports two source formats:
- **SKILL.md** (agentskills.io standard) — YAML frontmatter + markdown body. Installed in prompt mode (LLM executes with permission-scoped tools) by default, or `--mode native` for JS conversion via teach pipeline.
- **Native** (`package.toml` + `main.js`) — installed directly via PackageManager.

Provenance tracked via `SourceURL`, `SourceHash`, `SourceText` fields on Skill. SHA256 verification for registry packages.

Config fields support `source = "namespace/key"` binding to resolve values from shared `secrets.json` (e.g. `source = "telegram/bot_token"`). Resolution order: package-scoped → source binding → default. Secrets file auto-migrates from flat/mixed formats to canonical nested JSON on first load.

## API Token Management

`kittypaw login` authenticates against a kittypaw-api server via OAuth (Google). Two modes:
- **HTTP callback** (default): local server on `127.0.0.1:0`, browser-based OAuth flow.
- **Code-paste** (`--code`): for SSH/remote sessions, enter a one-time code from the browser.

Tokens stored in `secrets.json` under namespace `kittypaw-api/{host}` (e.g. `kittypaw-api/localhost:8080`).
`APITokenManager` (`core/api_token.go`) handles auto-refresh with single-flight mutex pattern.
JWT expiry checked client-side via base64 payload decode (no signature verification) with 30-second grace window.

Before OAuth, `loginHTTP`/`loginCode` call unauthenticated `GET {apiURL}/discovery` (see `core/discovery.go`) to resolve service topology. The response persists three URLs per-host under the portal namespace — `api_base_url`, `relay_url`, `skills_registry_url` — with empty-string-deletes semantics so stale URLs don't survive a relay migration. Discovery failures log a warning and fall back to the user-supplied `apiURL` so login works in collapsed deployments.

KakaoTalk relay pairing skips OAuth entirely: `wizardKakao` (CLI) and `handleSetupKakaoRegister` (web) hit `/discovery` anonymously, `POST {relay_url}/register` directly, then persist the full `wss://…/ws/{token}` under `kittypaw-api/{host}`. `InjectKakaoWSURL` (invoked by `ChannelSpawner.Reconcile` as the single injection site) reads that secret at spawn time and populates `KakaoWSURL` on the runtime channel config — the token is never written to `config.toml`, so rotations don't require a config edit.

Packages with `source = "kittypaw-api/access_token"` config fields get auto-refreshed tokens at execution time (`engine/executor.go:runSkillOrPackage`).

## HTTP Sandbox Security

`Http.get/post/put/delete/patch/head` support an optional `options` argument: `Http.get(url, {headers: {key: value}})`.
Hop-by-hop headers (`Host`, `Connection`, `Transfer-Encoding`, `Upgrade`, `TE`, `Trailer`) are blocked.

SSRF validation (`validateHTTPTarget`): explicit `allowed_hosts` in `package.toml` takes priority over private IP blocking — packages can declare `allowed_hosts = ["localhost"]` to reach local API servers. The package resolver validates URLs against the package's AllowedHosts and stores the validated hostname in context; `executeHTTP` verifies the actual request hostname matches.

## Permission System

Destructive operations (Shell.exec, Git.push, etc.) require user approval in `supervised` autonomy mode.
Chat channels that implement `channel.Confirmer` (currently Telegram) show an inline keyboard for approve/deny.
Config: `[permissions]` section in `config.toml` — `require_approval` (operation list) + `timeout_seconds`.
Callback responses route through channel-internal `sync.Map` (not `eventCh`) to prevent dispatchLoop deadlock.

## Config Internals

TOML config at `~/.kittypaw/config.toml`. See `core/config.go` for all options.
Server-wide settings (bind, master API key, tenants) go in `~/.kittypaw/server.toml`. See `core/config.go:TopLevelServerConfig`.

## Development

```bash
make build          # Build binary
make test           # All tests (verbose, no cache)
make test-unit      # Short tests only
make lint           # golangci-lint (errcheck, staticcheck, revive, misspell, ...)
make fmt            # gofmt + goimports
```

### Commit Conventions

Conventional Commits enforced by [lefthook](https://github.com/evilmartians/lefthook):

```
type(scope): description

types: feat, fix, refactor, perf, docs, chore, test, ci, build, merge
```

Pre-commit hooks run `gofmt` and `golangci-lint` automatically. Install with:

```bash
lefthook install
```

### CI

GitHub Actions runs `lint` and `test` on every push/PR to `main`. See `.github/workflows/ci.yml`.

## Release

Version is injected via ldflags (`-X main.version`). `kittypaw --version` prints it.

## Testing

```bash
make test           # All tests
make test-unit      # Unit tests only (fast)
go test ./store/... # Single package
```
