# KittyAPI 배포 메모

KittyAPI는 `api.kittypaw.app`에서 `/v1/*` 데이터 API만 제공합니다.
인증, discovery, JWKS는 `apps/portal`이 `portal.kittypaw.app`에서
제공합니다.

## 환경 변수

`deploy/env.example`를 기준으로 서버의 EnvironmentFile을 구성합니다.

```text
PORT=9712
UNIX_SOCKET=/home/jinto/kittyapi/kittyapi.sock
BASE_URL=https://api.kittypaw.app
DATABASE_URL=postgres://...
CORS_ORIGINS=https://kittypaw.app,https://portal.kittypaw.app,https://api.kittypaw.app,https://chat.kittypaw.app
AIRKOREA_API_KEY=
HOLIDAY_API_KEY=
WEATHER_API_KEY=
```

## 검증

```bash
curl https://api.kittypaw.app/health
curl https://api.kittypaw.app/discovery              # 404
curl https://api.kittypaw.app/.well-known/jwks.json  # 404
bash deploy/smoke.sh
```

## Fabric 작업

```bash
uv run fab setup
uv run fab deploy
uv run fab smoke
uv run fab migrate
uv run fab rollback
uv run fab status
uv run fab logs
```

## 마이그레이션

현재 production DB는 기존 `kittypaw_api` 데이터베이스를 계속 사용합니다.
서비스 소유권은 분리됐지만, 물리 DB 분리는 별도 cutover 계획에서 다룹니다.
