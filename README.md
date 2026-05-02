# Kitty Monorepo

Kitty is the future monorepo for the KittyPaw product family.

This repository is intended to hold multiple independently deployed services
while centralizing the contracts and cross-service verification that make those
services safe to evolve together.

## Shape

```text
apps/
  kittypaw/          Local CLI, daemon, and local web UI
services/
  api/               Auth, device credentials, discovery, public API proxy
  chat/              Hosted chat UI and daemon relay resource server
  kakao/             KakaoTalk webhook and WebSocket gateway
contracts/           Wire-level schemas, examples, and version policies
testkit/             Cross-service verification helpers and fakes
deploy/              Per-service deployment assets
docs/                Architecture notes, decisions, operations, plans
scripts/             Repository-level helper scripts
```

## Core Rule

The repo boundary is shared. Runtime ownership is not.

- Services are deployed separately.
- Service-owned databases stay private to their service.
- Services do not import another service's internal implementation.
- Shared code is added only when it removes real cross-service duplication.
- Wire contracts, schemas, examples, and E2E fixtures are centralized first.
- Contract changes must run producer and consumer tests together.

## Release Model

Product releases use namespaced tags:

```text
kittypaw/v0.1.0
api/v0.1.0
chat/v0.1.0
kakao/v0.1.0
```

`apps/kittypaw` keeps its own release workflow. The install script must resolve
the latest `kittypaw/v*` release, not the repository-wide latest release.

## Current Status

This directory is a skeleton only. Existing repositories have not been moved
into it yet.
