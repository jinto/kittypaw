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
- Keep KittyChat independent of login/session creation.
- Prepare for future API server integration through a store/verifier interface.

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

Both credential classes resolve to principals:

- API credentials resolve to `openai.Principal`:
  `user_id`, `device_id`, `account_id`.
- Device credentials resolve to `broker.DevicePrincipal`:
  `user_id`, `device_id`, `local_account_ids`.

The MVP env variables seed one API credential and one device credential into an
in-memory store. Later, the same interfaces can be backed by API-server-issued
JWT/JWKS verification, API-key introspection, or Postgres-backed records.

## Components

### `internal/identity`

Add a small identity package with:

- `Store` interface for resolving API and device tokens.
- `MemoryStore` implementation seeded at process startup.
- Token hashing is not implemented in this slice because the MVP keeps parity
  with the existing static-token behavior. The interface must not leak
  that implementation detail.

Suggested interface:

```go
type Store interface {
    ResolveAPIClient(ctx context.Context, token string) (openai.Principal, error)
    ResolveDevice(ctx context.Context, token string) (broker.DevicePrincipal, error)
}
```

The package defines `ErrUnauthorized` for missing or unknown credentials.

### Store-backed Authenticators

Replace direct static token matching in runtime wiring with store-backed
authenticators:

- OpenAI/client authenticator reads `Authorization: Bearer ...` or `x-api-key`
  and resolves the token through `identity.Store`.
- Daemon authenticator reads `Authorization: Bearer ...` or `x-device-token` and
  resolves the token through `identity.Store`.

Existing static authenticators remain for focused package tests. The main server
wiring uses the identity store.

### Config Seeding

Keep the current environment variables:

- `KITTYCHAT_API_TOKEN`
- `KITTYCHAT_DEVICE_TOKEN`
- `KITTYCHAT_USER_ID`
- `KITTYCHAT_DEVICE_ID`
- `KITTYCHAT_LOCAL_ACCOUNT_ID`

`cmd/kittychat` seeds an in-memory store from this config and passes
store-backed authenticators to the handlers. This keeps local development and
existing tests working while moving the production boundary into one place.

## Request Flow

API client request:

1. Client calls `/nodes/{device_id}/v1/chat/completions` or
   `/nodes/{device_id}/v1/models` with an API-server-issued credential.
2. KittyChat extracts the token.
3. Identity store resolves it to `openai.Principal`.
4. OpenAI handler verifies the URL `device_id` equals the credential's allowed
   `device_id`.
5. Broker verifies the daemon is online, the user matches, and the account is
   allowed by the daemon's registered principal.
6. Request is relayed to the daemon.

Daemon connection:

1. Daemon connects to `/daemon/connect` with a device credential.
2. Identity store resolves it to `broker.DevicePrincipal`.
3. Daemon sends `hello` with its `device_id` and local accounts.
4. Handler rejects the connection if `hello` does not match the credential.
5. Broker registers the daemon connection for relay traffic.

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

- `MemoryStore` resolves seeded API tokens.
- `MemoryStore` resolves seeded device tokens.
- Unknown tokens return `identity.ErrUnauthorized`.
- Store-backed OpenAI authenticator accepts bearer and `x-api-key`.
- Store-backed daemon authenticator accepts bearer and `x-device-token`.
- `cmd/kittychat` builds a router using the identity store seeded from config.

Keep the existing end-to-end fake daemon test passing unchanged at the behavior
level.

## Future Extensions

- API server JWT/JWKS verification for browser/session-backed web chat.
- API key introspection or synced API-key records for OpenAI-compatible clients.
- Device credential issuance and rotation from KittyPaw API.
- Pairing code flow for new daemon registration.
- Postgres-backed account/device authorization.
