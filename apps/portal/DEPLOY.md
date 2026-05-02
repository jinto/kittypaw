# Kitty Portal 배포 메모

Portal은 `portal.kittypaw.app`에서 auth authority와 service discovery를
제공합니다. `/v1/*` 데이터 API는 `apps/kittyapi`가 담당합니다.

## 환경 변수

`deploy/env.example`를 기준으로 서버의 EnvironmentFile을 구성합니다.

```text
PORT=9714
BASE_URL=https://portal.kittypaw.app
API_BASE_URL=https://api.kittypaw.app
DATABASE_URL=postgres://...
JWT_PRIVATE_KEY_PEM_B64=
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=
WEB_REDIRECT_URI_ALLOWLIST=https://chat.kittypaw.app/auth/callback
```

## RS256 서명 키

JWT는 RS256으로 서명되며 공개 키는
`https://portal.kittypaw.app/.well-known/jwks.json`으로 노출됩니다.

```bash
# Linux
openssl genrsa 2048 | base64 -w0

# macOS
openssl genrsa 2048 | base64 | tr -d '\n'
```

출력값은 `JWT_PRIVATE_KEY_PEM_B64`에만 설정하고 git에 커밋하지 않습니다.

## 검증

```bash
curl https://portal.kittypaw.app/health
curl https://portal.kittypaw.app/discovery
curl https://portal.kittypaw.app/.well-known/jwks.json
curl https://portal.kittypaw.app/v1/geo/resolve       # 404
bash deploy/smoke.sh
```

## DB

Portal은 users, refresh_tokens, devices 테이블을 소유합니다. 현재 production
DB 물리는 기존 `kittypaw_api` 데이터베이스를 공유하며, 별도 DB로의 물리
분리는 후속 cutover에서 다룹니다.
