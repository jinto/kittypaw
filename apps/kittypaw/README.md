# KittyPaw

AI agent framework with multi-channel messaging, JavaScript sandbox, and skill learning.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/jinto/kittypaw/main/install.sh | sh
```

## Quick Start

```bash
kittypaw init          # interactive setup (LLM, Telegram, workspace)
kittypaw serve         # start the server
kittypaw chat          # interactive chat (auto-starts daemon)
```

## Skills

```bash
kittypaw install https://github.com/owner/repo   # install from GitHub
kittypaw install /path/to/local/skill             # install from local directory
kittypaw search <keyword>                         # search skill registry
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
go build ./cmd/kittypaw
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
