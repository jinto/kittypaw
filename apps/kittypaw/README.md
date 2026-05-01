# KittyPaw

Experimental Go framework for local AI agents. Single binary, goja JS sandbox, 5 channel adapters, skill registry. **v0.0.8 alpha** — honest status over polish.

## Status

- ✅ **Working** — CLI + daemon, registry install, sandbox + permission, 5 channel adapters (Telegram/Slack/Discord/Kakao/WS)
- 🚧 **Partial** — Reflection candidate surface (verified), `skill create` syntax (5/5 measured), Web search source quality
- 🔬 **Experimental** — Family account, MoA, live workspace indexing
- ❌ **Not / retired** — Windows GUI signing, "learns the more you use it" auto-adaptation, self-healing (retired)

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/kittypaw-app/kittypaw/main/install.sh | sh
```

## Quick Start

```bash
kittypaw setup --account alice            # interactive setup (account login, LLM, channels); auto-enters chat on TTY
kittypaw skill install weather-briefing   # install a skill from registry
kittypaw chat "오늘 날씨 알려줘"            # one-shot chat (auto-starts daemon)
```

Inspect what you get: a local daemon, one installed skill, an LLM-backed chat. Skill runtime behaviour depends on its package, the configured APIs, and the LLM provider.

```bash
kittypaw chat          # interactive REPL mode
kittypaw serve         # start as HTTP/WebSocket server
```

## Accounts

Fresh installs create named local accounts under `~/.kittypaw/accounts/<accountID>/`.
The legacy `~/.kittypaw/accounts/default/` layout still works for upgraded installs.

```bash
printf '%s\n' "$LOCAL_WEB_PASSWORD" | kittypaw setup --account alice --password-stdin
printf '%s\n' "$BOB_WEB_PASSWORD" | kittypaw account add bob --password-stdin

KITTYPAW_ACCOUNT=bob kittypaw chat
kittypaw chat --account bob
```

If multiple accounts exist, CLI commands that read or write account config require
`--account <id>` or `KITTYPAW_ACCOUNT=<id>`. The local Web UI requires login once
`~/.kittypaw/auth.json` has local users; each account has its own Web UI password.

## Skills

```bash
kittypaw skill install weather-briefing           # install from registry
kittypaw skill install https://github.com/owner/repo   # install from GitHub
kittypaw skill install /path/to/local/skill       # install from local directory
kittypaw skill search <keyword>                   # search skill registry
kittypaw skill list                               # list installed skills
kittypaw skill create <description>               # generate a draft skill from natural language
```

## Config

Account TOML config lives at `~/.kittypaw/accounts/<accountID>/config.toml`.
Server-wide settings live at `~/.kittypaw/server.toml`, and local Web UI login
metadata lives at `~/.kittypaw/auth.json`.

```toml
[registry]
url = "https://raw.githubusercontent.com/kittypaw-app/skills/main"
```

## Build from Source

```bash
make build    # Build binary
make test     # Run tests
make lint     # Lint (requires golangci-lint)
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development workflow and conventions.

## Release

Tag-triggered CI via GoReleaser + GitHub Actions.

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Stop / Uninstall

```bash
kittypaw stop                   # stop the running server
```

```bash
kittypaw stop
rm /usr/local/bin/kittypaw      # remove binary
rm -rf ~/.kittypaw              # remove config and data
```

## License

[Elastic License 2.0](LICENSE)
