# kittypaw-api v1

## Plan 1: Project Scaffolding ✅

- [x] **T1: Go module + health endpoint** — `go mod init`, `cmd/server/main.go` (chi + /health), `internal/config/config.go`, 테스트 통과
- [x] **T2: Makefile + .gitignore + .env.example** — build/test/lint/run 타겟
- [x] **T3: golangci-lint** — `.golangci.yml` v2, `make lint` 통과
- [x] **T4: lefthook** — `lefthook.yml` (pre-commit: fmt + lint, commit-msg: conventional commit)
- [x] **T6: GitHub Actions CI + CLAUDE.md** — `.github/workflows/ci.yml` (lint → test)

## Plan 2: Auth ✅

- [x] **T1: Database foundation** — migrations (users, refresh_tokens) + pgx pool + `UserStore` interface + `PostgresUserStore`
- [x] **T2: JWT package** — `Sign`, `Verify` (HS256, 15min TTL)
- [x] **T3: OAuth infra** — `StateStore` (10K cap, 10min TTL, lazy eviction) + PKCE helpers
- [x] **T4: OAuth Google** — `HandleGoogleLogin` + `HandleGoogleCallback` (PKCE + code exchange + upsert + tokens)
- [x] **T5: OAuth GitHub** — `HandleGitHubLogin` + `HandleGitHubCallback`
- [x] **T6: Refresh token rotation** — `RefreshTokenStore` + `HandleTokenRefresh` (7-day expiry, reuse detection)
- [x] **T7: Auth middleware + /auth/me + CORS + route wiring** — JWT middleware, context helpers, CORS, full route wiring

## Plan 3: Data Proxy ✅

- [x] **T1: In-memory cache** — `Cache` (Get/Set/GetStale, TTL, stale-while-revalidate, background cleanup)
- [x] **T2: Rate limiting** — fixed window counter + daily 10K cap + Retry-After header + middleware
- [x] **T3: /v1/air endpoint** — 에어코리아 프록시 (15s timeout, cache, Warning header on stale, 502 on failure)
- [x] **T4: Route wiring** — cache + ratelimit + proxy integrated into main.go

## Plan 4: Calendar API (특일정보) ✅

- [x] **T1: Config** — `HOLIDAY_API_KEY` env var + `.env.example`
- [x] **T2: HolidayHandler** — 한국천문연구원 특일정보 프록시 (공휴일, 기념일, 24절기) + 테스트 10개
- [x] **T3: Route wiring** — `/v1/calendar/*` 라우트 등록
- [x] **T4: 검증** — 전체 테스트 65개 통과, lint 0 issues

## Plan 5: KMA Village Forecast Wrapper + KittyPaw fallback wiring ← 현재

> Spec: `.claude/plans/data-go-kr-wrappers.md` (v3, Phase 1+2 합의 통과)
> Goal: `/v1/weather/kma/village-fcst` proxy + `weather-briefing` skill 의 KMA fallback hook
> Delivery model: M1 (master key + cache + redistribution OK — data.go.kr 공공데이터)
> Atomic single commit (사용자 명시 허락 후)

- [x] **T1: `internal/proxy/kma/` sub-package + grid** —
      RED: `kma/grid_test.go` 11 case (5 도시: 서울 60,127 / 부산 98,76 / 대구 89,90 / 인천 55,124 / 제주 53,38 + 경계 ±0.001° 5쌍 + 한반도 외 lat=0/lon=0 + lat=43.5 → ErrOutOfKoreaPeninsula). `make test ./internal/proxy/kma/...` fail 확인.
      GREEN: `kma/grid.go` LCC 변환 (RE=6371.00877, GRID=5.0, SLAT1=30°, SLAT2=60°, OLON=126°, OLAT=38°, XO=43, YO=136) + range check (lat∈[33,39], lon∈[124,132]) + ErrOutOfKoreaPeninsula sentinel. `kma/doc.go` package doc. all green.

- [x] **T2: `internal/proxy/kma/basetime`** —
      RED: `basetime_test.go` 10 case (정상 05:30 → today 0500 / 경계 직전 05:09 → today 0200 / 직후 05:11 → today 0500 / 정확 05:10 → today 0500 / 자정 직후 00:30 → yesterday 2300 / 02:09 → yesterday 2300 / 02:11 → today 0200 / 비-슬롯 13:30 → today 1100 / 23:09 → today 2000 / 23:11 → today 2300). 시그니처 강제: `func NowToBaseDateTime(now time.Time) (baseDate, baseTime string)`. fail 확인.
      GREEN: `basetime.go` 구현. 8 슬롯 + 발표 후 10분 지연 + 날짜 경계. all green.

- [x] **T3: weather handler RED** —
      `internal/config/config.go` 에 `WeatherAPIKey` 필드 + `.env.example` 갱신.
      `internal/proxy/weather.go` stub: `WeatherHandler{Cache, HTTPClient(Timeout=15s), APIKey, BaseURL, Now func()time.Time}` + `VillageForecast() http.HandlerFunc` (빈 구현).
      `internal/proxy/weather_test.go` 6 sub-test:
        - happy + cache hit counter (`atomic.Int32` upstream call counter — 1번째 1, 2번째 *여전히* 1)
        - resultCode "03" → 502, log, 캐시 X
        - upstream 503 → cache.GetStale → 200 + `Warning: 110`
        - timeout (HTTPClient.Timeout=100ms test override + mock 200ms sleep) → 502 + context cancel
        - lat/lon 누락 → 400
        - 한반도 외 (lat=0) → 400
      fail 확인.

- [x] **T4: weather handler GREEN** —
      `weather.go` 본체 — VillageForecast 의 ⓛ–⑨ 흐름. inline `parseKMAError(body) (resultCode, resultMsg, isError)`. `kma.LatLngToGrid` + `kma.NowToBaseDateTime(h.Now())`. cacheKey prefix `"kma:village:"`. `http.NewRequestWithContext` 패턴. 6 sub-test all green.

- [x] **T5: route + integration test** —
      `cmd/server/main.go` `NewRouter` 에 `/v1/weather/kma/village-fcst` 라우트.
      `weather_integration_test.go` (`//go:build integration`): env 없으면 `t.Skipf("WEATHER_API_KEY not set")`, 있으면 서울 좌표 1회 실 KMA 호출 + `response.body` 존재 확인.
      Router-level test: 동일 IP 6회 anon 호출 → 6번째 429 (AC #7).

- [x] **T6: KittyPaw `weather-briefing` skill fallback wiring** —
      `../skills/packages/weather-briefing/main.js` 수정 (~20줄).
      `tryKMAFallback(lat, lon)` 추가 — KR 좌표 (lat∈[33,39], lon∈[124,132]) 한정 KittyAPI `/v1/weather/kma/village-fcst` 호출 (Http.get + timeout_ms=5000).
      기존 `fetchWeather()` 에 hook — Open-Meteo 실패/timeout 시 fallback.

- [x] **T7: build + lint + test + commit** —
      `make build / make lint (golangci-lint v2) / make test` 모두 pass.
      Conventional Commits — `feat(proxy): KMA village forecast wrapper + skill fallback`.
      **사용자 명시 허락 후** atomic single commit.

**Operational Checklist** (코드 외, 운영자 작업):
- [ ] data.go.kr 기상청 단기예보 (`15084084`) 인증키 신청 + 승인 (1-3일)
- [ ] 운영 서버 환경변수 `WEATHER_API_KEY` 등록
- [ ] 프로덕션 smoke: 서울 좌표 1회 curl → 200
