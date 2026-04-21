# TASKS

KittyPaw 작업 현황. 완료된 Plan 은 Archive 에 한 줄 요약 + 커밋 해시 로 기록. 상세는 git log 참고.

---

## 🔨 In Progress

_(없음)_

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
