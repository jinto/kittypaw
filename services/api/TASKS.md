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

## Plan 5: KMA Village Forecast Wrapper + KittyPaw fallback wiring ✅

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

## Plan 6: Places DB + /v1/geo/resolve

> Spec: `.claude/plans/geo-address-coords.md` (v9, PR-2 의사결정 박제)
> Goal: 자체 통합 places DB로 LLM 자연어 위치 입력 → 좌표 변환. **외부 API 의존 0**
> 데이터: Wikidata(CC0) + 서울교통공사 1~8호선(제한없음) + 별칭 50 + 행안부 도로명주소(PR-2, 제한없음)
> PR-1: Wikidata + 서울교통공사 + 별칭 + 벤치마크 ✅
> PR-2: 행안부 도로명주소 (EPSG:5179 → WGS84 pure-Go 변환 + 별도 addresses 테이블) ← **현재 (build target)**

### PR-1 ✅ (8 태스크 — TDD 사이클)

- [x] **T1: migration** —
      RED: `migrations/00X_create_places.up.sql` + `down.sql`. `make migrate` → places + alias_overrides + pg_trgm + 인덱스 생성. `SELECT 1 FROM places LIMIT 0` 통합 테스트.
      GREEN: SQL 작성. `UNIQUE (source, source_ref)` + GIN 인덱스. pg_trgm 권한 부재 시 명시적 에러.

- [x] **T2: model/place.go** —
      RED: `internal/model/place_test.go` 5 함수 테이블 테스트 — `FindExact`, `FindByAlias`, `FindByFuzzy`, `FindAliasOverride`, `Upsert`. fixture INSERT 후 검증.
      GREEN: pgx raw SQL 5 함수. ORDER BY `similarity DESC, (CASE type WHEN 'landmark' THEN 0 ELSE 1 END) ASC, source_priority DESC, id ASC`.

- [x] **T3: proxy/places.go + places_errors.go** —
      RED: `internal/proxy/places_test.go` Resolve 통합 테스트 — NFC 정규화·길이 검증·typeHint·chain 5단계 + 400/414/422 응답.
      GREEN: `Resolve` chain (alias_override → exact → alias → fuzzy → 422). 에러 enum const.

- [x] **T4: cmd/seed-wikidata** —
      RED: `cmd/seed-wikidata/main_test.go` fakeUpstream으로 SPARQL mock — 페이징·재시도·swap·체크포인트.
      GREEN: SPARQL 클라이언트 (offset+limit 1000, max retry 3, exponential backoff). transactional swap (places_import_<run_id>). 체크포인트 `places_import_state.json`.

- [x] **T5: cmd/seed-seoul-metro** —
      RED: 작은 CSV 입력 → places에 정확 INSERT 검증.
      GREEN: CSV 파서 + COPY FROM ON CONFLICT.

- [x] **T6a: 별칭 50개 + 골든 17건** —
      RED: `migrations/00Y_seed_alias_overrides.up.sql` (§10 정책 준수). `testdata/golden_cases.json` 12 positive + 5 negative. `internal/proxy/places_golden_test.go`.
      GREEN: 50개 SQL 시드 + 골든 테스트 통과 (코엑스/광화문/강남역/63빌딩/잠실역/장한평역/롯데월드타워/경복궁/DDP/코엑스몰 + 422 케이스).

- [x] **T6b: corpus 인프라 + 벤치마크 cmd** (24건 bootstrap, 100건 확장은 운영 후 follow-up) —
      RED: `testdata/korean_corpus.json` 100건 (50 시나리오 + 50 변형 NFC/NFD/한자/오타). `cmd/benchmark-resolve/main.go`. `make benchmark-resolve` 타겟.
      GREEN: corpus 작성 + 측정 + **precision ≥ 0.85 게이트**. 미달 시 alias 보강 또는 threshold 조정.

- [x] **T7: 라우트 등록 + README + Makefile + docs/maintenance.md** —
      RED: `cmd/server/main.go` `/v1/geo/resolve` 라우트. 통합 테스트.
      GREEN: 라우트 1줄 + README LLM normalize 가이드 섹션 + Makefile (`seed-wikidata`, `seed-seoul-metro`, `benchmark-resolve`). `make build/lint/test` pass. **사용자 명시 허락 후** atomic commit.

**Operational Checklist**:
- [ ] PostgreSQL pg_trgm superuser 1회 설치 (RDS 시 `rds.extensions = pg_trgm` 파라미터 그룹)
- [ ] `make seed-wikidata` 첫 임포트 (~10k row, 수 분)
- [ ] `make seed-seoul-metro` 첫 임포트 (~280 row, 수 초)
- [ ] cron: Wikidata 주간, 서울교통공사 연 1회

**Follow-up Issues** (PR-1 범위 외, Phase 2 리뷰 결과 포함):
- [ ] **Anon rate limit 재검토** — 현재 5 rpm/IP는 LLM 사용에 부족 (Security Lane #2). 옵션: (a) /v1/geo만 별도 한도, (b) 전체 anon 한도 상향, (c) auth 강제. 트래픽 데이터 후 결정
- [ ] **Integration test harness** — PostgreSQL + pg_trgm 실제 SQL 동작 검증 `//go:build integration` (Adversarial #9). docker-compose 권고
- [ ] **Fuzzy threshold 튜닝** — 0.7은 한국어 짧은 토큰("강남" → "강남역" similarity ≈ 0.67)에서 미달. corpus benchmark 결과로 0.45~0.5로 조정 검토 (Adversarial #5)
- [ ] **Curated alias 좌표 검증** — `잠실` 등 round-numbered 좌표는 placeholder 가능성. corpus benchmark에서 ±200m 게이트로 사후 검증 (Adversarial #6)
- [ ] **alias_overrides 우선순위 메타데이터** — `disabled BOOLEAN` / `defeat_exact BOOLEAN` 등 운영 중 큐레이터 실수 보호 컬럼 (Adversarial #6)
- [ ] **cron 실패 알림 채널** (Slack/email) — 30일 stale 운영자 무인지 방지
- [ ] **6개월 정확도 측정 KPI 대시보드** — Steelman 잔여 우려 대응
- [ ] **PR-2 EPSG 라이브러리 PoC 스파이크** — `go-proj` 등 후보 1개 확정 (PR-2 첫 태스크)
- [ ] **down.sql 위험성 강화** — `migrate down 003`이 운영 데이터 즉시 삭제. maintenance.md 경고 강화 (Security #6)

### PR-2 ← 현재 (build target — 8 태스크 TDD 사이클)

> 의사결정 4건 (geo-address-coords.md §15):
> - **D1**: EPSG:5179 → WGS84 = pure-Go LCC + datum-shift 무시 (CGO 0)
> - **D2**: 데이터 소스 = 행안부 도로명주소 전체 DB txt
> - **D3**: cron 주기 = 매월 5일 KST 03:00 (월간)
> - **D4**: 부분 주소 = 422 + format hint (보수적)

- [ ] **T1: migration 005 — addresses 테이블** —
      RED: `migrations/005_create_addresses.up.sql` + `down.sql`. `make migrate` → addresses 테이블 + 인덱스 생성. `SELECT 1 FROM addresses LIMIT 0` 통합 테스트.
      GREEN: 스키마 (`pnu UNIQUE`, `road_address_normalized`, `region_sido/sigungu`). gin_trgm_ops on normalized + building. region (sido, sigungu) 복합 인덱스.

- [ ] **T2: internal/geo/epsg5179.go — LCC inverse** ⏸️ 보류 — 행안부 "제공하는 주소 (도형, 좌표)" 자료 도착 후 재개. **이유**: tmp/ 사물주소.zip 의 좌표 (광양 X=224711, 강릉 X=73807) 가 EPSG:5179 X 범위 (80K~1.4M) 와 불일치 → plan v9 의 EPSG:5179 가정 자체 검증 필요. 별도 신청 자료의 좌표계 (EPSG:5179 / 5181 / 5186) 결정 후 LCC 파라미터 확정.
      RED: `internal/geo/epsg5179_test.go` 6 case (서울/부산/대구/인천/제주/대전 시청 알려진 좌표 → WGS84, ±5m 게이트). bbox 외 → ErrOutOfKorea.
      GREEN: LCC inverse (EPSG:5179 가정 파라미터: lat_0=38, lon_0=127.5, lat_1=30, lat_2=60, x_0=1000000, y_0=2000000, GRS80 a=6378137 b=6356752.3141). 좌표계 확정 후 파라미터 교체.

- [ ] **T3: internal/model/address.go (5 함수 + integration test)** —
      RED: `address_integration_test.go` (`//go:build integration`) — FindByRoadExact / FindByRoadFuzzy / FindByBuilding / FindByPNU / Upsert. fixture INSERT + truncate isolation.
      GREEN: pgx raw SQL 5 함수. ORDER BY similarity DESC, region_sido ASC, id ASC. road_address_normalized = NFC + 시도 약어 통일 (서울특별시 ↔ 서울).

- [ ] **T4: cmd/seed-juso — 행안부 txt parser + EPSG + COPY FROM** —
      RED: `cmd/seed-juso/main_test.go` mini fixture (10 row pipe-delimited txt) → addresses INSERT 정확 검증. EPSG 변환 후 좌표 ±5m.
      GREEN: 시도별 분할 입력 (17 파일), per-시도 transactional swap, 청크 단위 COPY FROM 10k row, 체크포인트 `.juso_import_state.json`. NULLIF 빈 문자열 → NULL.

- [ ] **T5: internal/proxy/places.go 확장 — addresses fallthrough** —
      RED: golden case 추가 — "서울 강남구 테헤란로 152" → 200 (source="juso") / "테헤란로 152" → 422 (부분 주소) / "역삼동 825-22" → 200 (지번).
      GREEN: `isAddressLikely(q)` 패턴 (시도 토큰 + 도로명/번지 정규식). chain 5단계로 추가 (alias_override → exact → alias → fuzzy → addresses → 422).

- [ ] **T6: docs/maintenance.md PR-2 갱신** —
      juso.go.kr 회원가입 + 다운로드 절차 (24h URL 만료 명시) + 매월 5일 KST 03:00 운영자 수동 다운로드 + `make seed-juso` 실행 + 실패 rollback 절차.

- [ ] **T7: testdata 확장 + benchmark 갱신** —
      RED: `testdata/korean_corpus.json` 100 → 130건 (도로명 20 + 지번 10). `cmd/benchmark-resolve` 측정.
      GREEN: corpus 작성 + **precision ≥ 0.85 게이트 유지**. 미달 시 alias 보강 또는 normalize 패턴 추가.

- [ ] **T8: 라우트 확장 + Makefile + atomic commit** —
      RED: `cmd/server/main.go` integration test ("서울 강남구 테헤란로 152" → 200 + 좌표). `make build/lint/test` pass.
      GREEN: Makefile `seed-juso` 타겟 1줄 + Conventional Commits — `feat(geo): 행안부 도로명주소 (EPSG:5179 pure-Go 변환)`. **사용자 명시 허락 후** atomic commit.

**Operational Checklist** (PR-2 머지 후):
- [ ] juso.go.kr 회원가입 + 다운로드 권한 신청
- [ ] 첫 다운로드 + `make seed-juso` (~30분-1시간, ~1천만 row)
- [ ] 백업 사이즈 측정 (addresses 인덱스 포함 ~3-5GB)
- [ ] cron 등록 (매월 5일 KST 03:00)
- [ ] production smoke: `curl '/v1/geo/resolve?q=서울 강남구 테헤란로 152'` → 200

## Plan 7: Almanac (KASI) — Phase A ✅ (`09fa12b` + `1c509c6` push to main)

> Spec: `.claude/plans/almanac-kasi-phase-a.md` (v3, T0 검증 + 3-reviewer 다관점 검증 통과)
> 상위 로드맵: `~/.claude/plans/majestic-percolating-cray.md`
> Goal: `/v1/almanac/lunar-date` (양→음) + `/v1/almanac/solar-date` (음→양) + `/v1/almanac/sun` (좌표/지역)
> Reuse: `holiday.go` 패턴 미러 (단 `_type=json`, `serviceName` 동적). `kma.ErrOutOfKoreaPeninsula` 가드 재사용
> Atomic single commit (사용자 명시 허락 후)

- [x] **T1: AlmanacHandler scaffold + LunarDate (양→음)** — 7 sub-test (plan v3 6 + `_type=json` 검증 1) all pass.
- [x] **T2: SolarDate (음→양) + stale/502 대칭 보강** — 7 sub-test (윤달 passthrough 포함) all pass.
- [x] **T3+T4: Sun (좌표/지역 통합) + 한반도 가드 (D9)** — 9 sub-test (OutOfPeninsula + DnYnSilentlyDropped + InvalidCoords 포함) all pass. `/sun` 단일 endpoint, `latitude+longitude` vs `location` 분기.
- [x] **T5: 라우트 등록 + router-level rate limit test** — `TestAlmanacRouteWiredWithRateLimit` (anon 5+1=429) pass. main.go 에 `/v1/almanac/{lunar-date,solar-date,sun}` 3 라우트 등록.
- [x] **T6: Integration test + build/lint/test** — `TestAlmanac_LiveKASI` 3 골든 케이스 pass (양력 2026-05-01 ↔ 음력 2026-03-15 평달 / 서울 sunrise=0537 sunset=1922 / round-trip). `make build / make lint (0 issues) / make test` 모두 pass.
- [x] **T7: Conventional Commit** — `09fa12b feat(almanac): 음력 변환 + 일출/일몰 (KASI)` push to main. Smoke test 7/7 통과 (port 28080).

**Operational Checklist**:
- [x] data.go.kr 활용 신청 (LrsrCldInfoService + RiseSetInfoService) — 2026-05-01 자동 승인 완료
- [ ] **L4 — kittypaw 스킬 패키지 측 통합 (별도 PR, 별도 레포 `../skills/packages/`)** — Plan 5 T6 선례. 본 PR 머지 ≠ 사용자 도달. 본 PR 끝난 후 별도 진행.
- [ ] **Phase C 키 신청 발의** (서울교통공사 OpenAPI) — 1~3일 리드타임. 본 plan 진행과 병렬 발의 권장 (상위 로드맵 명시 결정).
- [ ] (P1 follow-up) D10 — 입력범위(1391~2050) 검증 — 별도 issue.
- [ ] (P1 follow-up) **D4 — KASI helper 통합 refactor** — Phase B (KMA UV) 추가 시점에 holiday/almanac/weather/UV 4개 ServiceName 11 endpoint 를 한 번에 통합. plan v2 박제: `.claude/plans/d4-kasi-helper-refactor.md`. 3 reviewer (Architect/Critic/CEO) Phase 2 ITERATE — 옵션 3 (UV 동시 통합) 채택. **재개 트리거**: Phase B UV endpoint production 추가 시점.
- [x] **holiday.go envelope 검증** — `parseKMAError` 재사용으로 `resultCode != "00"` 응답이 24h 캐시되지 않도록 fix. `fetch()` 의 200 OK 분기에서 검증 → fetch error → stale fallback → 502.

## Plan 8: Smoke 3-Layer L1.A — Holiday Integration Test ✅ (`3c28f6a` push to main)

> Spec: `.claude/plans/smoke-3-layer.md` (v2, Architect/Critic 14 finding ITERATE 후 재작성. CEO 메타 비판 dispatch — 사용자 명시 결정)
> Goal: `internal/proxy/holiday_integration_test.go` 신규 + `Makefile` 분리 (DB 의존 vs API 의존 build tag split)
> Reuse: `almanac_integration_test.go` 패턴 미러 (in-process httptest + `HOLIDAY_API_KEY` env + `t.Skipf` if missing)
> 직접 동기: `3688453 fix(holiday): _type=json` (prod ~4-day 502 회귀 — 외부 KASI 실 동작 grounded 검증 layer 부재)
> 결정 D1~D7: plan v2 §"핵심 결정 7개" 모두 사용자 합의 박제. D4 = (A) bash+curl (v1 (C) 에서 다운그레이드 — Architect F1 critical)

**TDD 변형**: production code 무변경. strict RED→GREEN 사이클 N/A. **RED** = test 자체 fail (env 미설정 ∨ envelope mismatch ∨ 골든 불일치), **GREEN** = `.env` KEY 주입 후 envelope OK + 골든 일치.

**L1.A 수락 기준 (AC1~AC5)**:
- **AC1**: HTTP 200 (요청 자체 fail 시 `t.Fatalf`)
- **AC2**: `parseKMAError(body)` → `isError == false` (envelope `response.header.resultCode == "00"`)
- **AC3**: JSON unmarshal 성공 + `body.items.item` 길이 ≥ 1 (NO_DATA 와 구분)
- **AC4**: (holidays 만, 골든) `2025-01-01` `dateName` = `1월1일` 또는 `신정`
- **AC5**: `HOLIDAY_API_KEY` 부재 시 `t.Skipf("HOLIDAY_API_KEY not set")` (기존 weather/almanac 패턴 동일)

**Retry 정책** (D2):
- upstream 502/timeout → 1회 재시도 (15s timeout, 1s backoff). 두 번째도 502 → `t.Fatalf` (실 회귀)
- envelope `resultCode=22/99/SERVICETIME_OUT` (limit hit) → `t.Skipf("daily limit reached")` + CI annotation
- envelope `resultCode=03` (NO_DATA) → endpoint-specific. holidays = `t.Fatalf`, anniversaries = `t.Skipf` 허용

- [x] **T1: `internal/proxy/holiday_integration_test.go` 신규 (3 sub-test + AC1~AC5 + 골든)** — Holidays/Anniversaries/SolarTerms 3 sub-test all PASS. `fmt.Sprint(float64)` 지수 표기 micro-bug RED 발견 → `%.0f` 수정. retry closure + rate-limit Skipf 분기.

- [x] **T2: `Makefile` 분리 + build tag 격리** — 신규 `test-integration-calendar` target + `test-integration-all` umbrella alias. plan v2 D1 1단계 충실 — 기존 `integration` 태그는 model+weather+almanac 통합 유지 (L1.B/C/D 시점 분리). `make build / make lint (0 issues)` 회귀 0.

**Operational Checklist** (L1.A 머지 후):
- [ ] **L2 plan trigger by 2026-05-16 (D7 SLA, L1.A=`3c28f6a` 2026-05-02 머지 + 14일)** — `ina:plan` 으로 L2 (CI integration job, GitHub Actions secrets + fork PR silent-green 차단) plan 작성. 미이행 시 L1.A 가치 ≈ 0 (로컬 한정 검증).
- [ ] **L3 별도 plan** — fab deploy 종결부에 `bash deploy/smoke.sh` 추가 (D4 (A) bash+curl+jq). prod URL `/health` + 핵심 endpoint 1-2개 envelope 검증.
- [ ] **T0 spike** — `data.go.kr` 5 service key 별 daily limit 확인 (HOLIDAY/WEATHER/AIRKOREA + KASI 음력/일출). 결과를 plan v2 §D3 표에 record. L2 plan prerequisite.
- [x] **L1.B (airkorea 5 endpoint) + L1.C (weather UltraShort 2)** — Plan 9 ✅ (이번 세션). 외부 API 의존 endpoint cover 100%.
- [ ] **L1.D (geo HTTP layer)** ⏸️ 별도 plan — DB+API hybrid 재설계 필요. plan v2 §D6 박제. 행안부 좌표 + PR-2 T2/T3 머지 후 trigger.
- [ ] **dual-mode test harness** (L3 의 in-process httptest + HTTP client BASE_URL 분기) — L3 sibling plan 시점에 재검토 (현재 비범위).

## Plan 9: Smoke 3-Layer L1.B + L1.C — AirKorea + Weather UltraShort ✅

> Spec: Plan 8 (`smoke-3-layer.md` v2) sibling. ina:plan 생략 + 직접 ina:build (CEO 비판 학습 — template 정착됨).
> Reuse: Plan 8 L1.A `holiday_integration_test.go` 패턴 미러
> 외부 API 의존 endpoint cover 율: 50% (7/14) → **100% (14/14)**

- [x] **L1.B: `internal/proxy/airkorea_integration_test.go` 신규** — 5 sub-test (RealtimeByCity/RealtimeByStation/Forecast/WeeklyForecast/UnhealthyStations) all PASS. AirKorea 도 `returnType=json` 사용 (holiday 와 동일 quirk class) 이지만 현재 정상 수신 — silent XML fallback 회귀 catch mechanism 박제. build tag `air_integration` + `make test-integration-air` target.

- [x] **L1.C: `internal/proxy/weather_integration_test.go` 확장** — `TestUltraShort_LiveKMA` 함수 추가 (Nowcast + Forecast 2 sub-test) all PASS. 기존 `TestVillageForecast_LiveKMA` 유지 (build tag `integration` Plan 8 D1 phased 충실).

- [x] **Makefile umbrella** — `test-integration-all` = `test-integration` + `test-integration-calendar` + `test-integration-air`.

**Operational Checklist (L1.B/C 머지 후)**:
- [ ] **L1.D (Geo HTTP layer)** ⏸️ 별도 plan — Plan 8 v2 D6 ⚠ DB+API hybrid. 행안부 좌표 자료 도착 + PR-2 T2/T3 머지 후 trigger.
- [ ] **OAuth integration (10 endpoint)** ⏸️ 별도 plan — browser flow (Playwright/headless). 사용자 영향 큼.
- [ ] **/health + /discovery** ⏸️ L3 prod smoke 영역 (외부 API 의존 0, integration 부적합).

## Follow-up 일감 (별도 PR / 별도 plan 권장)



- [ ] **L4 — 신규 kittypaw skill 작성** (`../skills/packages/`) — 음력 변환 + 일출/일몰. spec 7가지 (묶음 단위 / trigger / config / 응답 포맷 / 에러 / 인증 / allowed_hosts) 결정 필요. `ina:think` → `ina:plan` 워크플로우 권장. Plan 5 T6 (weather-briefing → KMA fallback) 선례 참고.
- [ ] **Phase C — 서울교통공사 OpenAPI 활용 신청** (사용자 직접 작업) — data.go.kr 카탈로그에서 "지하철 실시간 도착정보" 검색 → 활용신청 → 자동/수동 승인 (1~3일) → `.env` `SEOUL_METRO_API_KEY` 등록. Phase B(KMA 확장) 작업과 병행 발의 권장.
- [ ] **D4 trigger 발동 — KASI helper 통합 refactor** — KASI endpoint 7개 (>5 trigger) 도달. `internal/proxy/kasi/endpoint.go` 공통 helper 추출 + `holiday.go` / `almanac.go` thin wrapper 화. 별도 `refactor(proxy):` PR. 회귀 검증 위해 기존 unit test 전부 그대로 통과해야 함.
- [ ] **Phase B 첫 endpoint — KMA 자외선 (UV)** — plan v1 박제 (`.claude/plans/kma-uv-index.md`). 3 reviewer Phase 2 ITERATE — **옵션 2 (PR-2 우선, UV 보류)** 채택. 재개 트리거: PR-2 머지 + KMA UV 키 활성화. 그 시점에 plan v2 박제 (Phase 2 ITERATE 항목 must-fix 5 + should-fix 5 반영) → ina:build.
