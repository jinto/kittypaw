# kittypaw-api v1

## Goal

kittypaw 스킬 패키지의 zero-config 데이터 접근. 사용자가 공공 API 키를 직접 발급받을 필요 없이 `skill install → chat` 즉시 동작.

## Context

- kittypaw: 오픈소스 Go AI 에이전트 프레임워크
- relay (CF Worker): 카카오 메시징 중계 — 별개 서비스로 유지
- kittypaw-api: 비공개 운영, 공공데이터 프록시 + 인증 서버

## Architecture

```
[Cloudflare DNS] → [VPS: Go 서버] → [PostgreSQL]
                         │
                         ├── /auth/*     (Google/GitHub OAuth + PKCE)
                         ├── /v1/air     (에어코리아 프록시)
                         ├── /health
                         │
                         └── 캐시 레이어 (in-memory 또는 Redis, 소스별 TTL)
```

- **언어**: Go
- **소스**: 비공개
- **도메인**: api.kittypaw.app (단일 도메인, auth + data 통합)
- **인프라**: VPS + Cloudflare DNS 프록시 (DDoS/SSL) + PostgreSQL
- **인증**: Google/GitHub OAuth only + PKCE (이메일/패스워드 없음)

## Endpoints

```
GET  /auth/google          → Google OAuth 시작
GET  /auth/google/callback → 콜백 + 토큰 발급
GET  /auth/github          → GitHub OAuth 시작
GET  /auth/github/callback → 콜백 + 토큰 발급
POST /auth/token/refresh   → access token 갱신
GET  /auth/me              → 사용자 정보

GET  /v1/air?lat=..&lon=.. → 대기질 (에어코리아 프록시)
GET  /health               → 서버 상태
```

## Auth & Authorization

- OAuth only (Google + GitHub), PKCE 필수
- JWT access token (15분) + opaque refresh token (7일, rotation)
- Anonymous tier: IP당 5 req/min
- Authenticated tier: per-user 60 req/min
- 전체 hard cap: 10,000 req/day

## Caching

- 에어코리아: TTL 1시간
- 장애 시: stale cache 반환 (stale-while-revalidate)
- 향후 데이터 소스별 TTL 추상화 대비

## Non-Goals (v1)

- 이메일/패스워드 인증
- auth/api 도메인 분리
- 다중 인스턴스 / 수평 확장
- relay 통합

## Acceptance Criteria

- [ ] 인증 없이 /v1/air → 200 (Anonymous, 5 req/min 이내)
- [ ] Anonymous 5회 초과 → 429
- [ ] OAuth 로그인 → JWT 발급 → /v1/air 60 req/min 허용
- [ ] 만료된 access token → 401, refresh → 새 토큰
- [ ] 동일 좌표 1시간 내 재요청 → 에어코리아 미호출 (캐시 히트)
- [ ] 에어코리아 장애 시 → stale cache 반환 (200 + 경고 헤더)
- [ ] /health → 200

## Ontology

| Entity | Relationship |
|--------|-------------|
| User | has many → AccessToken, RefreshToken |
| AccessToken | JWT, 15분 TTL, belongs to User |
| RefreshToken | opaque, 7일 TTL, rotation, belongs to User |
| DataSource | 에어코리아 (v1), 확장 가능 |
| CacheEntry | belongs to DataSource, TTL per source |
| RateLimit | per-IP (anonymous) 또는 per-User (authenticated) |

## Review Summary

### Architect
- PKCE 필수 → 반영
- Refresh token rotation + absolute expiry → 반영
- 단일 도메인 → 반영

### Critic
- Rate limit 수치 명시 → 반영 (5/60 req/min, 10K/day)
- 토큰 형식 명시 → 반영 (JWT + opaque)
- Stale cache 전략 → 반영
- 패스워드 해싱 → N/A (OAuth only로 변경)

### CEO
- 이메일/패스워드 제거 → 반영
- Anonymous tier → 반영
- SDK 토큰 자동 관리 → v2 스코프로 연기
