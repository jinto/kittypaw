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
- static-token MVP auth for one device/account

The relay is application-level. It only forwards the narrow chat/OpenAI-compatible
surface and is not a generic localhost tunnel.

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `KITTYCHAT_API_TOKEN` | required | Bearer token for hosted OpenAI-compatible client requests |
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
KITTYCHAT_API_TOKEN=api_secret \
KITTYCHAT_DEVICE_TOKEN=dev_secret \
KITTYCHAT_USER_ID=user_1 \
KITTYCHAT_DEVICE_ID=dev_1 \
KITTYCHAT_LOCAL_ACCOUNT_ID=alice \
make run
```

## Next Steps

- Replace static MVP auth with cloud users, sessions, API keys, device credentials,
  and pairing codes backed by Postgres.
- Add the real KittyPaw daemon outbound connector.
- Add hosted chat UI after the OpenAI-compatible streaming path is stable.
