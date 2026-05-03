# Portal Auth and Discovery Split Design

> Historical plan snapshot. This document records the implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

Date: 2026-05-02

## Goal

Make `portal.kittypaw.app` the canonical bootstrap and identity origin, then
extract that responsibility into a separate `apps/portal` deployable.

This is intentionally a breaking migration. `api.kittypaw.app/auth/*`,
`api.kittypaw.app/discovery`, and `api.kittypaw.app/.well-known/jwks.json` are
not compatibility endpoints after the canonical switch.

## Target Domains

| Domain | Role |
| --- | --- |
| `kittypaw.app` | Public website, docs, install page |
| `portal.kittypaw.app` | Discovery, OAuth, token issuance, JWKS, device credentials, future account UI |
| `api.kittypaw.app` | Resource API: weather, air, calendar, geo, almanac |
| `chat.kittypaw.app` | Hosted chat, OpenAI-compatible relay, daemon WebSocket relay |
| `kakao.kittypaw.app` | Kakao OpenBuilder webhook and Kakao-specific WebSocket bridge |

## Phase 1: Canonical Portal Origin

`apps/kittyapi` still hosts the code temporarily, but all public identity
contracts move to the portal origin:

- JWT issuer: `https://portal.kittypaw.app/auth`
- JWKS URL: `https://portal.kittypaw.app/.well-known/jwks.json`
- auth base URL: `https://portal.kittypaw.app/auth`
- discovery URL: `https://portal.kittypaw.app/discovery`
- resource API audience remains `https://api.kittypaw.app`
- chat audience remains `https://chat.kittypaw.app`

Deployment must expose auth/discovery/JWKS only through
`portal.kittypaw.app`. The `api.kittypaw.app` virtual host should serve only
resource API routes and health.

## Phase 2: `apps/portal`

Create a separate Go service under `apps/portal` that owns:

- OAuth login and callbacks
- CLI and hosted-web auth code exchange
- token refresh
- device pair/list/delete/refresh
- JWKS publication
- `/discovery`
- user, refresh token, and device tables

After extraction, `apps/kittyapi` becomes a resource server. It should verify
portal-issued JWTs for authenticated rate-limit or future user-scoped resource
behavior, but it must not own token issuance or auth database tables.

## Data Ownership

Initial extraction may use the same PostgreSQL server, but ownership changes
logically before physical separation:

- `apps/portal` owns users, refresh tokens, devices, and identity keys.
- `apps/kittyapi` owns resource data such as places and addresses.

Physical DB separation can follow once deployment is stable.

## Tests

Phase 1 must update producer and consumer contract tests:

- API token issuance must emit the portal issuer.
- Chat JWT/JWKS verifier must expect the portal issuer and portal JWKS URL.
- Kittypaw discovery consumer must expect portal `auth_base_url`.
- Discovery fixtures must advertise portal auth and API resource separately.

Phase 2 must keep these tests green while moving code ownership.

