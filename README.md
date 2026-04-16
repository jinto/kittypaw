# KittyPaw

AI agent framework with multi-channel messaging, JavaScript sandbox, and skill learning.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/jinto/kittypaw/main/install.sh | sh
```

## Quick Start

```bash
kittypaw setup                            # interactive setup (LLM, channels)
kittypaw skill install weather-briefing   # install a skill from registry
kittypaw chat "오늘 날씨 알려줘"            # one-shot chat (auto-starts daemon)
```

That's it — Korean weather briefing in 3 commands. The skill reads your locale and location from config, fetches live forecast data, and summarizes it in your language.

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
kittypaw teach                                    # create a skill from conversation
```

## Config

TOML config at `~/.kittypaw/config.toml`.

```toml
[registry]
url = "https://raw.githubusercontent.com/kittypaw/skills/main"
```

## Build from Source

```bash
go build -o kittypaw ./cli
go test ./...
```

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

[Elastic License 2.0](https://www.elastic.co/licensing/elastic-license)
