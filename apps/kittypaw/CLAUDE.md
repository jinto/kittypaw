# GoPaw

Go port of KittyPaw — an AI agent framework with JavaScript sandbox execution, multi-channel messaging, and skill learning.

## Architecture

```
cmd/gopaw/     CLI binary (Cobra)
core/          Types, config, skill management, WebSocket protocol
llm/           LLM provider abstraction (Claude, OpenAI, Ollama)
sandbox/       JavaScript execution sandbox (subprocess-based)
store/         SQLite persistence with 14 migrations (WAL mode)
engine/        Agent loop state machine, skill executor, compaction, scheduling
channel/       Messaging channels (Telegram, Slack, Discord, Kakao, WebSocket)
server/        HTTP API (Chi) + WebSocket streaming
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
- **Subprocess sandbox**: Uses `deno run` (or `node`) instead of fork+Seatbelt+QuickJS
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
