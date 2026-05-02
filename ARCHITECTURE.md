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

### `services/api`

The cloud resource API surface:

- public data proxy endpoints
- API resource audience: `https://api.kittypaw.app`
- transition host for identity implementation until `services/portal` is
  extracted

During the phase-1 portal split, the binary can still contain identity code,
but public identity routes are served only on `portal.kittypaw.app`.

### `services/portal`

The identity and bootstrap surface. In phase 1 this is a logical service hosted
by the API binary; phase 2 extracts it into its own deployable:

- OAuth login
- JWT and refresh token issuance
- device pair, refresh, list, revoke
- JWKS publication
- service discovery
- future account and device UI

Canonical issuer: `https://portal.kittypaw.app/auth`.

### `services/chat`

The hosted chat resource server:

- hosted chat static app
- daemon outbound WebSocket endpoint
- route discovery
- OpenAI-compatible relay endpoints
- JWT/JWKS verification for API and device credentials

The service forwards a narrow chat surface. It must not become a generic
localhost tunnel.

### `services/kakao`

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
services and apps
  -> contracts
  -> testkit
```

Services may depend on contracts and testkit. Services must not depend on other
services' internal packages.

## Deployment

Each service remains independently deployed:

- `apps/kittypaw`: GitHub release assets and install script
- `services/api`: service binary and API database migrations
- `services/portal`: identity service binary and auth database migrations
  after extraction
- `services/chat`: service binary and hosted chat static assets
- `services/kakao`: service binary and Kakao gateway data store

Databases remain owned by their service.
