# KittyPaw

Experimental Go framework for local AI agents. Single binary, goja JS sandbox, 5 channel adapters, skill registry. **v0.0.8 alpha** — honest status over polish.

## Status

- ✅ **Working** — CLI + daemon, registry install, sandbox + permission, 5 channel adapters (Telegram/Slack/Discord/Kakao/WS)
- 🚧 **Partial** — Reflection candidate surface (verified), `skill create` syntax (5/5 measured), Web search source quality
- 🔬 **Experimental** — Family account, MoA, live workspace indexing
- ❌ **Not / retired** — Windows GUI signing, "learns the more you use it" auto-adaptation, self-healing (retired)

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/jinto/kittypaw/main/install.sh | sh
```

## Quick Start

```bash
kittypaw setup                            # interactive setup (LLM, channels); auto-enters chat on TTY — pass --no-chat to skip
kittypaw skill install weather-briefing   # install a skill from registry
kittypaw chat "오늘 날씨 알려줘"            # one-shot chat (auto-starts daemon)
```

Inspect what you get: a local daemon, one installed skill, an LLM-backed chat. Skill runtime behaviour depends on its package, the configured APIs, and the LLM provider.

```bash
kittypaw chat          # interactive REPL mode
kittypaw serve         # start as HTTP/WebSocket server
```

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

TOML config at `~/.kittypaw/config.toml`.

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
