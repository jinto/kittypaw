# Release Tags

Use namespaced tags for product releases:

```text
kittypaw/v0.1.0
api/v0.1.0
chat/v0.1.0
kakao/v0.1.0
```

## Kittypaw Install Script

The Kittypaw install script must not call the repository-wide latest release.
In a monorepo, the latest repository release might belong to another service.

The script should list releases and select the newest tag that starts with
`kittypaw/v`.

Kittypaw releases are published from `kittypaw-app/kittypaw` and built by
`.github/workflows/release-kittypaw.yml`. The workflow intentionally builds
archives directly with `go build` instead of GoReleaser because prefixed
monorepo tags such as `kittypaw/v0.4.0` require GoReleaser Pro's monorepo
support. The workflow signs and notarizes the macOS archives after upload, then
regenerates `checksums.txt` so the installer checks the signed artifacts.

## GitHub Actions

Each release workflow should filter by tag prefix:

```yaml
on:
  push:
    tags:
      - "kittypaw/v*"
```

The workflow working directory should be the product directory, for example:

```yaml
defaults:
  run:
    working-directory: apps/kittypaw
```
