# KittyPaw

Experimental Go framework for local AI agents. Single binary, goja JS sandbox,
5 channel adapters, skill registry. **Alpha** — honest status over polish.

## Status

- ✅ **Working** — CLI + local server, registry install, sandbox + permission, 5 channel adapters (Telegram/Slack/Discord/Kakao/WS)
- ✅ **Working** — Telegram/Kakao inbound media metadata; attached images are available to the agent through `Vision.analyzeAttachment(...)`
- 🚧 **Partial** — Reflection candidate surface (verified), `skill create` syntax (5/5 measured), Web search source quality
- 🔬 **Experimental** — Family account, MoA, live workspace indexing
- ❌ **Not / retired** — Windows GUI signing, "learns the more you use it" auto-adaptation, self-healing (retired)

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/kittypaw-app/kitty/main/install-kittypaw.sh | sh
```

Without `VERSION`, the installer follows `apps/kittypaw/stable.json`, not the
newest GitHub release. Use `VERSION` to install a specific candidate release
for testing:

```bash
VERSION=0.4.9 curl -fsSL https://raw.githubusercontent.com/kittypaw-app/kitty/main/install-kittypaw.sh | sh
```

Installer overrides for local forks or nonstandard install locations:

```bash
KITTYPAW_INSTALL_REPO=owner/repo \
INSTALL_DIR="$HOME/.local/bin" \
curl -fsSL https://raw.githubusercontent.com/kittypaw-app/kitty/main/install-kittypaw.sh | sh

KITTYPAW_INSTALL_SCRIPT_URL=https://example.com/install-kittypaw.sh \
curl -fsSL https://raw.githubusercontent.com/kittypaw-app/kitty/main/install-kittypaw.sh | sh
```

## Quick Start

```bash
kittypaw setup --account alice            # interactive setup (account login, LLM, channels); auto-enters chat on TTY
kittypaw skill install weather-briefing   # install a skill from registry
kittypaw chat "오늘 날씨 알려줘"            # one-shot chat (auto-starts local server)
```

Inspect what you get: a local server, one installed skill, an LLM-backed chat. Skill runtime behaviour depends on its package, the configured APIs, and the LLM provider.

```bash
kittypaw chat          # interactive REPL mode
kittypaw server start  # start as HTTP/WebSocket server
```

## In-Chat Commands

These commands are entered inside `kittypaw chat`, Telegram, Kakao, or another
connected chat channel:

```text
/help                 show command help
/status               show today's local execution stats
/skills               list local user-created skills
/run <name>           run an installed skill or package by id/name
/teach <description>  create and save a draft skill from chat
/persona <profile-id> set the default assistant profile for this account
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

Operational environment variables:

| Variable | Purpose |
|---|---|
| `KITTYPAW_ACCOUNT` | Select the local account for CLI commands |
| `KITTYPAW_TELEGRAM_BOT_TOKEN` | Seed `kittypaw account add` / setup with a Telegram bot token |
| `KITTYPAW_ALLOW_INSECURE_REGISTRY=1` | Test/local override that permits non-HTTPS skill registries |
| `INSTALL_DIR` | Install destination for `apps/kittypaw/install-kittypaw.sh` |
| `KITTYPAW_INSTALL_REPO` | Root installer repository override, e.g. `owner/repo` |
| `KITTYPAW_INSTALL_SCRIPT_URL` | Root installer script URL override |
| `KITTYPAW_CHANNEL=latest` | Installer override that follows the newest GitHub release instead of stable |
| `VERSION` | Installer override for a specific release, e.g. `0.4.9` |

## Build from Source

```bash
make build    # Build binary
make test     # Run tests
make lint     # Lint (requires golangci-lint)
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development workflow and conventions.

## Release

KittyPaw releases are built from the monorepo root workflow
`.github/workflows/release-kittypaw.yml`. Product tags are namespaced:

```bash
git tag kittypaw/vX.Y.Z
git push origin kittypaw/vX.Y.Z
```

The workflow builds archives directly with `go build`, signs and notarizes the
macOS binaries, and updates release checksums. Do not use plain `vX.Y.Z` tags
for monorepo product releases.

Binary releases are candidates until `apps/kittypaw/stable.json` is manually
promoted. The default install command follows stable; use `VERSION=X.Y.Z` to
test a candidate before promotion.

## Stop / Uninstall

```bash
kittypaw server stop            # stop the running server
```

```bash
kittypaw server stop
rm /usr/local/bin/kittypaw      # remove binary
rm -rf ~/.kittypaw              # remove config and data
```

## License

[Elastic License 2.0](LICENSE)
