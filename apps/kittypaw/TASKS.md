# TASKS

KittyPaw 작업 현황. 완료된 Plan 은 Archive 에 한 줄 요약 + 커밋 해시 로 기록. 상세는 git log 참고.

---

## 🔨 In Progress

### Plan A1 — Sub-plan A: Prompt Reframe + Role Tagging + Eval 인프라
*(통합 plan: `.claude/plans/you-ai-distributed-cerf.md` 의 Sub-plan A)*

목표 (사용자 명시 성공 기준): "엔화는?" 같은 단답에 비서답게 — 되묻기 / 스킬 설치 제안 / 검색 확장 제안 / 비서 시점 응답. 케이스 특화 X 일반화. 자체 검증 loop 통과까지.

- [ ] T1 — Test fixture 5 카테고리 (vague/domain/weak_serp/framing/stale)
- [ ] T2 — LLM judge rubric (Anthropic eval define-success 가이드)
- [ ] T3 — Judge runner test infra (real LLM call, integration build tag)
- [ ] T4 — Unit tests RED (DecisionBlock / EvidenceBlock / CapabilityBlock 구조 + role tagging XML)
- [ ] T5 — engine/prompt.go QualityBlock → DecisionBlock + EvidenceBlock + CapabilityBlock 재구성 GREEN
- [ ] T6 — engine/executor.go buildSubLLMMessages 비서 시점 priming + tool_result XML wrap GREEN
- [ ] T7 — 자체 검증 + iteration (judge LLM 채점, 통과 X 시 autoresearch + 재시도 max 3 iter)
- [ ] T8 — Manual smoke + 사용자 성공 기준 통과 보고
- [ ] T9 — 단일 atomic commit (사용자 명시 허락)

**Rollback trigger**: T7 max iteration 도달 + 통과 X → Sub-plan B/C (Tool contract + Ambiguity audit) 진입 결정 사용자 confirm.

---

### Plan TI — `kittypaw tenant add <name>` interactive fallback ← 현재
*(plan: `.claude/plans/tenant-add-interactive.md`)*

목표: `tenant add` 의 flag-only UX → no-flag 시 4 단계 prompt fallback. 새 사용자 추가 마찰 제거.

- [x] T1 — `cli/tenant_wizard_test.go` 의 `TestPromptTenantSetup_AllFields` (RED)
- [x] T2 — `cli/tenant_wizard.go` 신규 + `promptTenantSetup` 구현 (GREEN)
- [x] T3 — `TestPromptTenantSetup_LocalSkipsAPIKey` — provider=local 시 api-key prompt skip (GREEN)
- [x] T4 — `cli/cmd_tenant.go` 의 `runTenantAdd` 에 `needsTenantPrompt` + interactive hook 추가
- [x] T5 — `TestNeedsTenantPrompt` (6 case truth table) + `TestPromptTenantSetup_EmptyTokenFails` + `TestMapProviderChoice`
- [x] T6 — `make build` ✅ / `make lint` ✅ 0 issues / `make test` ✅ 모든 test pass
- [ ] T6.5 — Manual smoke (`./bin/kittypaw tenant add testjane` → 4 prompt → tenant 생성 → cleanup) — 사용자 진행
- [ ] T7 — atomic commit (사용자 명시 허락 후)

이 plan 은 Plan 2 (사용자 추가 명령) 의 *MVP* — flag-only path 와 backward compat.

---

## 📋 Next Up

### Plan 2: 사용자 추가 명령 + 페어링 흐름
- `kittypaw user add <name>` (디렉토리만 생성) + 별도 페어링 명령
- per-tenant secrets/config 인프라 (Plan 1 완료) 위에서 `--user` 플래그 도입

### Plan 5: Daemon Auto-start UX
- setup → register → daemon ready 의 polling timeout 부족 (10s) — 첫 부팅 시 store migration / FTS5 init 무거움
- 단 Plan 1 amendment 후 smoke 에서는 정상 부팅됨 — priority 낮음
- 개선 방향: timeout 30s 또는 명시적 `launchctl kickstart -k`

### Plan 6: 카카오 봇 페어링 컨텍스트 인지
- 페어링 wizard 의 인증코드 발송 단계인데 봇이 "안녕하세요. 무엇을 도와드릴까요?" 일반 인사로 응답 (context-blind)
- 출처: 코드에 hardcoded 안 됨 → LLM persona 응답. wizard ↔ daemon 페어링 mode 신호 + system prompt 분기 필요

### Plan 7: Agent Hallucination 방어 — 검색 결과 신뢰성
- 2026-04-26 smoke 발견: `paw> 환율` → "1,483.50원, 2024년 12월 31일 기준" stale/fabricated. 사용자 의심 없으면 잘못된 정보 판단.
- 검색 결과 timestamp/source 추적, LLM "오늘 정보" claim 가드, stale data 명시

### Plan 8: Proactive Skill Discovery
- 2026-04-26 smoke 발견: "날씨"/"미세먼지"/"환율" 일상 쿼리에 generic 검색만. 기대: 도메인 스킬 추천
- system prompt 에 skill registry awareness + proactive recommendation routing

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

### 2026-04-26 이후

- **Secrets + Config per-tenant Alignment** — OAuth 토큰, Kakao relay URL, api_url, config.toml 모두 글로벌 `~/.kittypaw/` → per-tenant `~/.kittypaw/tenants/<id>/` 정렬. `core.LoadTenantSecrets(tenantID)` + `ConfigPath()` 의미 변경. `ChannelSpawner.Reconcile` 시그니처 보존 (load-bearing sync contract). `Server.secrets` 필드 제거로 stale-cache overwrite (web kakao_register → setup_complete data-loss path) 차단. c1a0c58 OAuth-once-per-host 의도 폐기. 5 회귀 테스트 + 3 fixture rewrite. 마이그레이션 0 — 사용자 wipe + 재설치. `8da0bd3`

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
- **Plan 8** — SharedTokenBudget (migration 016). Auto-Fix Loop은 미검증 약속이라 retire (commit `feat!: retire LLM-driven self-healing`).
- **Plan 7** — MCP Registry (connect + listTools + callTool + prompt injection).
- **Plan 6** — Memory Context → LLM Prompt Injection (facts/failures/stats 구조).
- **Plan 5** — Teach Loop (natural language → skill generation + syntax check + approve).
- **Plan 4** — Channel SessionID + Response Retry (user_id → SessionID + pending_responses 재시도).
- **Plan 3** — E2E Agent Loop Test (mock provider + sandbox round-trip + retry).
- **Plan 2** — LLM Provider Resilience (doWithRetry + jitter + SSE scanner buffer + error events).
- **Plan 1** — Skill Scheduler Wiring (sync.Once Stop + in-flight guard + SetLastRun 실패 처리).
- **LLM Test Infra** — HTTP 클라이언트 주입 functional option + OpenAI stream_options + onToken nil-guard.
