# Oochy

**Program your bot by talking to it.**

Oochy is a single-binary AI agent where you teach new skills through conversation. Describe what you want in natural language, Oochy writes the code, runs it in a sandbox, and if you approve, it runs forever.

No workflow canvas. No drag-and-drop. Just conversation.

## Quickstart

```bash
# Install
curl -fsSL https://raw.githubusercontent.com/jinto/oochy/main/install.sh | bash

# Set up
oochy init

# Teach your first skill
oochy teach "every morning, summarize the top 5 Hacker News stories and send them to my Telegram"

# Or teach from Telegram
# Send /teach followed by your description to your bot
```

## How It Works

```
You: "send me a daily joke every morning at 9am"

Oochy:
  1. LLM generates JavaScript code for the task
  2. Code runs in a dual-layer sandbox (QuickJS VM + OS kernel isolation)
  3. You see the code, the dry-run output, and the permissions it needs
  4. You approve → skill saved to .oochy/skills/joke-every-morning.js
  5. Next morning at 9am, the skill runs automatically
```

## Features

- **Conversational programming** — describe what you want, Oochy writes the code
- **Teach loop** — `/teach` on Telegram or `oochy teach` from CLI
- **Dual-layer sandbox** — QuickJS VM isolation + macOS Seatbelt / Linux Landlock kernel isolation
- **5 primitives** — Telegram, Http, Storage, Llm, Schedule
- **Skill versioning** — skills saved as `.oochy/skills/*.skill.toml` + `*.js`, git-friendly
- **Schedule evaluator** — cron-based scheduling with auto-disable on failure
- **Per-skill storage** — each skill gets isolated key-value storage (SQLite)
- **Single binary, zero dependencies** — just `./oochy` and an API key

## Demo Examples

### Daily HN Digest
```bash
oochy teach "every morning at 9am, fetch the top 5 stories from Hacker News and send a summary to my Telegram"
```

### RSS Watcher
```bash
oochy teach "check the Rust blog RSS feed every hour and notify me on Telegram when there's a new post"
```

### GitHub Issue Notifier
```bash
oochy teach "when I say /issues, check my GitHub repo for new issues and list them"
```

## CLI Reference

```bash
oochy init                          # Set up API key and config
oochy teach <description>           # Teach a new skill (interactive)
oochy serve                         # Start bot server (Telegram + scheduler)
oochy run <name>                    # Run a skill manually
oochy run <name> --dry-run          # Dry-run a skill (no side effects)
oochy skills list                   # List all taught skills
oochy skills explain <name>         # LLM explains what a skill does
oochy skills disable <name>         # Disable a skill
oochy skills delete <name>          # Delete a skill
oochy skills import <path>          # Import skills from a directory
oochy config check                  # Validate configuration
```

## Architecture

Oochy is organized as a Cargo workspace with focused crates:

| Crate | Purpose |
|---|---|
| `oochy-core` | Skill file system, config, types, capability checking |
| `oochy-llm` | LLM client (Claude API), prompt construction |
| `oochy-sandbox` | QuickJS VM execution + OS-level isolation (Seatbelt/Landlock) |
| `oochy-channels` | Telegram channel adapter |
| `oochy-cli` | Binary entry point, teach loop, schedule evaluator, skill executor |

### Execution Flow

```
User message or /teach command
  → Channel adapter (Telegram / CLI / stdin)
  → Skill trigger matching (taught skills checked first)
  → If /teach: LLM generates JS → sandbox dry-run → approve → save to .oochy/skills/
  → If skill match: load JS from .oochy/skills/ → execute in sandbox
  → If no match + freeform enabled: LLM generates one-shot JS → execute
  → OS sandbox enforces filesystem/network policy
  → Result returned to channel
```

## Configuration

Copy `oochy.toml.example` to `oochy.toml` or run `oochy init`:

```toml
[llm]
provider = "claude"
api_key = ""          # or set OOCHY_API_KEY env var
model = "claude-sonnet-4-20250514"
max_tokens = 4096

[sandbox]
timeout_secs = 30
memory_limit_mb = 64
allowed_hosts = ["api.telegram.org"]

# Restrict /teach to specific Telegram users (empty = allow all)
admin_chat_ids = []

# Allow freeform LLM responses when no skill matches (default: false)
freeform_fallback = false

# Telegram channel
[[channels]]
channel_type = "telegram"
token = "123456:ABC-your-bot-token"
```

## 5 Primitives

Skills have access to 5 built-in primitives:

| Primitive | Methods | Description |
|-----------|---------|-------------|
| `Telegram` | `sendMessage`, `sendPhoto`, `editMessage` | Send messages via Telegram |
| `Http` | `get`, `post`, `put`, `delete` | HTTP requests (SSRF-protected) |
| `Storage` | `get`, `set`, `delete`, `list` | Per-skill key-value storage (SQLite) |
| `Llm` | `generate` | Call Claude API (max 3 per execution) |
| `Schedule` | (declarative) | Cron-based scheduling via skill TOML |

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
