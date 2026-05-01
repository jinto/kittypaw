# KittyChat

KittyChat is the chat relay for local KittyPaw daemons. It lets web chat and
OpenAI-compatible clients reach a local KittyPaw daemon through an outbound
WebSocket connection, without exposing the daemon's control UI or requiring
Tailscale/port forwarding.

## MVP Scope

This repository currently contains the relay core:

- `GET /health`
- `GET /daemon/connect` for local daemon outbound WebSocket connections
- `GET /nodes/{device_id}/v1/models`
- `POST /nodes/{device_id}/v1/chat/completions`
- JSON relay frames over WebSocket
- in-memory single-instance device broker
- API-issued JWT verifier for web chat and OpenAI-compatible clients
- env-seeded MVP daemon credential verifier for one device/account
- operation-based daemon protocol v1 for OpenAI-compatible relay requests

The relay is application-level. It only forwards the narrow chat/OpenAI-compatible
surface and is not a generic localhost tunnel.

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `KITTYCHAT_JWT_SECRET` | unset | HS256 secret shared with kittyapi for API-issued access tokens. Falls back to `JWT_SECRET` when unset |
| `KITTYCHAT_API_TOKEN` | required when JWT secret is unset | Static MVP bearer token fallback for web chat and OpenAI-compatible client requests |
| `KITTYCHAT_DEVICE_TOKEN` | required | Bearer token for daemon WebSocket connections |
| `KITTYCHAT_USER_ID` | required | MVP cloud user id |
| `KITTYCHAT_DEVICE_ID` | required | MVP device id |
| `KITTYCHAT_LOCAL_ACCOUNT_ID` | required | Local KittyPaw account id routed through this device |
| `KITTYCHAT_BIND_ADDR` | `:$PORT` or `:8080` | HTTP bind address |
| `PORT` | `8080` | Port fallback when `KITTYCHAT_BIND_ADDR` is unset |
| `KITTYCHAT_VERSION` | `dev` | Version string returned by `/health` |

## Development

```bash
make test
make lint
make build
```

Example local run:

```bash
KITTYCHAT_JWT_SECRET=test_jwt_secret_with_at_least_32_bytes \
KITTYCHAT_DEVICE_TOKEN=dev_secret \
KITTYCHAT_USER_ID=user_1 \
KITTYCHAT_DEVICE_ID=dev_1 \
KITTYCHAT_LOCAL_ACCOUNT_ID=alice \
make run
```

API client tokens are expected to use the kittyapi wire format:

```json
{
  "iss": "kittyapi",
  "sub": "user_<id>",
  "aud": ["kittyapi", "kittychat"],
  "scope": ["chat:relay", "models:read"],
  "v": 1
}
```

## Next Steps

- Replace env-seeded credentials with API-server-issued sessions, API keys,
  device credentials, and pairing codes backed by Postgres.
- Add the real KittyPaw daemon outbound connector.
- Add web chat UI after the OpenAI-compatible streaming path is stable.
