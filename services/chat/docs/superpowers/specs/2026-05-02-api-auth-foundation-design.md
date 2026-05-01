# API Auth Foundation Design

## Context

KittyChat currently accepts static environment tokens for one API client and one
daemon. That is enough for the relay MVP, but production access must be gated by
KittyPaw's API server. KittyChat must not become a separate login, session, or
account authority.

The API server is the source of truth for users, sessions, API keys, devices,
pairing, and account permissions. KittyChat is a resource server: it verifies
API-server-issued credentials, maps them to allowed devices and local accounts,
and only then relays chat traffic to a daemon.

## Goals

- Introduce an auth/identity boundary that can represent API clients and daemon
  credentials without hard-coding one global token.
- Preserve current MVP behavior through an environment-seeded in-memory store.
- Make authorization explicit: an API client can only relay to the device and
  local account assigned by its credential.
- Treat the daemon WebSocket protocol as a versioned contract with operations
  and capabilities, not only HTTP paths.
- Keep KittyChat independent of login/session creation.
- Prepare for future API server integration through a credential verifier
  interface with explicit claims.

## Non-Goals

- Do not add Postgres in this slice.
- Do not implement pairing codes in this slice.
- Do not implement API server HTTP introspection in this slice.
- Do not add a web chat UI in this slice.
- Do not broaden relayable paths beyond the existing OpenAI-compatible surface.

## Credential Model

There are two credential classes:

- API client credential: used by web chat and OpenAI-compatible clients when
  calling `/nodes/{device_id}/v1/models` or
  `/nodes/{device_id}/v1/chat/completions`.
- Device credential: used by a local KittyPaw daemon when opening
  `/daemon/connect`.

Both credential classes first verify into API-server-shaped claims, then convert
to the package-specific principals used by the relay:

- API credentials verify to `APIClientClaims` and convert to
  `openai.Principal`.
- Device credentials verify to `DeviceClaims` and convert to
  `broker.DevicePrincipal`.

Claims include the fields that `api.kittypaw.app` will own:

- `audiences`: must include `kittychat`. API server JWTs can use
  `["kittyapi", "kittychat"]` for backwards-compatible multi-audience tokens.
- `version`: must be `1` for this slice.
- `scopes`: must contain known scope strings.
- API client claims: `subject`, `user_id`, `device_id`, `account_id`.
- Device claims: `subject`, `user_id`, `device_id`, `local_account_ids`.

The initial scope vocabulary is:

- `chat:relay`
- `models:read`
- `daemon:connect`

The MVP env variables seed one API credential and one device credential into an
in-memory store. Later, the same interfaces can be backed by API-server-issued
JWT/JWKS verification, API-key introspection, or Postgres-backed records.

## Components

### `internal/identity`

Add a small identity package with:

- `CredentialVerifier` interface for verifying API and device credentials.
- `APIClientClaims` and `DeviceClaims` types that mirror the agreed API-server
  credential contract.
- Scope constants for `chat:relay`, `models:read`, and `daemon:connect`.
- `MemoryCredentialVerifier` implementation seeded at process startup.
- Token hashing is not implemented in this slice because the MVP keeps parity
  with the existing static-token behavior. The interface must not leak
  that implementation detail.

Suggested interface:

```go
type CredentialVerifier interface {
    VerifyAPIClient(ctx context.Context, token string) (APIClientClaims, error)
    VerifyDevice(ctx context.Context, token string) (DeviceClaims, error)
}
```

The package defines `ErrUnauthorized` for missing or unknown credentials.

### Verifier-backed Authenticators

Replace direct static token matching in runtime wiring with store-backed
authenticators:

- OpenAI/client authenticator reads `Authorization: Bearer ...` or `x-api-key`
  and verifies the token through `identity.CredentialVerifier`.
- Daemon authenticator reads `Authorization: Bearer ...` or `x-device-token` and
  verifies the token through `identity.CredentialVerifier`.

Existing static authenticators remain for focused package tests. The main server
wiring uses the identity verifier.

### Wire Protocol v1

The server-to-daemon request frame uses `operation` as the stable contract:

- `openai.models`
- `openai.chat_completions`

HTTP compatibility fields can still be included in request frames:

- `openai.models` maps to `GET /v1/models`.
- `openai.chat_completions` maps to `POST /v1/chat/completions`.

The daemon hello frame declares version and capability support:

```json
{
  "type": "hello",
  "device_id": "dev_1",
  "local_accounts": ["alice"],
  "daemon_version": "0.1.0",
  "protocol_version": "1",
  "capabilities": ["openai.models", "openai.chat_completions"]
}
```

Unknown operations are rejected by frame validation. Unknown capabilities are
rejected in this MVP so incompatible daemon builds fail fast.

### Config Seeding

Keep the current environment variables:

- `KITTYCHAT_API_TOKEN`
- `KITTYCHAT_DEVICE_TOKEN`
- `KITTYCHAT_USER_ID`
- `KITTYCHAT_DEVICE_ID`
- `KITTYCHAT_LOCAL_ACCOUNT_ID`

`cmd/kittychat` seeds an in-memory verifier from this config and passes
store-backed authenticators to the handlers. This keeps local development and
existing tests working while moving the production boundary into one place.

The env-seeded API client claims use:

- `audiences`: `["kittychat"]`
- `version`: `1`
- `scopes`: `chat:relay`, `models:read`

The env-seeded device claims use:

- `audiences`: `["kittychat"]`
- `version`: `1`
- `scopes`: `daemon:connect`

## Request Flow

API client request:

1. Client calls `/nodes/{device_id}/v1/chat/completions` or
   `/nodes/{device_id}/v1/models` with an API-server-issued credential.
2. KittyChat extracts the token.
3. Identity verifier verifies it to `APIClientClaims`.
4. API claims convert to `openai.Principal`.
5. OpenAI handler verifies the URL `device_id` equals the credential's allowed
   `device_id`.
6. Broker verifies the daemon is online, the user matches, and the account is
   allowed by the daemon's registered principal.
7. Request is relayed to the daemon.

Daemon connection:

1. Daemon connects to `/daemon/connect` with a device credential.
2. Identity verifier verifies it to `DeviceClaims`.
3. Device claims convert to `broker.DevicePrincipal`.
4. Daemon sends `hello` with its `device_id`, local accounts,
   `daemon_version`, `protocol_version`, and capabilities.
5. Handler rejects the connection if `hello` does not match the credential.
6. Broker registers the daemon connection for relay traffic.

## Error Handling

- Missing or invalid credentials return `401`.
- A valid credential for a different device returns `403`.
- A valid request for an offline daemon returns `503`.
- Backpressure remains `429`.
- Relay/protocol failures remain `502`.

Responses keep the current JSON error shape for OpenAI-compatible clients in
this slice.

## Testing

Add focused tests for:

- `MemoryCredentialVerifier` verifies seeded API tokens into claims.
- `MemoryCredentialVerifier` verifies seeded device tokens into claims.
- Unknown tokens return `identity.ErrUnauthorized`.
- Invalid audience, version, scope, or required identity fields are rejected.
- Verifier-backed OpenAI authenticator accepts bearer and `x-api-key`.
- Verifier-backed daemon authenticator accepts bearer and `x-device-token`.
- `cmd/kittychat` builds a router using the identity verifier seeded from
  config.
- Protocol validation accepts the hello v1 shape and rejects unknown operations
  or capabilities.
- OpenAI-compatible HTTP routes send daemon request frames with stable
  `operation` values.

Keep the existing end-to-end fake daemon test passing unchanged at the behavior
level.

## Future Extensions

- API server JWT/JWKS verification for browser/session-backed web chat.
- API key introspection or synced API-key records for OpenAI-compatible clients.
- Device credential issuance and rotation from KittyPaw API.
- Pairing code flow for new daemon registration.
- Postgres-backed account/device authorization.
