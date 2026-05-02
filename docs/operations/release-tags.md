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
