# KittyAPI

[한국어](README.ko.md)

Backend API server for [KittyPaw](https://github.com/kittypaw-app). Provides public data proxying with caching and OAuth authentication for zero-config data access from KittyPaw skills.

```
KittyPaw Client ──► KittyAPI ──► Public Data APIs (AirKorea, etc.)
                        │
                        ├── OAuth (Google, GitHub)
                        ├── JWT + Refresh Token Rotation
                        ├── Rate Limiting (anon 5/min, auth 60/min)
                        └── /discovery (service URLs for SDK)
```

## Features

- **Data proxy** — cached access to public APIs (AirKorea air quality: realtime, forecast, weekly, unhealthy stations)
- **OAuth authentication** — Google + GitHub with PKCE, no email/password
- **CLI login** — `kittypaw login` via HTTP callback or one-time code paste
- **JWT + refresh tokens** — 15min access, 7-day refresh with rotation and reuse detection. Issued tokens carry `aud=["kittyapi","kittychat"]`, `scope`, and `v=1` claims — see [`docs/specs/kittychat-credential-foundation.md`](docs/specs/kittychat-credential-foundation.md).
- **Rate limiting** — per-IP anonymous (5/min) + per-user authenticated (60/min), daily 10K cap
- **Service discovery** — `GET /discovery` returns Kakao relay, API, and skills registry URLs
- **Stale-while-revalidate** — serves stale cached data when upstream is down

## Quick Start

```bash
# Prerequisites: Go 1.22+, PostgreSQL

# Configure
cp .env.example .env
# Edit .env — set DATABASE_URL, JWT_SECRET, OAuth credentials

# Database
createdb kittypaw_api
migrate -path migrations -database "$DATABASE_URL" up

# Run
make run
```

## API

### Public

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `GET` | `/discovery` | Service URLs (Kakao relay, API base, skills registry) |

### Auth

| Method | Path | Description |
|---|---|---|
| `GET` | `/auth/google` | Google OAuth login |
| `GET` | `/auth/github` | GitHub OAuth login |
| `POST` | `/auth/token/refresh` | Refresh access token |
| `GET` | `/auth/me` | Current user info |
| `GET` | `/auth/cli/{provider}` | CLI OAuth login (mode=http\|code) |
| `POST` | `/auth/cli/exchange` | Exchange one-time code for tokens |

### Data Proxy

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/air/airkorea/realtime/station` | Realtime air quality by station |
| `GET` | `/v1/air/airkorea/realtime/city` | Realtime air quality by city |
| `GET` | `/v1/air/airkorea/forecast` | Air quality forecast |
| `GET` | `/v1/air/airkorea/forecast/weekly` | Weekly particulate forecast |
| `GET` | `/v1/air/airkorea/unhealthy` | Stations exceeding standards |
| `GET` | `/v1/calendar/holidays` | Holiday info by year/month (KASI) |
| `GET` | `/v1/calendar/anniversaries` | Anniversary info |
| `GET` | `/v1/calendar/solar-terms` | 24 solar terms |
| `GET` | `/v1/weather/kma/village-fcst` | KMA village forecast (3-day) |
| `GET` | `/v1/weather/kma/ultra-srt-ncst` | KMA ultra-short nowcast |
| `GET` | `/v1/weather/kma/ultra-srt-fcst` | KMA ultra-short forecast |
| `GET` | `/v1/geo/resolve?q={query}` | Resolve Korean location → lat/lon (see [maintenance](docs/maintenance.md)) |

#### `/v1/geo/resolve` — LLM normalize guidance

For best results, kittypaw skill prompts should normalize location tokens
before calling this endpoint:

1. `○○역` (subway station) → pass as-is
2. Road or lot address → pass as-is
3. Landmark / POI ("코엑스", "광화문") → pass as-is (Wikidata + alias overrides)
4. Commercial branch ("스타벅스 강남R점") → substitute the nearest subway station
5. Unknown / ambiguous → ask the user to clarify

The endpoint returns 422 with a hint when input cannot be resolved; the
client (LLM) should surface or substitute and retry. KMA forecast grids
are 5km × 5km, so coordinate accuracy within ~1km is sufficient.

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Server port |
| `BASE_URL` | `http://localhost:8080` | Public base URL |
| `DATABASE_URL` | *(required)* | PostgreSQL connection string |
| `JWT_SECRET` | *(required, 32+ chars)* | JWT signing secret |
| `GOOGLE_CLIENT_ID` | | Google OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | | Google OAuth client secret |
| `GITHUB_CLIENT_ID` | | GitHub OAuth client ID |
| `GITHUB_CLIENT_SECRET` | | GitHub OAuth client secret |
| `CORS_ORIGINS` | `BASE_URL` | Comma-separated allowed origins |
| `AIRKOREA_API_KEY` | | AirKorea public data API key |
| `KAKAO_RELAY_URL` | | KakaoTalk relay server URL |
| `CHAT_RELAY_URL` | | Chat remote relay control plane URL |
| `API_BASE_URL` | | API base URL (for /discovery) |
| `SKILLS_REGISTRY_URL` | `https://github.com/kittypaw-app/skills` | Skills package registry |

## Deployment

See [DEPLOY.md](DEPLOY.md) for production deployment with systemd, nginx, and fabric.

```bash
fab deploy     # Build, upload, restart
fab status     # Service status
fab logs       # Tail logs
fab rollback   # Restore previous binary
fab migrate    # Run database migrations
```

## Development

```bash
make build     # Build binary
make test      # Run all tests
make lint      # Run golangci-lint
make fmt       # Format code (gofmt + goimports)
make run       # Build and run (loads .env)
```

## License

Elastic License 2.0. See [LICENSE](LICENSE) for details.
