# KittyAPI 배포 메모

## 사전 준비

```bash
# fabric (로컬)
pip install fabric

# golang-migrate CLI (서버) — https://github.com/golang-migrate/migrate
curl -L https://github.com/golang-migrate/migrate/releases/download/v4.18.2/migrate.linux-amd64.tar.gz | tar xz
sudo mv migrate /usr/local/bin/
```

## 최초 셋업

```bash
# 서버에 디렉토리, nginx, systemd 설정
DEPLOY_DOMAIN=api.kittypaw.app fab setup

# 서버에 SSH 접속 후 .env 편집
ssh second
vi /home/jinto/kittyapi/.env

# PostgreSQL DB 생성
sudo -u postgres createdb kittypaw_api
sudo -u postgres createuser kittypaw

# 마이그레이션 실행
fab migrate
```

## 배포

```bash
fab deploy     # 빌드 → 업로드 → 재시작
fab status     # 서비스 상태 확인
fab logs       # 로그 확인
fab rollback   # 이전 바이너리로 복원
fab migrate    # DB 마이그레이션
```

## DNS 설정

- A 레코드: api.kittypaw.app → 서버 IP
- Cloudflare 사용 시 프록시 모드 ON

## 검증

```bash
curl https://api.kittypaw.app/health
curl https://api.kittypaw.app/.well-known/jwks.json   # Plan 20 PR-A
```

## RS256 서명 키 (Plan 20 PR-A)

JWT는 RS256으로 서명되며, 공개 키는 `/.well-known/jwks.json`으로 노출됩니다.

```bash
# 1) RSA 2048-bit private key 생성 (dev 또는 일회성)
# Linux:
openssl genrsa 2048 | base64 -w0
# macOS (-w0 미지원, tr로 개행 제거):
openssl genrsa 2048 | base64 | tr -d '\n'

# 2) 출력값을 systemd EnvironmentFile에 박제 (절대 git에 커밋 금지)
#    /etc/kittyapi/env  (예시)
JWT_PRIVATE_KEY_PEM_B64=...

# 3) 서비스 재시작 → 시작 시 fail-fast 검증 (비트 ≥2048, base64/PEM parse OK)
sudo systemctl restart kittyapi
```

**Key rotation 운영 절차** (`docs/specs/kittychat-credential-foundation.md` D5):
- old key 최소 30분 overlap (= access TTL 15min + JWKS cache 10min + safety 5min)
- rotation 시점에 양측(우리 + kittychat) 운영 알림
- 시작 정책은 단일 키 무회전 — 사고 시 수동 rotation
