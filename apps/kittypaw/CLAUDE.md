# GoPaw

Go port of KittyPaw — an AI agent framework with JavaScript sandbox execution, multi-channel messaging, and skill learning.

## Architecture

```
cmd/gopaw/     CLI binary (Cobra)
core/          Types, config, skill management, persona profiles/presets, tenant isolation, WebSocket protocol, setup wizard shared logic
llm/           LLM provider abstraction (Claude, OpenAI, Ollama)
mcp/           MCP client registry (external tool server connections)
sandbox/       JavaScript execution sandbox (in-process goja VM)
store/         SQLite persistence with 17 migrations (WAL mode)
engine/        Agent loop state machine, skill executor, compaction, scheduling
channel/       Messaging channels (Telegram, Slack, Discord, Kakao, WebSocket)
server/        HTTP API (Chi) + WebSocket streaming + ChannelSpawner (hot-reload)
client/        REST/WS client + DaemonConn (thin client: auto daemon discovery/start)
```

## Build & Run

```bash
go build ./cmd/gopaw
./gopaw init                # interactive 4-step wizard (LLM, Telegram, workspace, HTTP)
./gopaw serve --bind :3000
```

Non-interactive setup for CI:
```bash
./gopaw init --provider anthropic --api-key $ANTHROPIC_API_KEY
```

## Key Design Decisions (vs Rust original)

- **No CGO**: Uses `modernc.org/sqlite` (pure Go) instead of sqlite3
- **In-process sandbox**: Uses `goja` (pure Go JS engine) instead of fork+Seatbelt+QuickJS
- **Official SDKs**: Raw HTTP for Anthropic/OpenAI APIs (with SSE streaming)
- **Goroutines**: Replace tokio async with goroutines + channels
- **Chi router**: Replaces Axum for HTTP routing
- **Cobra CLI**: Replaces Clap for command-line parsing
- **Multi-tenant BaseDir**: All filesystem operations use `Session.BaseDir` via `*From(baseDir, ...)` function variants, enabling per-tenant data isolation without engine/handler changes

## Skill Install

```bash
gopaw install https://github.com/owner/repo   # install from GitHub
gopaw install /path/to/local/skill             # install from local directory
gopaw search <keyword>                          # search skill registry
```

Supports two source formats:
- **SKILL.md** (agentskills.io standard) — YAML frontmatter + markdown body. Installed in prompt mode (LLM executes with permission-scoped tools) by default, or `--mode native` for JS conversion via teach pipeline.
- **Native** (`package.toml` + `main.js`) — installed directly via PackageManager.

Provenance tracked via `SourceURL`, `SourceHash`, `SourceText` fields on Skill. SHA256 verification for registry packages.

## Config

TOML config at `~/.gopaw/config.toml`. See `core/config.go` for all options.
Server-wide settings (bind, master API key, tenants) go in `~/.gopaw/server.toml`. See `core/config.go:TopLevelServerConfig`.

Registry URL (default: GitHub `kittypaw/skills`):
```toml
[registry]
url = "https://raw.githubusercontent.com/kittypaw/skills/main"
```

## Testing

```bash
go test ./...
```
