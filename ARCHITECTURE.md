# Architecture

Kitty is a multi-service monorepo. It centralizes development context and
cross-service verification, but it does not collapse services into one runtime.

## Components

### `apps/kittypaw`

The local product surface:

- CLI
- local daemon
- local browser UI
- local account config and local store
- skill engine and execution sandbox
- channel adapters
- outbound connectors to hosted services

This remains a modular local monolith and should normally be released as one
KittyPaw binary.

### `apps/kittyapi`

The cloud resource API surface:

- public data proxy endpoints
- API resource audience: `https://api.kittypaw.app`
- anonymous IP rate limiting for upstream protection

Identity, discovery, and JWKS routes are intentionally not served by this app.

### `apps/portal`

The identity and bootstrap surface:

- OAuth login
- JWT and refresh token issuance
- device pair, refresh, list, revoke
- JWKS publication
- service discovery
- future account and device UI

Canonical issuer: `https://portal.kittypaw.app/auth`.

### `apps/chat`

The hosted chat resource server:

- hosted chat static app
- daemon outbound WebSocket endpoint
- route discovery
- OpenAI-compatible relay endpoints
- JWT/JWKS verification for API and device credentials

The service forwards a narrow chat surface. It must not become a generic
localhost tunnel.

### `apps/kakao`

The Kakao gateway:

- Kakao OpenBuilder webhook
- Kakao callback dispatch
- pairing code flow for Kakao users
- WebSocket bridge to local Kittypaw
- Kakao-specific rate limit and kill switch

This stays separate because vendor webhook behavior, callback security, and
operational controls differ from the generic chat relay.

## Contract Ownership

Contracts are wire-level agreements between deployable units. They include:

- HTTP request and response shapes
- JWT claim shape, issuer, audience, scopes, and version policy
- WebSocket frame shapes
- operation names
- example payloads used by producer and consumer tests

Internal language interfaces are not contracts unless they cross a process or
service boundary.

## Dependency Direction

```text
apps
  -> contracts
  -> testkit
```

Apps may depend on contracts and testkit. Apps must not depend on other apps'
internal packages.

## Deployment

Each app remains independently deployed or released:

- `apps/kittypaw`: GitHub release assets and install script
- `apps/kittyapi`: service binary and API database migrations
- `apps/portal`: identity service binary and auth database migrations
- `apps/chat`: service binary and hosted chat static assets
- `apps/kakao`: service binary and Kakao gateway data store

Hosted services expose `/health` with build identity (`status`, `version`, and
`commit`) so deployment smoke can verify the running binary immediately after a
restart. Current nginx/systemd deployment favors Unix socket binding for hosted
Go services where supported, with nginx proxying public HTTPS traffic to the
socket instead of a public TCP port.

Databases remain owned by their service. The first portal split keeps the
existing production database physical layout; identity migrations are copied to
`apps/portal`, while `apps/kittyapi` keeps historical migrations until the DB
cutover is planned separately.
