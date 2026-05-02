# kittychat credential foundation — Plan 17

> **Slug**: `kittychat-credential-foundation`
> **Date**: 2026-05-02
> **Trigger**: kittychat 측 codex 가 implementer 들어가기 전 합의 요청 — auth contract source of truth 박제 필요
> **Decision mode**: C 하이브리드 (spec 박제 + minimal slice 즉시)

## Context

kittychat 은 *우리 발급 credential 의 검증자*. 우리(kittyapi) 가 *발급자 + contract source of truth*. 분산 client (daemon) 환경 → wire protocol **additive only**.

kittychat 측 codex 가 결정 (그쪽 영역):
- `identity.Store` → `identity.CredentialVerifier` rename
- `APIClientClaims` / `DeviceClaims` 정의 + handler principal 변환
- env-seeded verifier 로 시작 (우리 발급 token 미사용 단계)

우리 측 결정 (이 plan 박제):
- claims schema, scope vocab, version policy
- **`aud` 정책 = multi-aud** (사용자 결정 2026-05-02): `aud=["kittyapi","kittychat"]` — RFC 7519 standard, 단일 token, BC 안전.

## Goal

이 plan 은 **spec 박제 + minimal JWT claims 확장 (Plan 17 T1~T5)**. defer 영역 박제.

## 핵심 결정

### D1. multi-aud (사용자 결정 채택)

```json
"aud": ["kittyapi", "kittychat"]
```

- API client JWT: 두 audience 모두 (kittypaw SDK 가 kittyapi 호출 + kittychat 가 동일 token 검증)
- daemon JWT (다음 slice): `aud=["kittychat"]` 만 — daemon 은 kittyapi 직접 호출 안 함

### D2. claims schema (RFC 7519 + 우리 확장)

**API client (web/CLI 사용자)**:
```json
{
  "iss": "https://portal.kittypaw.app/auth",
  "sub": "user_<id>",
  "aud": ["https://api.kittypaw.app", "https://chat.kittypaw.app"],
  "scope": ["chat:relay", "models:read"],
  "v": 1,
  "device_id": "<optional, set when device-scoped>",
  "account_id": "<optional, set when account-scoped>",
  "iat": 1234567890,
  "exp": 1234568790
}
```

**daemon (device-scoped, 다음 slice)**:
```json
{
  "iss": "https://portal.kittypaw.app/auth",
  "sub": "device:<device_id>",
  "aud": ["https://chat.kittypaw.app"],
  "scope": ["daemon:connect"],
  "v": 1,
  "user_id": "user_<id>",
  "device_id": "<id>",
  "local_accounts": ["alice", "bob"],
  "iat": 1234567890,
  "exp": 1234568790
}
```

**Wire-format 박제 (검증자 측 주의)**:
- `sub` 는 RFC 7519 standard. **`uid` 는 사용 안 함** — 초기 박제 시 `uid` 박제됐던 것을 2026-05-02 정정. 검증자가 *uid 우선 + sub fallback* hack 박제 불필요.
- `iss="https://portal.kittypaw.app/auth"` 박제 — issuer mismatch 시 reject 권장. `api.kittypaw.app/auth` compatibility 없음.
- `aud` URL form (`["https://api.kittypaw.app", "https://chat.kittypaw.app"]`) — RFC 7519 / OIDC pattern. resource server URL 일치 (Auth0/Okta/AWS Cognito 같은 표준).

### D3. scope vocabulary (확장 가능, additive only)

| scope | 의미 | grant 대상 |
|---|---|---|
| `chat:relay` | chat completion relay (kittychat → daemon) | API client |
| `models:read` | models list | API client |
| `daemon:connect` | daemon outbound WSS | daemon credential |
| *(추가 시 plan 또는 follow-up commit 으로 박제)* | | |

### D4. version policy

- `v=1` 박제 (모든 발급 token)
- **additive only** — field 추가 OK, rename/remove 금지
- unknown field/scope **ignore** (forward compat)
- breaking change 발생 시 `v` bump + dual issue 기간 박제

### D5. signature algorithm — RS256 + JWKS (Plan 20 PR-A 박제, 사용자 0명 cutover)

- **RS256** — kittyapi 발급, kittychat 이 JWKS public key 로 검증. HS256 secret 공유 risk 제거.
- **JWKS endpoint**: `https://portal.kittypaw.app/.well-known/jwks.json` (RFC 8615 well-known + RFC 7517 JWK Set).
- **kid header 필수**: 모든 JWT header 에 `kid` 박제. JWKS 룩업의 단일 진실. RFC 7638 SHA-256 thumbprint 알고리즘.
- **Cache-Control**: `public, max-age=600` (10min). kittychat 도 cache TTL 10min 합의 — drift 시 rotation contract (old key overlap 30min) 깨짐.
- **Key rotation contract** (운영 절차):
  - 시작은 단일 키. 자동 회전 후순위.
  - 사고 시 수동 rotation. **old key 최소 30분 overlap 유지** (= access TTL 15min + cache TTL 10min + safety 5min).
  - rotation 시점에 양측 (우리 + kittychat) 운영 알림 합의.
- **kittychat fetch 실패 fail-mode** (cross-team 합의):
  - JWKS endpoint 일시 unreachable → kittychat 이 stale cache 사용.
  - unknown kid 발견 → kittychat 이 1회 refetch 시도. 무한 refetch 방지를 위해 backoff (최소 1초 간격).
  - 두 정책 모두 kittychat 측 verifier 책임.
- **Verify invariants** (downgrade + cross-audience guard):
  - `WithValidMethods([]string{"RS256"})` 강제 — alg=HS256 위조 토큰 거부 (downgrade attack)
  - `WithIssuer(Issuer)` exact match
  - `WithAudience(expected)` strict — caller 가 지정. user middleware = `AudienceAPI`만, kittychat verifier = `AudienceChat`만. **device JWT 가 user middleware 통과 시 거부** (cross-audience leak guard).
  - `WithLeeway(60*time.Second)` — clock skew 60s 양쪽 합의
- **JWKSProvider interface**:
  ```go
  type JWKSProvider interface {
      Lookup(kid string) (*rsa.PublicKey, error)
      JWKSet() JWKSet
  }
  ```
  - 우리 측: in-memory single-key store (PR-A) → 미래 multi-key rotation 시 동일 인터페이스로 교체.
  - kittychat 측: HTTP fetch + cache (10min) — 본 spec 외부, 그쪽 구현 자유.
- **사용자 0명 윈도우 cutover** (BC 부담 0): HS256 ship 후 마이그레이션이 아니라 PR-A 부터 RS256 직진. ClaimsVersion v1 → v2 bump (PR-B). user JWT + device JWT 동일 cutover.

### D5b. device JWT 발급 — production endpoints (Plan 23 PR-D 박제)

PR-D 머지 시점에 4 endpoints + production issue 함수 활성. PR-A `c75c238`, PR-B `eca6e42`, PR-C `d275a86`, PR-D 다음 commit으로 cutover 완성.

**Production issue 함수**: `auth.SignDeviceJWT(userID, deviceID string, key *rsa.PrivateKey, kid string, ttl time.Duration) (string, error)`. testfixture의 `IssueDeviceJWT` 와 wire-format 동일 (TestSignDeviceJWT_WireFormatMatchesIssueDeviceJWT 가 cross-team contract 회귀 검출).

**Endpoint contracts**:

| Method/Path | Auth | Body | Response |
|---|---|---|---|
| `POST /auth/devices/pair` | Bearer user JWT | `{name, capabilities}` | 200 + `{device_id, device_access_token, device_refresh_token, expires_in: 900}` |
| `POST /auth/devices/refresh` | **none** (opaque body token만) | `{refresh_token}` | 200 동일 shape, rotation. 401 if reuse → `RevokeAllForDevice` 호출 |
| `GET /auth/devices` | Bearer user JWT | — | 200 array (paired_at DESC, revoked_at IS NULL filter). 0 devices = `[]` (NOT `null`) |
| `DELETE /auth/devices/{id}` | Bearer user JWT | — | 200 `{}`. soft-delete + RevokeAllForDevice. 다른 user의 device → 404 (non-disclosure) |

**Refresh route is OUTSIDE authMW** (Plan 23 결정 3): chi.Group으로 분리. daemon이 stale Authorization header (만료된 device JWT) 함께 보내도 user-aud middleware 통과 시도 → 401 차단되지 않고 handler 도달. opaque token만 credential.

**Pair atomicity** (결정 2): sequential explicit revoke. DeviceStore.Create 성공 후 어떤 단계 (SignDeviceJWT / GenerateRefreshToken / CreateForDevice) 실패 시 즉시 `DeviceStore.Revoke(dev.ID)` 호출 — orphan device row 방지.

**Error mapping table** (운영자/디버거 reference):

| 시나리오 | HTTP | 비고 |
|---|---|---|
| Pair: anonymous | 401 | UserFromContext nil |
| Pair: malformed/large/empty body | 400 | 4KiB cap |
| Pair: capabilities is array | 400 | Go decode type-mismatch |
| Pair: post-Create 단계 실패 | 500 | compensating revoke 후 |
| Refresh: malformed/large body | 400 | 1KiB cap |
| Refresh: unknown/expired/user-scoped/revoked | 401 silent | 정보 누출 회피 |
| Refresh: revoked refresh (reuse) | 401 + RevokeAllForDevice | reuse detection |
| List: anonymous | 401 | |
| List: 0 devices | 200 + `[]` | NOT `null` |
| Delete: anonymous | 401 | |
| Delete: not found / different user / already revoked / invalid UUID | 404 | non-disclosure |

### D6. device 개념 — schema 만, 발급 endpoint defer

- `users` (1) → `devices` (N) 1:N 박제 (다음 slice)
- columns: `id, user_id, name, created_at, last_seen, credential_version`
- 발급 endpoint (`POST /auth/devices/pair`, `POST /auth/devices/{id}/credential`) = 다음 slice
- 지금 slice 는 *API client claims* 만 다룸. daemon claims 는 placeholder.

### D7. defer 박제 (명시적 비범위)

| 항목 | 미루는 근거 |
|---|---|
| device 발급 endpoint | spec 합의 후 별도 slice — daemon 측 진행 속도 의존 |
| opaque API key + introspection | OpenAI-compatible client. JWT 만으로 첫 slice 충분 |
| JWKS public endpoint | RS256 마이그레이션 후. kittychat 이 introspection mode 시작 시 안 만들어도 됨 |
| pairing flow (registration code) | UI/UX 영역, daemon 측 진행과 묶음 |
| scope 검증 middleware | 발급자 측 (우리)가 *발급* 만 박제 후 검증자 측 (kittychat) 이 enforce |

### D8. auth authority vs resource server (Plan 13, 사용자 결정 2026-05-02)

**배경**: "API 가 인증한다" 는 명명 혼란. *auth authority* 와 *resource server* 는 *논리적으로 다른 책임*. 코드는 이미 옳은 방향 (kittypaw-api 가 발급, kittychat 이 검증), 단 *이름/식별자* 가 따라가지 못함.

**결정**:
- **issuer (path-based)**: `iss = "https://portal.kittypaw.app/auth"`. *현재* 의 사실 (실 host 박제) 일치. `/auth/*` namespace 가 *auth authority endpoint*, `/v1/*` 가 *api resource server endpoint*.
- **audience (URL form)**: `aud = ["https://api.kittypaw.app", "https://chat.kittypaw.app"]`. RFC 7519 / OIDC standard. resource server URL.
- **discovery `auth_base_url`**: `https://portal.kittypaw.app/auth` 박제 (server 가 `BaseURL + "/auth"` derive).

**2026-05-02 portal split update**:
- `portal.kittypaw.app` 가 canonical auth/discovery/JWKS host.
- `api.kittypaw.app/auth/*`, `/discovery`, `/.well-known/jwks.json` compatibility 없음.
- 별 process 추출은 `services/portal` phase 2에서 수행.

**책임 분리 명시**:
| 역할 | host (지금) | path | 책임 |
|---|---|---|---|
| auth authority | `portal.kittypaw.app` | `/auth/*` | OAuth login, refresh, /me, JWT 발급 |
| api resource server | `api.kittypaw.app` | `/v1/*` | 데이터 프록시 (검증만) |
| chat resource server | `chat.kittypaw.app` | `/*` | relay (검증만) |

지금은 *auth authority + api resource server* 가 한 process 박제 (kittyapi)지만 host boundary는 분리되어 있다. 다음 단계에서 `services/portal`로 process를 나눈다.

## Plan 17 — TDD 태스크 분해 (kickoff 시 ina:plan)

T1~T5 가 ≤7 task 한계 안. kickoff 시 분해:

- **T1**: `internal/auth/jwt.go` `Claims` struct 확장 (`Aud []string`, `Scope []string`, `V int`) — RED in `jwt_test.go`
- **T2**: `auth.SignForAudiences(userID string, audiences []string, scopes []string, secret string, ttl time.Duration)` 신규 helper — 기존 `Sign` BC 보존
- **T3**: OAuth Google/GitHub callback 의 access token 발급 시 default claims 적용 (`aud=["kittyapi","kittychat"]`, `scope=["chat:relay","models:read"]`, `v=1`)
- **T4**: `internal/auth/scopes.go` 신규 — scope 상수 박제 (`ScopeChatRelay = "chat:relay"` 등)
- **T5**: README 의 OAuth 섹션 + 본 plan link 박제 + commit

## kittychat 측 합의 사항 (그쪽 책임 — 이 plan 의 비범위)

- `identity.Store` → `identity.CredentialVerifier` rename
- `APIClientClaims` / `DeviceClaims` 타입 정의 (위 D2 schema 매칭)
- env-seeded verifier 시작 — `chat:relay`, `models:read`, `daemon:connect` default seed
- daemon hello schema 합의 (kittypaw daemon 담당과 — 우리 영역 X)

## Risk / Open Question

| # | Risk | Mitigation |
|---|---|---|
| R1 | multi-aud 가 *기존 access token* 의미 변경 → BC | `auth.Verify` 가 *aud 미박제 token 도 통과* (T2 에서 backward-compat 검증). 신규 발급만 multi-aud. |
| R2 | scope 검증 middleware 도입 시점 | 다음 slice. 우리 측은 *발급* 만, 검증은 kittychat 책임 (이번 slice 한정) |
| R3 | device credential 발급 — daemon 진행 속도 의존 | daemon 담당이 hello schema 박제 + 우리 측 device endpoint 신설 동시 trigger 시 진행 |
| R4 | scope vocabulary 변경 — 한 번 박제하면 rename 비용 큼 | 본 plan D3 박제 + 추가 시 *additive only*. rename 금지. |

## Blast Radius

| 파일 | 변경 |
|---|---|
| `internal/auth/jwt.go` | `Claims` struct + `SignForAudiences` 신설 |
| `internal/auth/jwt_test.go` | claims 확장 검증 |
| `internal/auth/scopes.go` | **신규** — scope const |
| `internal/auth/google.go` / `github.go` | callback 의 token 발급 시 default claims |
| `README.md` / `README.ko.md` | OAuth 섹션 + link |
| `migrations/` | 미변경 (device schema = 다음 slice) |

production 코드 변경: `jwt.go` (struct + helper), `google.go`/`github.go` (호출). 5 file.

## 검증 (Plan 17 머지 시점)

- [ ] T1~T5 모두 ✅
- [ ] `make test` PASS (기존 + 신규 case)
- [ ] `make build / make lint` 0 issues
- [ ] *기존* 발급 token (aud 미박제) 도 `auth.Verify` 통과 — BC 검증
- [ ] *신규* 발급 token 이 multi-aud + scope + v=1 박제 검증 — `TestSignForAudiences_*`
- [ ] kittychat 측 env verifier 가 본 spec 일치 적용 시작 (그쪽 PR 트래킹)

## 다음 slice (Plan 17 머지 후)

1. device schema migration (`migrations/0XX_create_devices.up.sql`)
2. device credential 발급 endpoint (`POST /auth/devices/pair`, `POST /auth/devices/{id}/credential`)
3. opaque API key + introspection endpoint
4. JWKS public endpoint + RS256 마이그레이션
5. scope 검증 middleware (우리 측 자체 사용 시)
