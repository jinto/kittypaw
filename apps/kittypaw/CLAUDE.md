# GoPaw

Go port of KittyPaw — an AI agent framework with JavaScript sandbox execution, multi-channel messaging, and skill learning.

## Architecture

```
cmd/gopaw/     CLI binary (Cobra)
core/          Types, config, skill management, persona profiles/presets, WebSocket protocol
llm/           LLM provider abstraction (Claude, OpenAI, Ollama)
mcp/           MCP client registry (external tool server connections)
sandbox/       JavaScript execution sandbox (in-process goja VM)
store/         SQLite persistence with 16 migrations (WAL mode)
engine/        Agent loop state machine, skill executor, compaction, scheduling
channel/       Messaging channels (Telegram, Slack, Discord, Kakao, WebSocket)
server/        HTTP API (Chi) + WebSocket streaming + ChannelSpawner (hot-reload)
client/        REST API client library
```

## Build & Run

```bash
go build ./cmd/gopaw
./gopaw init
./gopaw serve --bind :3000
```

## Key Design Decisions (vs Rust original)

- **No CGO**: Uses `modernc.org/sqlite` (pure Go) instead of sqlite3
- **In-process sandbox**: Uses `goja` (pure Go JS engine) instead of fork+Seatbelt+QuickJS
- **Official SDKs**: Raw HTTP for Anthropic/OpenAI APIs (with SSE streaming)
- **Goroutines**: Replace tokio async with goroutines + channels
- **Chi router**: Replaces Axum for HTTP routing
- **Cobra CLI**: Replaces Clap for command-line parsing

## Config

TOML config at `~/.gopaw/config.toml`. See `core/config.go` for all options.

## Testing

```bash
go test ./...
```
