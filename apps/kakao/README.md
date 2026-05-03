# KittyKakao

[한국어](README.ko.md)

A messaging relay server for [KittyPaw](https://github.com/kittypaw-app). Bridges KakaoTalk chatbot messages to connected clients over WebSocket.

```
KakaoTalk User ──► Kakao OpenBuilder ──► KittyKakao ──► WebSocket ──► KittyPaw Client
                                              ◄── async callback ◄── response ◄──┘
```

## How It Works

1. A KittyPaw client registers and receives a **pairing code**
2. The KakaoTalk user sends the 6-digit code to the chatbot to link their account
3. When the user sends a message, Kakao's OpenBuilder forwards it to KittyKakao
4. KittyKakao relays the message to the paired client via WebSocket
5. The client responds, and KittyKakao delivers the reply through Kakao's async callback

## Features

- **WebSocket relay** with automatic ping/pong keepalive
- **6-digit pairing** for device-to-account linking
- **Async callbacks** via Kakao OpenBuilder's callback protocol
- **Rate limiting** — configurable daily and monthly caps
- **Killswitch** — instantly suspend message processing
- **SSRF protection** — callback URLs restricted to `*.kakao.com`
- **SQLite + WAL** — zero-dependency persistent storage
- **Unix socket** — direct nginx-to-relay communication
- **Graceful shutdown** — drains connections on SIGTERM

## Quick Start

```bash
# Build
make build

# Configure
cp deploy/env.example .env
# Edit .env — at minimum, set WEBHOOK_SECRET

# Run
WEBHOOK_SECRET=your-secret ./kittykakao
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|---|---|---|
| `WEBHOOK_SECRET` | *(required)* | Shared secret for webhook and admin authentication |
| `BIND_ADDR` | `0.0.0.0:8787` | TCP address or Unix socket path (paths starting with `/`) |
| `DATABASE_PATH` | `relay.db` | SQLite database file path |
| `CHANNEL_URL` | *(empty)* | KakaoTalk channel URL returned on registration |
| `DAILY_LIMIT` | `10000` | Max messages per day |
| `MONTHLY_LIMIT` | `100000` | Max messages per month |
| `LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `RUST_LOG` | *(legacy)* | Legacy log level fallback accepted for existing `.env` files |

## API

### Public

| Method | Path | Description |
|---|---|---|
| `POST` | `/register` | Register a new client, returns token + pairing code |
| `GET` | `/pair-status/{token}` | Check if a token has been paired |
| `POST` | `/webhook?secret=` | KakaoTalk OpenBuilder webhook endpoint |
| `GET` | `/ws/{token}` | WebSocket connection for paired clients |
| `GET` | `/health` | Health check with version and commit hash |

### Admin

| Method | Path | Description |
|---|---|---|
| `GET` | `/admin/stats?secret=` | Usage stats, active sessions, memory, file descriptors |
| `POST` | `/admin/killswitch?secret=` | Enable/disable message processing |

## WebSocket Protocol

**Server → Client** (incoming message):
```json
{
  "id": "action_id",
  "text": "user message",
  "user_id": "kakao_user_id",
  "attachments": [
    {
      "id": "kakao_media_0",
      "type": "image",
      "source": "kakao",
      "url": "https://cdn.example.com/cat.png"
    }
  ]
}
```

`attachments` is omitted when the Kakao OpenBuilder payload has no media.

**Client → Server** (response):
```json
{"id": "action_id", "text": "response message", "image_url": "https://cdn.example.com/reply.png", "image_alt": "reply image"}
```

## Deployment

See [DEPLOY.md](DEPLOY.md) for production deployment with systemd, nginx, and Cloudflare.

Pre-built deployment configs are in the `deploy/` directory:
- `kittykakao.service` — systemd unit
- `kittykakao.nginx` — nginx reverse proxy with WebSocket support
- `env.example` — environment variable template

## Development

```bash
make build            # Build binary
make test             # Run tests
make lint             # Lint
LOG_LEVEL=debug make run  # Run with debug logging
```

## License

Elastic License 2.0. See [LICENSE](LICENSE) for details.
