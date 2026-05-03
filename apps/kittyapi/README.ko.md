# KittyAPI

[English](README.md)

KittyPaw의 데이터 API 서버입니다. 이 앱은 `/v1/*` 공공데이터 프록시를
소유합니다. 인증, 서비스 디스커버리, OAuth, JWKS, device credential은
`apps/portal`로 분리되어 있습니다.

```text
KittyPaw client -> portal.kittypaw.app -> discovery, OAuth, JWKS
               -> api.kittypaw.app    -> /v1 public data APIs
```

## 기능

- AirKorea, KASI, KMA 공공데이터 프록시와 캐시
- `/v1/geo/resolve` 한국어 장소/주소 해석
- upstream 보호를 위한 익명 IP rate limit
- upstream 실패 시 stale cache 응답

## 빠른 시작

```bash
cp .env.example .env
# DATABASE_URL과 필요한 공공데이터 API 키를 설정합니다.

createdb kittypaw_api
migrate -path migrations -database "$DATABASE_URL" up

make run
```

## API

| Method | Path | 설명 |
|---|---|---|
| `GET` | `/health` | 버전 및 커밋 해시 포함 헬스 체크 |
| `GET` | `/v1/air/airkorea/realtime/station` | 측정소별 실시간 대기질 |
| `GET` | `/v1/air/airkorea/realtime/city` | 시도별 실시간 대기질 |
| `GET` | `/v1/air/airkorea/forecast` | 대기질 예보 |
| `GET` | `/v1/air/airkorea/forecast/weekly` | 주간 미세먼지 예보 |
| `GET` | `/v1/air/airkorea/unhealthy` | 기준 초과 측정소 |
| `GET` | `/v1/calendar/holidays` | 공휴일 정보 |
| `GET` | `/v1/calendar/anniversaries` | 기념일 정보 |
| `GET` | `/v1/calendar/solar-terms` | 24절기 정보 |
| `GET` | `/v1/weather/kma/village-fcst` | 기상청 단기예보 |
| `GET` | `/v1/weather/kma/ultra-srt-ncst` | 초단기 실황 |
| `GET` | `/v1/weather/kma/ultra-srt-fcst` | 초단기 예보 |
| `GET` | `/v1/almanac/lunar-date` | 양력에서 음력 변환 |
| `GET` | `/v1/almanac/solar-date` | 음력에서 양력 변환 |
| `GET` | `/v1/almanac/sun` | 일출/일몰 |
| `GET` | `/v1/geo/resolve?q={query}` | 한국어 장소를 좌표로 변환 |

`/auth/*`, `/discovery`, `/.well-known/jwks.json`은 portal 분리 이후 이
앱에서 의도적으로 404를 반환합니다.

기존 auth migration은 production DB cutover 계획 전까지 이 앱에도
남겨둡니다. 런타임은 더 이상 identity route를 제공하지 않습니다.

## 설정

| 변수 | 기본값 | 설명 |
|---|---|---|
| `PORT` | `8080` | 서버 포트. 프로덕션 deploy env는 현재 `9712`로 override합니다 |
| `UNIX_SOCKET` | 없음 | 선택 Unix 소켓 경로. 설정하면 nginx는 TCP 포트 대신 이 소켓으로 프록시합니다 |
| `BASE_URL` | `http://localhost:8080` | CORS 기본값으로 쓰는 API origin |
| `DATABASE_URL` | 필수 | PostgreSQL 연결 문자열 |
| `CORS_ORIGINS` | `BASE_URL` | 허용 origin CSV |
| `AIRKOREA_API_KEY` | | AirKorea 공공데이터 API 키 |
| `HOLIDAY_API_KEY` | | KASI 공공데이터 API 키 |
| `WEATHER_API_KEY` | | KMA 공공데이터 API 키 |
