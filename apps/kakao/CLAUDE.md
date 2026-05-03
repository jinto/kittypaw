# KittyKakao — Development Guidelines

## Open Source Security Policy

이 프로젝트는 오픈소스로 공개된다. 누구나 자신의 서버에 배포할 수 있지만, 우리 시스템의 내부 정보는 코드에 포함되어서는 안 된다.

### 코드에 포함하면 안 되는 것

- 실제 서버 IP, 호스트명, SSH alias
- 카카오 채널 ID (실제 채널 식별자)
- 운영 도메인
- WEBHOOK_SECRET 값
- API 키, 토큰, 인증 정보

### 허용되는 것

- 환경변수에서 읽는 코드 (`os.Getenv("WEBHOOK_SECRET")`)
- 플레이스홀더 (`{{DOMAIN}}`, `your.domain.example`)
- 테스트 전용 더미 값 (`https://pf.kakao.com/test`)
- SSH alias를 환경변수로 읽는 배포 스크립트 (`DEPLOY_HOST`)

### 이전 수정 이력 (참고)

| 파일 | 수정 내용 | 이유 |
|------|-----------|------|
| `internal/config/config.go` | `CHANNEL_URL`은 환경변수에서 읽고 기본값은 빈 값으로 유지 | 카카오 채널 ID 하드코딩 제거 |
| `deploy/env.example` | `CHANNEL_URL`을 플레이스홀더로 | 동일 |
| `deploy/kittykakao.nginx` | `server_name`을 `{{DOMAIN}}`으로 | 운영 도메인 노출 방지 |
| `DEPLOY.md` | 도메인, 호스트명을 일반화 | 내부 인프라 정보 제거 |
| `fabfile.py` | `HOST`를 `DEPLOY_HOST` 환경변수로 | 서버 정보 분리 |

### 커밋 전 체크리스트

- [ ] 실제 도메인, 채널 ID, 시크릿 값이 소스에 하드코딩되지 않았는지 grep으로 확인
- [ ] 새 환경변수를 추가했다면 `deploy/env.example`에 플레이스홀더 추가
- [ ] nginx 설정에 실제 도메인 대신 `{{DOMAIN}}` 사용

## Build & Test

```bash
make build
make test
```

## Deploy

```bash
DEPLOY_HOST=<ssh-alias> DEPLOY_DOMAIN=<domain> fab setup   # 최초 1회
DEPLOY_HOST=<ssh-alias> fab deploy                          # 배포
```
