# KittyAPI

[한국어](README.ko.md)

Data API server for KittyPaw. This app owns the public-data proxy routes under
`/v1/*`; identity, service discovery, OAuth, JWKS, and device credentials live
in `apps/portal`.

```text
KittyPaw client -> portal.kittypaw.app -> discovery, OAuth, JWKS
               -> api.kittypaw.app    -> /v1 public data APIs
```

## Features

- Cached access to public APIs: AirKorea, KASI calendar/almanac, KMA weather
- Korean place and address resolution under `/v1/geo/resolve`
- Anonymous IP rate limiting for upstream protection
- Stale-while-revalidate cache behavior when upstreams fail

## Quick Start

```bash
# Prerequisites: Go, PostgreSQL
cp .env.example .env
# Edit .env and set DATABASE_URL plus upstream API keys as needed.

createdb kittypaw_api
migrate -path migrations -database "$DATABASE_URL" up

make run
```

## API

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `GET` | `/v1/air/airkorea/realtime/station` | Realtime air quality by station |
| `GET` | `/v1/air/airkorea/realtime/city` | Realtime air quality by city |
| `GET` | `/v1/air/airkorea/forecast` | Air quality forecast |
| `GET` | `/v1/air/airkorea/forecast/weekly` | Weekly particulate forecast |
| `GET` | `/v1/air/airkorea/unhealthy` | Stations exceeding standards |
| `GET` | `/v1/calendar/holidays` | Holiday info by year/month |
| `GET` | `/v1/calendar/anniversaries` | Anniversary info |
| `GET` | `/v1/calendar/solar-terms` | 24 solar terms |
| `GET` | `/v1/weather/kma/village-fcst` | KMA village forecast |
| `GET` | `/v1/weather/kma/ultra-srt-ncst` | KMA ultra-short nowcast |
| `GET` | `/v1/weather/kma/ultra-srt-fcst` | KMA ultra-short forecast |
| `GET` | `/v1/almanac/lunar-date` | Solar date to lunar date |
| `GET` | `/v1/almanac/solar-date` | Lunar date to solar date |
| `GET` | `/v1/almanac/sun` | Sunrise and sunset |
| `GET` | `/v1/geo/resolve?q={query}` | Resolve Korean location to lat/lon |

`/auth/*`, `/discovery`, and `/.well-known/jwks.json` intentionally return
404 from this app after the portal split.

Historical auth migrations remain in this app until the production DB cutover
is planned; the runtime no longer serves identity routes.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Server port |
| `BASE_URL` | `http://localhost:8080` | Public API origin for CORS default |
| `DATABASE_URL` | required | PostgreSQL connection string |
| `CORS_ORIGINS` | `BASE_URL` | Comma-separated allowed origins |
| `AIRKOREA_API_KEY` | | AirKorea public data API key |
| `HOLIDAY_API_KEY` | | KASI public data API key |
| `WEATHER_API_KEY` | | KMA public data API key |

## Development

```bash
make build
make test
make run
bash deploy/smoke.sh
```
