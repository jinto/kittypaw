# KittyChat

KittyChat is the chat relay for local KittyPaw daemons. It lets web chat and
OpenAI-compatible clients reach a local KittyPaw daemon through an outbound
WebSocket connection, without exposing the daemon's control UI or requiring
Tailscale/port forwarding.

## MVP Scope

This repository currently contains the relay core:

- `GET /health` with version and commit hash
- `GET /daemon/connect` for local daemon outbound WebSocket connections
- `GET /v1/routes` for authenticated online daemon/account discovery
- `GET /nodes/{device_id}/accounts/{account_id}/v1/models`
- `POST /nodes/{device_id}/accounts/{account_id}/v1/chat/completions`
- legacy MVP fallback routes without `{account_id}`:
  - `GET /nodes/{device_id}/v1/models`
  - `POST /nodes/{device_id}/v1/chat/completions`
- JSON relay frames over WebSocket
- in-memory single-instance device broker
- Portal-issued JWT verifier for web chat, OpenAI-compatible clients, and daemon
  device credentials
- env-seeded static credential fallback for MVP/manual testing
- operation-based daemon protocol v1 for OpenAI-compatible relay requests

The relay is application-level. It only forwards the narrow chat/OpenAI-compatible
surface and is not a generic localhost tunnel.

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `KITTYCHAT_JWKS_URL` | unset | Portal JWKS endpoint for RS256 access tokens and daemon device credentials |
| `KITTYCHAT_JWT_SECRET` | unset | Legacy HS256 shared-secret fallback. Falls back to `JWT_SECRET` when unset |
| `KITTYCHAT_API_TOKEN` | required when no JWT verifier is configured | Static MVP bearer token fallback for web chat and OpenAI-compatible client requests |
| `KITTYCHAT_DEVICE_TOKEN` | required when no JWT verifier is configured | Static MVP bearer token fallback for daemon WebSocket connections |
| `KITTYCHAT_USER_ID` | required for static token fallback | MVP cloud user id |
| `KITTYCHAT_DEVICE_ID` | required for static token fallback | MVP device id |
| `KITTYCHAT_LOCAL_ACCOUNT_ID` | required for static token fallback | Local KittyPaw account id routed through this device |
| `KITTYCHAT_BIND_ADDR` | `:$PORT` or `:8080` | TCP bind address or Unix socket path |
| `PORT` | `8080` | Port fallback when `KITTYCHAT_BIND_ADDR` is unset |
| `KITTYCHAT_PUBLIC_BASE_URL` | `https://chat.kittypaw.app` | Public Chat origin used in route metadata |
| `KITTYCHAT_API_AUTH_BASE_URL` | `https://portal.kittypaw.app/auth` | Portal auth base URL used by OpenAI-compatible clients |
| `KITTYCHAT_VERSION` | `dev` | Version string returned by `/health` |
| `KITTYCHAT_COMMIT` | unset | Commit hash returned by `/health` |

## Development

```bash
make test
make lint
make build
```

Run the local end-to-end smoke to verify a fake daemon can receive an
OpenAI-compatible chat completion request and stream a response back:

```bash
make smoke-local
```

Example local run:

```bash
KITTYCHAT_API_TOKEN=api_secret \
KITTYCHAT_DEVICE_TOKEN=dev_secret \
KITTYCHAT_USER_ID=user_1 \
KITTYCHAT_DEVICE_ID=dev_1 \
KITTYCHAT_LOCAL_ACCOUNT_ID=alice \
make run
```

API client tokens submitted directly to KittyChat are expected to use the
portal wire format and include the KittyChat audience:

```json
{
  "iss": "https://portal.kittypaw.app/auth",
  "sub": "user_<id>",
  "aud": ["https://chat.kittypaw.app"],
  "scope": ["chat:relay", "models:read"],
  "v": 2
}
```

These API client tokens are user-scoped. They do not need `device_id` or
`account_id`; the account-scoped HTTP route selects the target device and local
account, and the broker verifies that the selected route belongs to the token's
`sub` user. If a future token includes `device_id`, kittychat treats it as an
additional restriction.

Daemon device credentials are also accepted as portal JWTs:

```json
{
  "iss": "https://portal.kittypaw.app/auth",
  "sub": "device:dev_1",
  "aud": ["https://chat.kittypaw.app"],
  "scope": ["daemon:connect"],
  "v": 2,
  "user_id": "user_<id>"
}
```

Device JWTs are RS256 signed, select keys with the JWT `kid` header, and are
verified against `KITTYCHAT_JWKS_URL`. KittyChat caches JWKS keys for 10 minutes
and refetches once when a token presents an unknown `kid`.

Static API/device tokens can still be configured as a fallback while the daemon
credential issuance flow is being rolled out.

## Routing Semantics

Daemon WebSocket connections are scoped by verified identity, not by local
account names alone. `hello.local_accounts` is the set of local account ids that
the current daemon process is actively serving on that connection. The relay
registers only the advertised active accounts. Static fallback device
credentials can still carry a configured account allow-list; RS256 device JWTs do
not carry account claims, so accounts are scoped by the verified `user_id` and
`device_id`.

Effective routing key:

```text
verified user_id + verified device_id + request account_id
```

This means `alice` on two devices, or under two API users, is not the same route.
Capabilities work the same way: a request is sent to a daemon connection only
when the connection's hello advertised the matching operation capability.

After hello, daemon-to-relay application frames are limited to
`response_headers`, `response_chunk`, `response_end`, `error`, `ping`, and
`pong`. A daemon `ping` receives a relay `pong` with the same `id` and is not
delivered to the request broker. Server-to-daemon `request` frames are rejected
if a daemon sends them back to the relay.

Clients can discover currently online routes with:

```text
GET /v1/routes
```

The endpoint requires a valid API client credential with either `models:read` or
`chat:relay`. It returns only routes for the authenticated `sub` user. If the
credential includes `device_id` or `account_id`, those claims are treated as
additional restrictions and the route list is filtered accordingly.

Example response:

```json
{
  "object": "list",
  "data": [
    {
      "device_id": "dev_1",
      "local_accounts": ["alice", "bob"],
      "capabilities": ["openai.models", "openai.chat_completions"]
    }
  ]
}
```

OpenAI-compatible clients should use the account-scoped routes when they need a
specific local account:

```text
/nodes/{device_id}/accounts/{account_id}/v1/models
/nodes/{device_id}/accounts/{account_id}/v1/chat/completions
```

The older `/nodes/{device_id}/v1/...` routes remain as an MVP fallback and use
the authenticated principal's default account. User-scoped JWTs without a
default account should use the account-scoped routes.

## Next Steps

- Keep static fallback credentials only for local/manual smoke paths.
- Expand local E2E coverage for offline devices, token refresh, and account
  isolation.
- Continue tightening the Chat BFF browser-session surface around cookie auth
  and account-scoped routing.
