# Local Auth Chat E2E Design

> Historical plan snapshot. This document records the implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

Date: 2026-05-02

## Goal

Add a repeatable local E2E command that proves the hosted auth/chat path:

1. Portal signs user and device JWTs with RS256.
2. Chat verifies those JWTs via Portal JWKS.
3. A Kittypaw daemon connection registers a local account route.
4. A browser logs in through Chat's BFF flow and receives only an HttpOnly
   session cookie.
5. Browser calls to Chat `/app/api/*` succeed without an `Authorization`
   header because Chat injects the server-side bearer token.
6. A chat completion request is relayed over the daemon websocket.

## Scope

This is not a production deploy smoke and does not contact Google. It uses real
Portal and Chat binaries, real PostgreSQL, real Portal migrations, real JWKS
verification, and the real Kittypaw chat relay connector package. Google OAuth
is replaced by a local fake OAuth server so the flow stays deterministic.

The first version uses the Kittypaw chat relay connector package instead of the
install script. Running the install script would test GitHub releases and shell
packaging, not the auth/chat contract. A later packaging smoke can cover that
separately.

## Architecture

Add a root opt-in command:

```bash
make e2e-local
```

The target calls `scripts/e2e-local.sh`, which starts a disposable PostgreSQL
container from `docker-compose.e2e.yml`, sets `DATABASE_URL`, and runs the Go
E2E harness under `testkit/e2e`.

The harness:

- migrates the Portal test database from `apps/portal/migrations`;
- starts a fake Google OAuth server;
- starts Portal as `go run ./cmd/server` with local OAuth endpoint overrides;
- starts Chat as `go run ./cmd/kittychat` with `KITTYCHAT_JWKS_URL` pointed at
  Portal;
- obtains a user token through Portal's normal Google flow, then pairs a device
  through `/auth/devices/pair`;
- connects a Kittypaw chat relay connector to Chat `/daemon/connect`;
- runs the browser flow through Chat `/auth/login/google`;
- asserts that `/app/api/session`, `/app/api/routes`, and
  `/app/api/nodes/{device}/accounts/{account}/v1/chat/completions` work without
  a browser `Authorization` header.

## Portal Test Seam

Portal needs configurable Google OAuth endpoints. The defaults remain the
production Google URLs. Local E2E sets:

- `GOOGLE_AUTH_URL`
- `GOOGLE_TOKEN_URL`
- `GOOGLE_USERINFO_URL`

These variables are intentionally narrow. They do not alter token signing,
allowed redirect validation, refresh tokens, device pairing, or JWKS.

## Data Flow

The harness uses one fake Google identity for both the daemon pairing path and
the browser BFF login path. Portal `CreateOrUpdate` maps both flows to the same
user, which lets Chat's route discovery return the daemon route for the BFF
session's user token.

Browser requests are made with an HTTP cookie jar. Assertions verify that the
browser-side `/app/api/*` calls do not send `Authorization`; the only credential
sent by the browser is the Chat session cookie.

## Failure Mode

The runner fails fast and prints service logs when startup or the flow fails.
The database DSN must contain `_test`; the harness refuses to migrate/drop any
other database.

## Boundaries

- Default `make smoke-local` remains Docker-free.
- `make e2e-local` is opt-in and may pull/start a Postgres image.
- The E2E harness does not import Portal or Chat internal packages.
- The E2E harness may import public Kittypaw packages needed to exercise the
  real daemon websocket protocol.
