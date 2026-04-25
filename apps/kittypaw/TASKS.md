# TASKS

KittyPaw 작업 현황. 완료된 Plan 은 Archive 에 한 줄 요약 + 커밋 해시 로 기록. 상세는 git log 참고.

---

## 🔨 In Progress

### Plan 1: Secrets per-tenant Alignment

**Spec**: `.claude/plans/secrets-per-tenant-alignment.md` (Phase 1+2 합의 완료)
**Bug**: `kittypaw login` 이 글로벌 `~/.kittypaw/secrets.json` 에 쓰지만 daemon 은 `~/.kittypaw/tenants/<id>/secrets.json` 에서 읽음. 새 토큰이 daemon 에 안 보임.
**결정**: 모든 시크릿 (API OAuth + Kakao + api_url) per-tenant 정렬. 마이그레이션 0. 사용자가 `~/.kittypaw` 삭제 후 재설치.
**Atomic**: 단일 commit (시그니처 변경 + write site 일관 이동 — 부분 적용 시 빌드 깨짐).

#### TDD 사이클

- [x] **T1+T2: 헬퍼 + 회귀 테스트 동시 추가** (RED)
  - `core/secrets.go` 에 `LoadTenantSecrets(tenantID string)` 추가 — `os.MkdirAll(filepath.Dir(path), 0o700)` 내장
  - 단위 테스트 (`core/secrets_test.go`): 임시 ConfigDir 환경에서 경로/디렉토리 생성 검증
  - 회귀 테스트 3개:
    - `TestAPIToken_PerTenant_LoginToDaemonRoundTrip` (server) — 글로벌 `secrets.json` 부재 가드 포함
    - `TestKakao_PerTenant_InjectFromTenantSecrets` (server)
    - `TestSecretsStore_MultiNamespace_Coexist` (core) — telegram + api 키 공존 검증
  - Verify: `go test -run TestAPIToken_PerTenant ./server/...` 가 **assertion fail** (compile OK)

- [x] **T3: Write site 5+ 위치 일괄 이동** (GREEN)
  - `cli/cmd_login.go:36` → `LoadTenantSecrets("default")`
  - `cli/init_wizard.go:481` (Kakao wizard — api_url + wss URL 두 키)
  - `server/api_setup.go:229`
  - `cli/main.go:243-247` (setup 완료 시 api_url write — Architect 발견)
  - `server/api_setup.go:537` (server-side setup write — Architect 발견)
  - `cli/main.go:747`, `:818` (read 도 per-tenant)
  - Verify: T1+T2 회귀 테스트 GREEN. `make build && go vet ./...` 통과.

- [x] **T4: `InjectKakaoWSURL` 시그니처 변경 (CEO trim — Reconcile 보존)**
  - `core/config.go:285` — 시그니처 `(tenantID string, channels []ChannelConfig)`. 내부에서 `LoadTenantSecrets(tenantID)`. 글로벌/`DefaultAPIServerURL` 폴백 제거.
  - `server/spawner.go:180` 호출만 `core.InjectKakaoWSURL(tenantID, configs)` 인자 변경. **`Reconcile` 시그니처 변경 없음** — sync contract 보호.
  - Verify: `make build && go vet ./...`. mock spawner stub (`TestHandleReload_WaitsForReconcile`, `TestAutoEntryNoRace -race -count 50`) 변경 없이 통과해야.

- [x] **T5: 기존 테스트 정리 (assertion 약화 금지)** — 변경 불필요 (모든 기존 테스트가 `LoadSecretsFrom` 임시 path 사용, production `LoadSecrets()` 와 무관하게 자체 격리)
  - `api_setup_fallback_test.go`, `init_wizard*_test.go` 등이 글로벌 가정이면:
    - **write**: per-tenant 동작 pin 하도록 본문 재작성, OR
    - **delete**: 1줄 코멘트와 함께 (`// removed: api_url global fallback no longer exists`)
  - 금지: assert 라인 슬쩍 수정 / `NotEqual` 류로 약화
  - Verify: `make test` 통과 + diff 자기 검토

- [x] **T6: 문서 + Grep AC 검증 + lint**
  - CLAUDE.md "API Token Management" 섹션:
    - `secrets.json` 경로 → per-tenant 명시
    - c1a0c58 의 "intentionally global" 의도 폐기 + 새 의도 ("per-tenant by design — multi-user on shared host")
    - `InjectKakaoWSURL(tenantID, channels)` 시그니처 명시
  - AC#3 grep 3차:
    - `rg 'core\.LoadSecrets\b' --type=go -g '!*_test.go' -g '!core/secrets.go' -g '!competitors/'` → 0건
    - `rg '"kittypaw/core"' --type=go -g '!*_test.go' -l | xargs rg '\bLoadSecrets\b' 2>/dev/null` non-comment 0건 (alias import)
    - `rg 'secrets\.Set\("kittypaw-api"' --type=go -g '!*_test.go' -g '!competitors/'` 컨텍스트가 모두 per-tenant store
  - `make lint` 통과
  - Verify: AC#1, #3, #5 만족

- [x] **T6.5: Plan 1 amendment — config.toml per-tenant** (smoke 중 발견된 critical regression)
  - `core/config.go:ConfigPath()` 의미를 default tenant 위치로 변경 + docstring
  - `cli/main.go` setup cfgPath 통일 — `tenants/default/` 명시 생성
  - 3 server fixture rewrite (`api_reload_race_test.go`, `api_reload_sync_test.go`, `api_reload_validation_test.go`)
  - Verify: `make test && make lint && go vet ./...` 모두 통과

- [x] **T7: 수동 smoke** (사용자 직접 검증 — 2026-04-26)
  - `~/.kittypaw` wipe → setup → daemon 자동 시작 → chat REPL 정상 진입
  - fs 검증: `tenants/default/{config.toml,secrets.json}` 존재, 글로벌 `~/.kittypaw/{config.toml,secrets.json}` 부재
  - 대화 정상 (안녕, 자기소개, 검색)

- [ ] **T8: 단일 commit** ← 현재
  - 메시지: `fix(secrets+config): align tenant data to per-tenant storage`

### Plan 2 (대기): 사용자 추가 명령 + 페어링 흐름
- 위 Plan 1 완료 후 진입. `kittypaw user add <name>` (디렉토리만 생성) + 별도 페어링 명령.

### Plan 3 (DEFERRED): `Family` → `Shared` 리네이밍
- Spec: `.claude/plans/family-to-shared-rename.md` (Phase 2 ITERATE 8+ 사항 보존됨)
- Plan 1, 2 종료 후 가치 재판단 (Architect steelman: "naming taste vs 481-occurrence 단일 PR cost")

### Plan 5 (backlog): Daemon Auto-start UX
- 현재 setup → register → 10s timeout polling. 첫 부팅 (store migration / FTS5 init) 이 무거우면 timeout. 단 plan 1+amendment 후 첫 smoke 에서는 정상 부팅 — priority 낮아짐.
- 개선 방향: timeout 30s, 또는 명시적 `launchctl kickstart -k`, 또는 register 직후 binary 직접 spawn.

### Plan 6 (backlog): 카카오 봇 페어링 컨텍스트 인지
- 페어링 wizard 가 인증코드 발송 단계인데 봇이 "안녕하세요. 무엇을 도와드릴까요?" 일반 인사로 응답. context-blind. 페어링 mode 시 봇이 "인증 코드를 입력해 주세요" 로 분기 필요.
- 출처: 코드에 hardcoded 안 됨 → LLM 응답 가능성. persona/system prompt 분기 + wizard ↔ daemon 페어링 신호 필요.

### Plan 7 (backlog, NEW): Agent Hallucination 방어 — 검색 결과 신뢰성
- 2026-04-26 smoke 중 발견: `paw> 환율` → "1,483.50원, 2024년 12월 31일 기준" stale/fabricated 답변. 사용자가 의심 안 하면 잘못된 정보로 판단할 위험.
- 검색 결과의 timestamp/source 추적, LLM 의 "오늘 정보" claim 가드, agent 가 stale data 명시.

### Plan 8 (backlog, NEW): Proactive Skill Discovery
- 2026-04-26 smoke 중 발견: "날씨"/"미세먼지"/"환율" 같은 일상 쿼리에 봇이 generic 검색만 함. 사용자 기대: "기상청 스킬 설치하실래요?" 같은 도메인 적합 스킬 추천.
- agent system prompt 에 skill registry awareness + proactive recommendation routing.

---

## 🎯 Active Backlog

### 🟢 장기 / 스코프 미확정

- [ ] **Permission Checker** — 에이전트 파일 접근 규칙 엔진. 현재 `isPathAllowed` + `AllowedPaths` 넘어서는 룰 엔진 스코프 미정.
- [ ] **bleve 백엔드 전환** — camelCase 토크나이징, 한국어 형태소, BM25 랭킹 필요 시점에.
- [ ] **시맨틱 검색** — LLM ranking / 임베딩 벡터. 스코프 미정.
- [ ] **Desktop GUI** — Wails vs Fyne 선정, 데몬 자동 실행 + WebView 채팅 UI. CLI + Web API 로 충분한 현 시점 우선순위 낮음.

---

## 📦 Deferred (미착수)

- [ ] **Skill Gallery 웹 UI + `SkillSetting` 저장** — 현재 `kittypaw install` CLI 만 존재 (Plan 19). 웹 갤러리 + 동적 settings 폼 + SQLite 암호화 저장이 미구현.
- [ ] **Telegram Pair Code** — `FetchTelegramChatID` 의 daemon race + multi-user 신원 탈취. Kakao pair-code 패턴 이식 예정. tenant==user 모델 확정 후 재검토 가치 있음.

---

## ✅ Archive

완료 순 (최신 → 과거). 커밋 해시 첨부.

### 2026-04-20 이후

- **File.summary + llm_cache** — `File.summary(path, options?)` JS skill: 워크스페이스 파일 LLM 요약 + generic `llm_cache` 테이블 캐시. `engine/summary.go` QuerySummary() + singleflight 미스 중복 제거 + `ON CONFLICT DO UPDATE` UPSERT (force_refresh 가 오염된 row 덮어쓸 수 있음) + prompt-injection 3-layer 방어 (system prompt + fenced markers + `sanitizeBasename`) + charge-after-response 예산 회계 + `RemoveFile` GC 캐스케이드. 19 unit tests. `349d77a`
- **MoA (Mixture of Agents)** — `Moa.query(prompt, options?)` JS skill: `[[models]]` 병렬 fan-out + Default 모델 합성. `engine/moa.go` QueryMoA() + sync.WaitGroup 변형 (partial failure 관용) + per-model ctx timeout + maxModels=5 가드 + 후보 1개 시 합성 skip + `SharedTokenBudget` 회계. 9 unit tests. `513b7ca`
- **Plan 27 Follow-up 2** — Indexer v2 overflow 자동 복구: `fsnotify.ErrEventOverflow` 감지 → 500ms debounce + 30s backoff 로 전 workspace full reindex + `OverflowCount` / `RecoveryCount` atomic 관측. `9cfce19`
- **Plan 27 Follow-up** — Indexer v2 hardening (bundle 1+2): dir-remove FTS cascade + watcher partial-add visibility. `e575f53`
- **Plan 27** — Workspace Indexer v2: fsnotify live filesystem watching + FTS5 incremental update. `8c45a4f`
- **Setup → Chat Auto-Entry** (Plan 26) — `kittypaw setup` 완료 시 TTY 에서 chat REPL 자동 진입 + daemon hot-reload. `814cc89` + `74acdaf`(/reload validation)
- **Tenant Remove** — `kittypaw tenant remove`: LIFO 드레인 → family config scrub → `.trash/` 이동 + BotFather 경고. `4ee9c95`
- **Family Init Wizard** — `kittypaw family init`: 인터랙티브 CLI, N명 일괄 온보딩, idempotent. `4007b41`
- **Multi-user Blockers** — MB1 tenant ID regex 완화, MB2/MB3 는 `tenant==user` 확정으로 revert. `e24cd9e` + `aedf04a`(revert)

### Plan 25 — Family Multi-Tenant (macOS single daemon, 7 personal + 1 family)

- **Plan A** — multi-tenant routing foundation (Event.TenantID, ChannelSpawner keying, fail-fast 중복 탐지). `8b3860a`
- **Plan B** — family tenant: cross-tenant `Share.read` + `Fanout.send/broadcast`. `a62075b` + `26ea597`(gate + dispatch)
- **Plan C** — operations: tenant health + panic isolation (`57fe75a`), `kittypaw tenant add` CLI (`4fae3a3`), admin RPC hot-activate (`eb26ec7`), E2E demos (`aa7f9cb`)
- Plan B→C 이월 — tenant fields wire + legacy migration activation. `83a986b`

### 2026-04-18 이전 (주제별)

- **Package Context Declaration** — Package 에 Context 필드 + UserConfig + event-in-context + locale. `18cac99` + `fd71ab0`
- **Discovery Endpoint Migration** — `/discovery` 로 api_base_url/relay_url/skills_registry_url 3 개 topology 동적 해석.
- **Relay Rust Rewrite** — KakaoTalk relay TS→Rust (axum + SQLite, self-hosted single binary).
- **Plan 24** — Web Tool Quality (HTML→Markdown, SearchBackend DDG/Tavily) + Agent Observe Loop.
- **Plan 23** — Prompt Quality: SystemPrompt 블록 분리 + QualityBlock + channelHint + 토큰 예산.
- **Plan 22** — Docs site Go rewrite alignment (docs/, docs/en/, docs/ja/ 전면 갱신).
- **Plan 21** — Permission Dialog (Confirmer interface + Telegram inline keyboard + audit log).
- **Plan 20** — GitHub Registry Packages (RegistryConfig + 3파일 다운로드 + SSRF 방어).
- **Plan 19** — Skill Install System (SKILL.md + GitHub resolver + SHA256 + prompt/native 모드).
- **Plan 18** — CLI Command Completion (suggestions / fixes / reflection / persona / memory / channels / reload 17개 메서드).
- **Plan 17** — Thin Client Architecture (CLI → Daemon HTTP/WS + DaemonConn flock + WebSocket 스트리밍).
- **Plan 16** — Workspace Indexer v1 (FTS5 full-text search + File.search/stats/reindex).
- **Plan 15** — Persona Preset System (`core.Profile` + presets + DetectDirty + preset status).
- **Plan 14** — Channel Hot-Reload (ChannelSpawner + Reconcile + no-drop dispatch).
- **Plan 13** — Vision / Image Skills (Claude/OpenAI/Gemini vision + OpenAI/Gemini image gen).
- **Plan 12** — Workspace Hardening (Workspace CRUD + isPathAllowed + 10MB 제한 + symlink walk).
- **Plan 11** — Package System (core/package.go + secrets.json + registry + cron + CLI).
- **Plan 10** — Reflection System (topic preferences + weekly report + evolution approve/reject).
- **Plan 9** — Agent Delegation (OrchestrateRequest JSON + errgroup fan-out + PM synthesize).
- **Plan 8** — SharedTokenBudget + Auto-Fix Loop (migration 016 + atomic CAS + 2x retry cap).
- **Plan 7** — MCP Registry (connect + listTools + callTool + prompt injection).
- **Plan 6** — Memory Context → LLM Prompt Injection (facts/failures/stats 구조).
- **Plan 5** — Teach Loop (natural language → skill generation + syntax check + approve).
- **Plan 4** — Channel SessionID + Response Retry (user_id → SessionID + pending_responses 재시도).
- **Plan 3** — E2E Agent Loop Test (mock provider + sandbox round-trip + retry).
- **Plan 2** — LLM Provider Resilience (doWithRetry + jitter + SSE scanner buffer + error events).
- **Plan 1** — Skill Scheduler Wiring (sync.Once Stop + in-flight guard + SetLastRun 실패 처리).
- **LLM Test Infra** — HTTP 클라이언트 주입 functional option + OpenAI stream_options + onToken nil-guard.
