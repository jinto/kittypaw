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
  "iss": "kittyapi",
  "sub": "user_<id>",
  "aud": ["kittyapi", "kittychat"],
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
  "iss": "kittyapi",
  "sub": "device:<device_id>",
  "aud": ["kittychat"],
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
- `iss="kittyapi"` 박제 — issuer mismatch 시 reject 권장.

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

### D5. signature algorithm

- **HS256** (현재) — kittychat 도 같은 secret 공유 또는 introspection
- **RS256 + JWKS public endpoint** = 다음 slice (kittychat 이 분리 process 에서 검증해야 할 때)
- 첫 slice 는 HS256 으로 충분 — kittychat env verifier 가 same-secret 검증

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
