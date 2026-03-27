# Oochy

A single-binary AI agent that generates and executes JavaScript code in a dual-layer sandbox.

## Features

- Single binary, zero dependencies — just `./oochy` and an API key
- LLM generates JavaScript (ES2020) code from natural language
- Dual-layer sandbox: QuickJS VM isolation + macOS Seatbelt / Linux Landlock kernel isolation
- Channels: Telegram, Discord, WebSocket
- Web dashboard at localhost:3000
- Per-agent capability model with rate limiting
- SQLite state persistence (WAL mode)

## Quickstart

```bash
# Set your Claude API key
export OOCHY_API_KEY=sk-ant-...

# Single event mode
echo '{"type":"web_chat","payload":{"text":"hello"}}' | ./oochy

# Server mode with all channels
./oochy serve
```

## Configuration

Copy `oochy.toml.example` to `oochy.toml` and customize:

```toml
[llm]
provider = "claude"
api_key = ""          # or set OOCHY_API_KEY env var
model = "claude-sonnet-4-20250514"
max_tokens = 4096

[sandbox]
timeout_secs = 30
memory_limit_mb = 64
allowed_paths = ["/tmp"]
allowed_hosts = ["api.telegram.org", "discord.com"]

# Channel definitions — one entry per integration
[[channels]]
channel_type = "web"
bind_addr = "0.0.0.0:3000"

[[channels]]
channel_type = "telegram"
token = "123456:ABC-your-bot-token"

[[channels]]
channel_type = "discord"
token = "your-discord-bot-token"

# Agent definitions — one entry per agent
[[agents]]
id = "assistant"
name = "Assistant"
system_prompt = "You are a helpful assistant."
channels = ["web", "telegram"]

  [[agents.allowed_skills]]
  skill = "Telegram"
  methods = ["sendMessage", "sendPhoto"]
  rate_limit_per_minute = 30

  [[agents.allowed_skills]]
  skill = "Http"
  methods = ["get"]
  rate_limit_per_minute = 60

  [[agents.allowed_skills]]
  skill = "Storage"
  rate_limit_per_minute = 60
```

## Architecture

Oochy is organized as a Cargo workspace with focused crates:

| Crate | Purpose |
|---|---|
| `oochy-core` | Agent loop, event routing, SQLite state persistence |
| `oochy-llm` | LLM client (Claude API), prompt construction, streaming |
| `oochy-sandbox` | QuickJS VM execution + OS-level isolation (Seatbelt/Landlock) |
| `oochy-channels` | Telegram, Discord, WebSocket channel adapters |
| `oochy-web` | HTTP dashboard and WebSocket endpoint (localhost:3000) |
| `oochy-cli` | Binary entry point, `serve` subcommand, config loading |

### Execution flow

```
User message
  → Channel adapter (Telegram / Discord / WebSocket / stdin)
  → Agent loop (oochy-core)
  → LLM generates JS code (oochy-llm)
  → QuickJS VM executes JS (oochy-sandbox)
  → OS sandbox enforces filesystem/network policy
  → Result returned to channel
```

## Development

```bash
cargo build
cargo test
cargo clippy
```

Run with debug logging:

```bash
RUST_LOG=oochy=debug ./oochy serve
```

## License

MIT
