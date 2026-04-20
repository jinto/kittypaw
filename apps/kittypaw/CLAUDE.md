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
- **Tenant routing**: Single daemon serves N personal tenants + optional `family` tenant. `TenantRouter` fans inbound events to the right `Session` by `Event.TenantID`; `ChannelSpawner` keys by `(tenantID, channelType)` — each tenant hosts at most one channel per type (one Telegram bot, one Kakao relay, etc). Mental model: tenant == one human user. Multi-channel-per-tenant (e.g. two Telegram bots under one tenant) is out of scope; a household uses multiple tenants + a `family` coordinator. `ValidateTenantID` also accepts leading-underscore IDs like `_default_`/`_shared_` for future reserved-form use — no boot logic auto-creates them. Provisioning via `kittypaw tenant add <name>` (`cli/cmd_tenant.go`, `core/tenant_setup.go`) stages the directory under `.<id>.staging/` and renames atomically; duplicate bot-token validation runs pre-commit so collisions fail before any filesystem write. For bulk household onboarding, `kittypaw family init` (`cli/cmd_family.go`) wraps the same provisioning path in an interactive TTY loop that prompts for each member's name/token/chat_id, de-duplicates tokens in-run, creates a `family` coordinator tenant at the end (skippable with `--no-family`), and is idempotent on re-run (existing tenants are skipped, not overwritten). When a daemon is already running, the CLI auto-activates via `POST /api/v1/admin/tenants` (`server/admin.go:AddTenant`) — no restart required (AC-U3). `AddTenant` serializes under `tenantMu` (shared with `RemoveTenant`), re-runs the bot-token collision check against the live tenant snapshot, opens per-tenant deps via `OpenTenantDeps` (same path as boot), stores them in `Server.tenantDeps` keyed by tenant ID, and uses an LIFO rollback stack so a failed channel reconcile fully unwinds registry/router/tenantList/tenantDeps side effects. `--no-activate` stages files only. Decommissioning uses `kittypaw tenant remove <name>`: (1) if the daemon is running, `POST /api/v1/admin/tenants/{id}/delete` drains channels via `Reconcile(id, nil)` and tears down registry entries in exact LIFO order (`tenants.Remove` → `tenantList` pop → `tenantRegistry.Unregister` → `delete(tenantDeps,id)` → `TenantDeps.Close`); (2) daemon-off or daemon-missing-config paths skip the RPC silently; (3) if the removed tenant is not itself the family tenant, the CLI scrubs `[share.<removed>]` from the family `config.toml` via `WriteConfigAtomic`; (4) the tenant directory is moved to `.trash/<id>-<YYYYMMDDHHMMSS>/` (collision-suffixed) so operators can recover it; (5) the CLI prints reminders to revoke the BotFather token and re-pair Kakao manually — the daemon never touches external credentials. See `core/tenant.go`, `server/tenant_router.go`, `server/admin.go`, `cli/cmd_tenant.go`.
- **Tenant panic isolation (AC-T8)**: Every worker goroutine wraps work in `engine.RecoverTenantPanic` (or the `engine.runWithTenantRecover` helper). A panic marks only the owning tenant `Degraded` via `core.HealthState` (atomic state machine: Ready / Degraded / Stopped-terminal) and never propagates to siblings; clean completion promotes back to `Ready`. Chokepoints: scheduler `tickOnce` / `reflectionTick` / skill goroutine / package goroutine, and `server.dispatchLoop` per-event. Health state is nil-safe for bare-struct test fixtures.

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

## MoA (Mixture of Agents)

`Moa.query(prompt, options?)` JS skill runs parallel fan-out to multiple named models from config `[[models]]`, then synthesizes the responses via the Default model. Implemented in `engine/moa.go`. Public core: `QueryMoA(ctx, MoARequest, ProviderResolver, *SharedTokenBudget)`; the JS adapter `executeMoA` in the same file fills `MoARequest.Models` from `s.Config.Models` and `SynthesizerModel` from the `Default=true` entry when the caller omits them.

**Design decisions (load-bearing)**:
- **Partial failure tolerant** — uses `sync.WaitGroup` + indexed slots (NOT `errgroup`, whose fail-fast cancel-siblings-on-first-error semantics are wrong for MoA). A single flaky/slow model lands as `candidates[i].Error` without failing the run.
- **Per-model timeout** — each candidate gets `context.WithTimeout` (default `moaDefaultTimeout=30s`, override via `options.per_model_timeout_ms`). Independent of the outer ctx so one slow model can't stall the rest.
- **Hard guardrails** — `len(models) > moaMaxModels (5)` → error (prevents accidental cost explosion from `[[models]]` table growth); `len(models) == 0` → error; `len(models) == 1` at the JS adapter → skip fan-out, degrade to a plain `Llm.generate`-style call + `slog.Info("moa: single-model config, degrading")`.
- **Synthesis skip** — ≤1 successful candidate means synthesis would just paraphrase; `QueryMoA` returns the sole candidate with `synthesized=false` and `model=<candidate model>`. Saves an LLM call and preserves original phrasing.
- **Synthesizer context protection** — per-candidate text is truncated to `moaCandidateCharLimit (8000 chars ≈ 2000 tokens)` before being fed to the synthesizer, bounded by `moaTruncate`. Even 5 candidates stay within 10K chars of synthesizer input.
- **Budget accounting** — `SharedTokenBudget.TrySpendFromUsage` is called for each successful candidate AND the synthesizer response. On exhaustion mid-flight, the in-flight goroutine calls `cancelAll()` to kill pending siblings; already-returned responses are kept (the API cost was paid) to maximize the value of completed work. Synthesizer overshoot is logged warn but not rejected (advisory post-hoc).
- **Synthesizer fallback chain** — if `resolver(SynthesizerModel)` returns nil, fall back to the first successful candidate's model. If that is also unresolvable, return `firstSuccess` with `synthesized=false`. MoA never errors purely due to a misconfigured synthesizer when real candidate data exists.
- **slog key hygiene** — MoA-level log fields use `moa_model` (not `model`) to avoid collision with provider-level slog context.

**JS API** (registered in `core/skillmeta.go` under `Moa`):
```js
Moa.query(prompt, {
  models: ["sonnet", "gpt-4"],    // optional, defaults to all [[models]] names
  synthesizer: "sonnet",            // optional, defaults to Default=true model
  per_model_timeout_ms: 30000       // optional, 30s default
}) → {
  text, model,                       // synthesizer output (or sole candidate when synthesized=false)
  usage: {input_tokens, output_tokens},
  candidates: [{model, text, usage, error?, latency_ms}],
  synthesized: boolean               // false when ≤1 candidate or synthesizer fallback hit
}
```

**Out of scope** (intentionally, to keep scope bounded): streaming synthesis, debate/critique mode, response caching, weighted voting. Each is a separate future plan.

## Permission System

Destructive operations (Shell.exec, Git.push, etc.) require user approval in `supervised` autonomy mode.
Chat channels that implement `channel.Confirmer` (currently Telegram) show an inline keyboard for approve/deny.
Config: `[permissions]` section in `config.toml` — `require_approval` (operation list) + `timeout_seconds`.
Callback responses route through channel-internal `sync.Map` (not `eventCh`) to prevent dispatchLoop deadlock.

## Config Internals

TOML config at `~/.kittypaw/config.toml`. See `core/config.go` for all options.
Server-wide settings (bind, master API key, tenants) go in `~/.kittypaw/server.toml`. See `core/config.go:TopLevelServerConfig`.

## Live Workspace Indexing

Workspace files are indexed into FTS5 incrementally via an fsnotify pipeline: `engine.Watcher` (recursive Add, excludedDirs, editor-temp-file filter, drains both `Events` and `Errors`) → `engine.Debouncer` (500 ms coalesce, 2 s cap, fake-clock-driven tests) → `engine.LiveIndexer.IndexFile`/`RemoveFile` on the existing `FTS5Indexer`. One `LiveIndexer` per tenant, constructed in `server.buildTenantSession` and stored on `TenantDeps.LiveIndexer`. **Startup order is watch-before-bulk-walk**: the startup goroutine calls `live.Start()` + `AddWorkspace` for every registered workspace *before* firing the bulk `Indexer.Index` walk — a filesystem change during the walk would otherwise land after the walker passed and before fsnotify was listening, leaving FTS permanently out-of-sync. `IndexFile` is idempotent so overlap between the initial scan and live events is safe. `handleWorkspacesCreate` reuses the same order (watch first, walk second); `handleWorkspacesDelete` calls `RemoveWorkspace` *before* `DeleteWorkspace` so no stray event lands in `workspace_files` after truncation.

**Symlink defense in depth**: `FTS5Indexer.IndexFile` runs `os.Lstat` before `os.Stat` and skips symlink entries silently — a tenant cannot plant a symlink inside its workspace that points at `/etc/passwd` or another tenant's BaseDir and have it auto-indexed. `store.SeedWorkspacesFromConfig` and API-driven workspace create both canonicalise paths via `filepath.EvalSymlinks` so prefix matching against fsnotify-emitted (symlink-resolved) paths stays consistent on macOS.

Opt-out via `[workspace] live_index = false` — `DefaultConfig` has `LiveIndex: true` (pinned by `TestWorkspaceConfig_DefaultsOn`), and when `LiveIndex=false` the field stays `nil` so v1 behavior (manual `File.reindex`) is preserved (pinned by `TestBuildTenantSession_LiveIndexDisabled`).

**Shutdown order is load-bearing**: `TenantDeps.Close` tears down **LiveIndexer before Store**, and `LiveIndexer.Close` itself runs `ctx.cancel → watcher.Close → consumer.Wait → debouncer.Close`. `Debouncer.Close` waits on an in-flight `sync.WaitGroup` covering fire callbacks currently inside `IndexFile`; without that wait the store would close while an `IndexFile` transaction was still open and log `sql: database is closed`. `LiveIndexer.Start` is serialized with `Close` under `l.mu` so a tenant torn down before its startup goroutine finishes can't race `watcher.Start` against `watcher.Close`. Goroutine leaks are guarded by `TestTenantDeps_Close_NoGoroutineLeak` (3× create/close cycle, ±3 goroutine slack); `go test -race ./engine ./server` is required green. Integration coverage lives under `//go:build integration` in `engine/live_indexer_integration_test.go` — macOS tempdir symlink (`/var/` → `/private/var/`) must be resolved via `filepath.EvalSymlinks` before `AddWorkspace`, otherwise kqueue emits `/private/var/...` paths that don't match the registered workspace root.

**Directory deletes cascade via prefix match**: `FTS5Indexer.RemoveFile` delegates to `store.DeleteWorkspaceFilesByPrefix`, which deletes the exact `abs_path` row *and* every row under it as a subtree (BINARY range `p+"/"` ≤ x < `p+"0"` in a single tx, FTS kept in sync). At fsnotify-Remove time the caller cannot stat the vanished path, so file-vs-dir is resolved server-side — exact match covers the file case, the range covers the dir case, LIKE-metacharacter paths are safe because parameters are bound literally. Trailing slashes on the prefix are normalized. An empty prefix is a no-op (callers wanting a full wipe use `DeleteWorkspaceIndex`).

**Subtree-unwatched visibility**: `Watcher.partialAdds` (atomic int64) counts non-root `fs.Add` / walk failures across `initial_walk` / `initial_subdir` / `runtime_create` phases; each increment emits `slog.Warn` with a `phase` key, and the count is exposed via `Watcher.PartialAddFailures()` + `LiveIndexer.PartialFailures()` (both safe before `Start` / after `Close`). Root Add failures remain terminal errors returned to the caller so the workspace can still enter lazy mode. The counter is cumulative (no reset) and detail-free — detailed path/error forensics stay in the Warn logs.

**Overflow auto-recovery**: `fsnotify.ErrEventOverflow` (Linux `IN_Q_OVERFLOW`, Windows 버퍼 오버런) 감지 시 — 커널 큐가 넘쳐 어떤 watch가 영향 받았는지 알 수 없으므로 — 해당 `Watcher` 가 소유한 모든 workspace 를 `500ms` debounce + `30s` backoff 로 자동 `Reindex`. 전체 walk + upsert + `DeleteStaleWorkspaceFiles` 로 blackout 동안의 create/delete 양방향이 수렴된다. 급격한 오버플로 버스트는 debounce 로 1회에 coalesce 되고, 지속적으로 오버플로하는 커널은 backoff 로 reindex 루프에 빠지지 않는다. `Watcher.OverflowCount()` / `LiveIndexer.RecoveryCount()` atomic API 로 관측 (둘 다 프로세스 시작 이후 누적, `Start` 전·`Close` 후 안전). `LiveIndexer.Close` 는 `ctx.cancel → watcher.Close → consume.Wait → debouncer.Close → overflow.Close` 순서로 진행해 in-flight `Reindex` 가 Close 보다 오래 살지 않는다. `TestLiveIndexer_Close_DuringRecovery_CtxCancelled` / `TestLiveIndexer_Close_NoGoroutineLeak_WithOverflow` 로 고정.

## Onboarding → Chat Auto-Entry

After `kittypaw setup` completes, the CLI drops the user straight into the `kittypaw chat` REPL when four conditions all hold (`cli/cmd_setup.go:autoChatEligible`): stdin is a TTY, stdout is a TTY, no `--provider` flag was passed (that path is non-interactive/CI), and `--no-chat` was not passed. Any one of those false → setup exits normally, preserving CI/scripted paths. The prompt wording (`setupPromptAutoChat` etc.) is golden-string tested — an incidental rewording must be a deliberate test update. Setup also calls `maybeReloadDaemon` before printing the completion box: if a daemon is running it POSTs `/api/v1/reload` so the subsequent chat REPL connects to a server that already sees the new channels; daemon-off prints a hint and returns (never fatal). `maybeReloadDaemon` returns a 3-state `reloadOutcome` (`DaemonOff` / `Reloaded` / `Failed`); if the running daemon **rejects** Reload, `runSetup` prints `setupMsgAutoChatBlocked` and skips auto-entry — attaching chat to a server still holding the previous config would silently run with stale LLM keys or channel tokens. `DaemonOff` and `Reloaded` both allow auto-entry (a fresh daemon reads the new config on spawn). The CLI deliberately does NOT write `user_context.onboarding_completed` to the DB — `server.isOnboardingCompleted` falls back to `cfg.LLM.APIKey != ""` and that fallback is load-bearing (pinned by `TestIsOnboardingCompleted_FallbackToLLMKey`).

**Load-bearing sync contract in `handleReload`**: the handler calls `spawner.Reconcile` synchronously under `tenantMu` and returns only after it completes. `cli/cmd_setup.maybeReloadDaemon` → `runChat` depends on this — if Reconcile ran async, chat would connect before the new channel set was wired up. Pinned by `TestHandleReload_WaitsForReconcile` (barrier-blocking stub) and `TestAutoEntryNoRace` (`-race -count 50` happens-before loop). Converting Reconcile to a goroutine requires updating both the CLI wiring AND those tests.

**Validation contract in `handleReload`**: the proposed cfg is checked with `core.ValidateTenantChannels` (bot_token / Kakao URL dedup against live peers) and `core.ValidateFamilyTenants` (no channels on `is_family=true`) **before** any state mutation — symmetric with `StartChannels` (boot) and `AddTenant` (hot-add). A rejected reload returns `409 Conflict` with an `slog.Error` (`reason=channel_duplicate` or `reason=family_with_channels`); `s.config` and the spawner are untouched so the daemon keeps running on the last-good config. The CLI surface `maybeReloadDaemon` already maps a Reload failure to `setupMsgAutoChatBlocked`, so a bad config.toml edit is caught and reported through the existing path. The entire validate→swap→reconcile sequence runs under `tenantMu` — releasing the lock mid-sequence would let a concurrent `AddTenant` validate against the stale default-tenant channel list and spawn a bot that duplicates the token this reload is about to introduce. Pinned by `TestHandleReload_DuplicateTelegramToken_Rejects`, `TestHandleReload_FamilyWithChannels_Rejects`, `TestHandleReload_SerializesWithAddTenant` (TOCTOU guard), and the `TestHandleReload_ValidConfig_SwapsAndReconciles` happy-path baseline.

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
