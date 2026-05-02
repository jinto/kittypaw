# Agent Guide

This repo is a Go-first monorepo skeleton for KittyPaw services.

## Operating Principles

- Read local docs before making structural changes.
- Keep service boundaries explicit.
- Do not move existing repositories into this tree without an explicit migration
  plan.
- Do not introduce shared runtime packages just because code looks similar.
- Prefer contract fixtures and cross-service tests over implicit shared types.
- Never let one service read another service's database directly.

## Directory Rules

- `apps/kittypaw` owns the local CLI, daemon, local web UI, engine, local store,
  and local channel adapters.
- `services/api` owns cloud auth, users, devices, discovery, JWKS, and public API
  proxy endpoints.
- `services/chat` owns hosted chat, route discovery, OpenAI-compatible relay
  endpoints, and daemon outbound WebSocket relay.
- `services/kakao` owns Kakao OpenBuilder webhook, Kakao callback dispatch, and
  Kakao-specific pairing.
- `contracts` owns wire-level schemas and examples. It is the first place to
  update when a producer/consumer contract changes.
- `testkit` owns reusable fake services and credentials for cross-service tests.

## Contract Change Checklist

When changing anything under `contracts/`:

1. Update the schema or enum source.
2. Update at least one example fixture.
3. Add or update producer tests in the owning service.
4. Add or update consumer tests in every affected service.
5. Run the root contract verification target once it exists.

## Release Tags

Use namespaced tags:

- `kittypaw/vX.Y.Z`
- `api/vX.Y.Z`
- `chat/vX.Y.Z`
- `kakao/vX.Y.Z`

Do not use a plain `vX.Y.Z` tag for product releases in this monorepo.
