# KittyAPI

[KittyPaw](https://github.com/kittypaw-app)를 위한 백엔드 API 서버. 공공데이터 프록시와 OAuth 인증을 제공하여 KittyPaw 스킬에서 별도 설정 없이 데이터에 접근할 수 있게 합니다.

```
KittyPaw 클라이언트 ──► KittyAPI ──► 공공데이터 API (에어코리아 등)
                            │
                            ├── OAuth (Google, GitHub)
                            ├── JWT + 리프레시 토큰 로테이션
                            ├── 속도 제한 (비인증 5/분, 인증 60/분)
                            └── /discovery (SDK용 서비스 URL)
```

## 주요 기능

- **데이터 프록시** — 공공 API 캐싱 접근 (에어코리아 대기질: 실시간, 예보, 주간, 초과 측정소)
- **OAuth 인증** — Google + GitHub, PKCE 적용, 이메일/비밀번호 없음
- **CLI 로그인** — `kittypaw login` HTTP 콜백 또는 일회용 코드 입력 방식
- **JWT + 리프레시 토큰** — 15분 액세스, 7일 리프레시 (로테이션 + 재사용 탐지)
- **속도 제한** — IP 기반 비인증 (5/분) + 사용자 기반 인증 (60/분), 일일 10K 상한
- **서비스 디스커버리** — `GET /discovery`로 릴레이, API, 스킬 레지스트리 URL 반환
- **Stale-while-revalidate** — 업스트림 장애 시 만료된 캐시 데이터 제공

## 빠른 시작

```bash
# 사전 준비: Go 1.22+, PostgreSQL

# 설정
cp .env.example .env
# .env 편집 — DATABASE_URL, JWT_SECRET, OAuth 자격증명 설정

# 데이터베이스
createdb kittypaw_api
migrate -path migrations -database "$DATABASE_URL" up

# 실행
make run
```

## API

### 공개

| 메서드 | 경로 | 설명 |
|---|---|---|
| `GET` | `/health` | 헬스 체크 |
| `GET` | `/discovery` | 서비스 URL (릴레이, API 베이스, 스킬 레지스트리) |

### 인증

| 메서드 | 경로 | 설명 |
|---|---|---|
| `GET` | `/auth/google` | Google OAuth 로그인 |
| `GET` | `/auth/github` | GitHub OAuth 로그인 |
| `POST` | `/auth/token/refresh` | 액세스 토큰 갱신 |
| `GET` | `/auth/me` | 현재 사용자 정보 |
| `GET` | `/auth/cli/{provider}` | CLI OAuth 로그인 (mode=http\|code) |
| `POST` | `/auth/cli/exchange` | 일회용 코드로 토큰 교환 |

### 데이터 프록시

| 메서드 | 경로 | 설명 |
|---|---|---|
| `GET` | `/v1/air/airkorea/realtime/station` | 측정소별 실시간 대기질 |
| `GET` | `/v1/air/airkorea/realtime/city` | 시도별 실시간 대기질 |
| `GET` | `/v1/air/airkorea/forecast` | 대기질 예보 |
| `GET` | `/v1/air/airkorea/forecast/weekly` | 주간 미세먼지 예보 |
| `GET` | `/v1/air/airkorea/unhealthy` | 기준 초과 측정소 |

## 설정

모든 설정은 환경변수로 관리합니다:

| 변수 | 기본값 | 설명 |
|---|---|---|
| `PORT` | `8080` | 서버 포트 |
| `BASE_URL` | `http://localhost:8080` | 공개 베이스 URL |
| `DATABASE_URL` | *(필수)* | PostgreSQL 연결 문자열 |
| `JWT_SECRET` | *(필수, 32자 이상)* | JWT 서명 시크릿 |
| `GOOGLE_CLIENT_ID` | | Google OAuth 클라이언트 ID |
| `GOOGLE_CLIENT_SECRET` | | Google OAuth 클라이언트 시크릿 |
| `GITHUB_CLIENT_ID` | | GitHub OAuth 클라이언트 ID |
| `GITHUB_CLIENT_SECRET` | | GitHub OAuth 클라이언트 시크릿 |
| `CORS_ORIGINS` | `BASE_URL` | 허용 오리진 (쉼표 구분) |
| `AIRKOREA_API_KEY` | | 에어코리아 공공데이터 API 키 |
| `RELAY_URL` | | 메시징 릴레이 서버 URL |
| `API_BASE_URL` | | API 베이스 URL (/discovery용) |
| `SKILLS_REGISTRY_URL` | `https://github.com/kittypaw-app/skills` | 스킬 패키지 레지스트리 |

## 배포

프로덕션 배포 가이드는 [DEPLOY.md](DEPLOY.md)를 참고하세요.

```bash
fab deploy     # 빌드, 업로드, 재시작
fab status     # 서비스 상태
fab logs       # 로그 확인
fab rollback   # 이전 바이너리로 복원
fab migrate    # DB 마이그레이션 실행
```

## 개발

```bash
make build     # 바이너리 빌드
make test      # 전체 테스트 실행
make lint      # golangci-lint 실행
make fmt       # 코드 포맷 (gofmt + goimports)
make run       # 빌드 후 실행 (.env 로드)
```

## 라이선스

Elastic License 2.0. 자세한 내용은 [LICENSE](LICENSE)를 참고하세요.
