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
relay/         KakaoTalk relay (Cloudflare Workers, TypeScript)
```

## Key Design Decisions (vs Rust original)

- **No CGO**: Uses `modernc.org/sqlite` (pure Go) instead of sqlite3
- **In-process sandbox**: Uses `goja` (pure Go JS engine) instead of fork+Seatbelt+QuickJS
- **Official SDKs**: Raw HTTP for Anthropic/OpenAI APIs (with SSE streaming)
- **Goroutines**: Replace tokio async with goroutines + channels
- **Chi router**: Replaces Axum for HTTP routing
- **Cobra CLI**: Replaces Clap for command-line parsing
- **Multi-tenant BaseDir**: All filesystem operations use `Session.BaseDir` via `*From(baseDir, ...)` function variants, enabling per-tenant data isolation without engine/handler changes

## Skill Install Internals

Supports two source formats:
- **SKILL.md** (agentskills.io standard) — YAML frontmatter + markdown body. Installed in prompt mode (LLM executes with permission-scoped tools) by default, or `--mode native` for JS conversion via teach pipeline.
- **Native** (`package.toml` + `main.js`) — installed directly via PackageManager.

Provenance tracked via `SourceURL`, `SourceHash`, `SourceText` fields on Skill. SHA256 verification for registry packages.

Config fields support `source = "namespace/key"` binding to resolve values from shared `secrets.json` (e.g. `source = "telegram/bot_token"`). Resolution order: package-scoped → source binding → default. Secrets file auto-migrates from flat/mixed formats to canonical nested JSON on first load.

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
