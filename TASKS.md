## Plan 24: Web Tool Quality + Agent Observe Loop ✅

Plan: `.claude/plans/web-tool-quality-observe-loop.md`
Spec: `.ina/specs/20260415-0300-think-web-tool-quality.md`

### Layer 1: Web Tool Quality

- [x] T1: htmlToMarkdown 변환기 + 테스트 — golang.org/x/net/html 토크나이저, h1-6/p/a/ul/ol/pre/code/table, script/style 무시
- [x] T2: SearchBackend 인터페이스 + DDG 어댑터 + 테스트 — engine/search.go, WebConfig
- [x] T3: TavilyBackend 구현 — POST api.tavily.com/search, Bearer auth
- [x] T4: Web.fetch markdown/title 확장 — {text, markdown, title, status} 반환, 기존 text 불변
- [x] T5: Web.search 백엔드 통합 — webSearch(ctx, query, cfg), SearchBackend 인터페이스로 전환

### Layer 2: Agent Observe Loop

- [x] T6: Observation 타입 + Agent.observe sandbox primitive — observeSignal{} 타입 sentinel, 5000자 제한
- [x] T7: Observe 루프 + 프롬프트 블록 + 회귀 테스트 — labeled observeLoop, BuildPrompt observations, AC #10 회귀

## Plan 23: Prompt Quality — Proactive Result Quality ✅

Plan: `.claude/plans/prompt-quality.md`
Spec: `.ina/specs/20260415-think-prompt-quality.md`

- [x] T1: Extract block constants from SystemPrompt (IdentityBlock, ExecutionBlock, SkillCreationBlock, MemoryBlock)
- [x] T2: Add QualityBlock — execution forcing + result quality + code-level persistence (~225 tokens)
- [x] T3: Add channelHint() for ChannelBlock — 5 channels + Telegram dispatch + unknown fallback
- [x] T4: Rewrite BuildPrompt assembly — SOUL.md first, block ordering, SystemPrompt→var
- [x] T5: Update tests + token budget regression — block presence, SOUL.md position, channel hints, 1171/1200 tokens

## Plan 22: Docs Site Go Rewrite Alignment ✅

Plan: `.claude/plans/docs-go-rewrite-alignment.md`
Spec: `.ina/specs/20260414-think-docs-go-rewrite-alignment.md`

- [x] T1: KO 메인 페이지 업데이트 (docs/index.html) — meta, JSON-LD, hero, features, tech, compare, CTA, footer
- [x] T2: EN 메인 페이지 업데이트 (docs/en/index.html) — KO 미러링 + 비교표 highlight 2행 추가
- [x] T3: JA 메인 페이지 업데이트 (docs/ja/index.html) — KO 미러링 + 비교표 highlight 2행 추가
- [x] T4: 케이스 페이지 6개 업데이트 — CLI 명령어, teach-skill Step3 제거, self-healing rollback 제거, CTA/footer
- [x] T5: 금지어 전수 검증 — grep으로 Rust/cargo/Dioxus/QuickJS/Seatbelt/Landlock/.dmg 0건 확인

## Plan 21: Permission Dialog for Chat Channels ✅

Plan: `.claude/plans/permission-dialog.md`
Spec: `.ina/specs/20260414-1200-think-permission-dialog.md`

- [x] T1: Confirmer interface + PermissionPolicy config
- [x] T2: Central permission gate in resolveSkillCall
- [x] T3: Permission audit logging
- [x] T4: Telegram Confirmer implementation
- [x] T5: dispatchLoop Confirmer injection
- [x] T6: Integration test + edge cases

## Plan 20: GitHub Registry Packages ✅

Plan: `.claude/plans/github-registry-packages.md`
Spec: `.ina/specs/20260414-github-registry-packages.md`

- [x] T1: `RegistryConfig` + `DefaultRegistryURL` — Config 구조체에 Registry 섹션 추가, DefaultConfig 기본값 설정, `DefaultRegistryURL` 상수 추가
- [x] T2: `fetchToFile` + `DownloadPackage` 멀티파일 — 헬퍼 추출, 3파일 개별 다운로드 (package.toml/main.js 필수, README.md 선택), 실패 시 tmpDir 정리
- [x] T3: `FilterEntries` + `SearchEntries` — 순수 필터 함수 + FetchIndex 조합 메서드
- [x] T4: 테스트 — 기존 DownloadSSRFDefense 수정, DownloadMultiFile, RequiredFile404, OptionalReadme404, FilterEntries 테이블 드리븐, SearchEntries, NotFoundID
- [x] T5: CLI `search` + `info` + `install` 분기 — newPkgSearchCmd, newPkgInfoCmd, newPkgInstallCmd 로컬/레지스트리 분기, registryClient 헬퍼
- [x] T6: 빌드 검증 — `go build` + `go vet` + `go test ./...` 통과

## Plan 19: Claude API Prompt Caching ✅

Plan: `.claude/plans/prompt-caching.md`

- [x] T1: `TokenUsage` cache 필드 추가 + 실패 테스트 작성 (JSON parse + backward compat)
- [x] T2: system prompt → content blocks + `cache_control` 포맷 변경 + 테스트
- [x] T3: JSON 응답에서 cache metrics 파싱 → T1 테스트 통과
- [x] T4: SSE 스트림에서 cache metrics 파싱 + 테스트
- [x] T5: `go test ./...` 전체 통과 + `TokenUsage{}` literal 누락 검증

## Plan 18: CLI Command Completion ✅

Plan: `.claude/plans/cli-completion.md`
Spec: `.ina/specs/20260414-2350-think-cli-completion.md`

- [x] T1: Client methods — 17 new methods in `client/client.go` (Enable/Explain Skill, Suggestions CRUD, Fixes, Reflection, Evolution, Channels) + array response wrapping
- [x] T2: `skills enable` + `skills explain` subcommands
- [x] T3: `suggestions` (list/accept/dismiss) + `fixes` (list/approve) + `reflection` (list/approve/reject/clear/run/weekly-report) groups
- [x] T4: `persona evolution` (list/approve/reject) + `memory search` + `channels list` + `reload` commands + root registration
- [x] T5: Build verification — `go build` + `go vet` + `go test ./...` all pass

## Plan 1: Skill Scheduler Wiring ✅

Plan: `.claude/plans/skill-scheduler-wiring.md`

- [x] Guard `Scheduler.Stop()` with `sync.Once` — prevent double-close panic
- [x] Add in-flight guard (`sync.Map`) — prevent concurrent runs of same skill
- [x] Handle `SetLastRun` failure for one-shot — skip execution if dedup record fails
- [x] Wire Scheduler into `server.Server` — start with cancelable ctx, stop on shutdown
- [x] Write `engine/schedule_test.go` — parseCronInterval, isDue, backoff, concurrency

## Plan 2: LLM Provider Resilience ✅

Plan: `.claude/plans/llm-resilience.md`

- [x] Add jitter to Claude `doWithRetry` backoff + ctx cancellation test
- [x] Add `doWithRetry` to OpenAI provider (429/503 retry + jitter) + tests
- [x] Fix scanner buffer in both `parseSSEStream` (64KB→1MB max) + overflow test
- [x] Handle SSE error events in Claude `parseSSEStream` + tests (0 tokens, N tokens)
- [x] Handle SSE error events in OpenAI `parseSSEStream` + tests (0 tokens, N tokens)

## Plan 3: E2E Agent Loop Test ✅

Plan: `.claude/plans/e2e-agent-loop.md`

- [x] Mock provider + test helper + TestE2ESimpleReturn
- [x] TestE2ESkillCall (Storage round-trip via sandbox → resolveSkillCall)
- [x] TestE2EErrorRetry (JS error → engine retry → success)

## Plan 12: Workspace Hardening ✅

Plan: `.claude/plans/workspace-hardening.md`
Spec: `.ina/specs/20260413-2230-think-workspace-hardening.md`

- [x] Store: Workspace CRUD + ListWorkspaceRootPaths + TOML seed + tests
- [x] Engine: Fix isPathAllowed (parent symlink walk) + resolveForValidation + symlink tests
- [x] Engine: Session.AllowedPaths atomic cache + RefreshAllowedPaths + wiring
- [x] Engine: File size limit (10MB read) + error contract (Go error, not JSON) + tests
- [x] Sandbox: Throw JS exception on resolver error + test
- [x] Server: Workspace CRUD API (GET/POST/DELETE) + api_workspace.go + route wiring
- [x] E2E: File access gating integration test (mock LLM → File.read → isPathAllowed)

## Plan 4: Channel SessionID + Response Retry ✅

- [x] Map user_id to SessionID in all channel ChatPayloads (KakaoTalk, Telegram, Slack, Discord)
- [x] Add pending_responses SQLite table + store CRUD (enqueue, dequeue, retry, cleanup)
- [x] Wire retry loop into serve command (30s poll, exponential backoff, 24h expiry)
- [x] Tests for SessionID mapping and pending response lifecycle

## Plan 5: Teach Loop — Skill Generation from Natural Language ✅

Plan: `.claude/plans/teach-loop.md`

- [x] Pure pipeline helpers + tests (stripFences, slugify, detectPermissions, inferTrigger)
- [x] syntaxCheck + tests (goja parse validation, 64KB cap)
- [x] generateCode + TEACH_PROMPT + tests (mock LLM, SkillRegistry-driven prompt)
- [x] HandleTeach orchestration + tests (full pipeline, edge cases)
- [x] ApproveSkill + tests (cron validation, SaveSkill roundtrip)
- [x] Wire CLI entry points (commands.go + main.go)
- [x] Wire API endpoints (server/api.go — structured JSON response)

## Plan 6: Memory Context → LLM Prompt Injection ✅

Plan: `.claude/plans/eager-wondering-quasar.md`

- [x] Add `MemoryContextLines()` to Store (facts cap 20, failures 24h cap 5, today's stats)
- [x] Sanitize user-supplied values for prompt injection and token explosion prevention
- [x] Wire memory context loading outside retry loop in `session.go`
- [x] Tests: empty, populated, partial, cap, 24h window, sanitization

## Plan 7: MCP Registry ✅

Plan: `.claude/plans/mcp-registry.md`

- [x] MCPRegistry scaffold — types, NewRegistry, ValidateConfig, IsConnected + tests (AC7, AC8)
- [x] MCPRegistry Connect + ListTools + AllTools + tests (AC2, AC5)
- [x] MCPRegistry CallTool + Shutdown + tests (AC1, AC6)
- [x] Skill metadata (Mcp.listTools) + executeMCP implementation + tests (AC1, AC2)
- [x] MCP tools prompt injection (BuildMCPToolsSection) + tests (AC3, AC4)
- [x] Wiring — Session, Server, CLI + nil-safety tests + verification

## Plan 8: SharedTokenBudget + Auto-Fix Loop ✅

Plan: `.claude/plans/tier1-features.md` (Plan 1)
Spec: `.ina/specs/20260413-2100-think-tier1-features.md`

- [x] Migration 016 — `ALTER TABLE skill_schedule ADD COLUMN fix_attempts` + store methods (`ClaimFixAttempt`, `ResetFixAttempts`, `GetFixAttempts`)
- [x] `engine/budget.go` — `SharedTokenBudget` (sync/atomic CAS) + remove old `TokenBudget` from orchestration.go + `budget_test.go`
- [x] Store updates — `RecordFix` 5th param `applied bool` + `ApplyFix` stale check + `GetFix` + update callers + tests
- [x] `engine/auto_fix.go` — `AttemptAutoFix` (generateCode direct call, manual TeachResult, autonomy gate) + `buildFixPrompt`
- [x] Wire auto-fix into `schedule.go` `runSkill()` failure path — DB-level CAS via `ClaimFixAttempt`, package skip, disable after 2x
- [x] `engine/auto_fix_test.go` + `budget_test.go` — 13 tests: concurrent CAS, autonomy gate, stale check, cap, supervised mode

## Plan 9: Agent Delegation ✅

Plan: `.claude/plans/tier1-features.md` (Plan 2)

- [x] Rewrite `OrchestrateRequest` — JSON PM format + `json.Unmarshal` + wire `SharedTokenBudget`
- [x] Implement `executeDelegate` in executor.go — validate, load SOUL.md (fallback to description), generate, budget check
- [x] Parallel fan-out with `errgroup` — per-child timeout, Depth+1, budget exhaustion cancels remaining
- [x] PM Synthesize — all success / partial fail / all fail modes
- [x] Wire `OrchestrateRequest` gate into `session.go` `runAgentLoop()` + pass budget through Session
- [x] `engine/orchestration_test.go` — 12 tests: JSON parse, depth, synthesize, SOUL.md fallback, budget, disabled config

## Plan 10: Reflection System ✅

Plan: `.claude/plans/tier1-features.md` (Plan 3)

- [x] Store methods — `ListTopicPreferences`, `DeleteExpiredReflection`, `DeleteUserContextPrefix`
- [x] `engine/reflection.go` — `RunReflectionCycle`, `IntentHash`, `BuildWeeklyReport`
- [x] `engine/evolution.go` — `TriggerEvolution`, `ApproveEvolution`, `RejectEvolution`
- [x] `server/api_reflection.go` — 6 endpoints (list, approve, reject, clear, run, weekly-report)
- [x] `server/api_persona.go` — 3 endpoints (list evolution, approve, reject)
- [x] Wire reflection cron into scheduler — built-in periodic task, configurable schedule
- [x] Tests — reflection cycle, JSON skip, TTL, weekly report, evolution approve/reject, zero messages

## Plan 11: Package System ✅

Plan: `.claude/plans/tier1-features.md` (Plan 4)
Note: CEO 권장 — Plans 8-10 배치 후 별도 PR로 진행

- [x] `core/package.go` — type definitions + `LoadPackageToml` + ID validation
- [x] `core/secrets.go` — `SecretsStore` (CRUD, 0600 perms, log masking)
- [x] `core/package_manager.go` — Install, Uninstall, List, Load, GetConfig, SetConfig, LoadChain
- [x] `core/registry.go` — `RegistryClient` (FetchIndex, DownloadPackage, SSRF defense)
- [x] Wire packages into scheduler + chain execution (prev_output, model priority, can_disable=false)
- [x] Cron upgrade (`robfig/cron/v3`) + CLI commands (`kittypaw packages {install,uninstall,list,search,config,run}`)
- [x] Tests — TOML parse, ID validation, secrets masking, chain loading, registry SSRF, schedule integration

## Plan 13: Vision / Image Skills ✅

Plan: `.claude/plans/vision-image-skills.md`
Spec: `.ina/specs/20260413-2055-think-vision-image-skills.md`

- [x] Provider resolution + image download helper + tests
- [x] Vision.analyze — 3 providers (Claude, OpenAI, Gemini) + arg validation + tests
- [x] Image.generate — 2 providers (OpenAI, Gemini) + Claude error + tests
- [x] Edge cases + acceptance criteria verification

## Plan 14: Channel Hot-Reload ✅

Plan: `.claude/plans/channel-hot-reload.md`
Spec: `.ina/specs/20260413-2239-think-channel-hot-reload.md`

- [x] ChannelSpawner core — `TrySpawn`, `Stop`, `GetChannel`, `List` + stub channel + unit tests (AC1, AC2, AC4)
- [x] Reconcile + ReplaceSpawn + StopAll — diff algorithm, serialized reconcile, parallel StopAll, best-effort errors + tests (AC5, AC6)
- [x] Server integration — `spawner` field, `eventCh`, `StartChannels`, `dispatchLoop`, `retryPendingResponses` (no-drop semantics) + shutdown wiring
- [x] API + onboarding — `handleReload` Reconcile + `GET /api/v1/channels` + `handleSetupTelegram` TrySpawn + `handleSetupComplete` Reconcile (AC1, AC3)
- [x] main.go migration — Remove channel startup (119-143), dispatch loop (146-190), `retryPendingResponses` (834-880) + wire `srv.StartChannels`

## Plan 15: Persona Preset System ✅

Plan: `.claude/plans/persona-preset-system.md`
Spec: `.ina/specs/20260413-2330-think-persona-preset-system.md`

- [x] T1: core/profile.go — Profile struct + presets + LoadProfile + EnsureDefaultProfile + tests
- [x] T2: core/profile.go — ApplyPreset + DetectDirty + PresetStatus + tests
- [x] T3: engine/prompt.go — BuildPrompt takes *core.Profile + tests
- [x] T4: engine/session.go — ResolveProfileName + orchestration.go loadSOUL refactor + tests
- [x] T5: engine/executor.go — Profile.switch via user_context DB + tests
- [x] T6: server/api_profile.go + cmd/kittypaw persona + init integration

## Plan 16: Workspace Indexer v1 (FTS5 Full-text Search) ✅

Plan: `.claude/plans/workspace-indexer.md`
Spec: `.ina/specs/20260414-2200-think-workspace-indexer.md`

- [x] T1: Migration + Store — `017_workspace_fts.sql` (workspace_files + workspace_fts) + Store CRUD (Upsert, Delete, Search, Aggregate, DeleteStale) + tests
- [x] T2: Indexer core — `engine/indexer.go` Indexer interface + FTS5Indexer (Index, Remove, file walk, binary detection, exclude patterns, chunked tx) + tests
- [x] T3: Search + Stats + Reindex — FTS5 query + snippet/line extraction + StatsOptions + upsert-based reindex + tests
- [x] T4: Skill metadata + Executor — `core/skillmeta.go` search/stats/reindex entries + `executor.go` early dispatch + AllowedPaths post-filter + tests
- [x] T5: Session + Server wiring — Session.Indexer field + server.New indexer creation + async startup indexing + API triggers (create→Index, delete→Remove) + tests

## Plan 17: Thin Client Architecture (CLI → Daemon HTTP/WS)

Plan: `.claude/plans/thin-client.md`
Spec: `.ina/specs/20260414-2330-think-thin-client.md`

- [x] T1: GET /health + Config.Server.Bind + Executions skill 필터 — server /health 라우트 + handleHealth + ServerConfig.Bind 필드 + handleExecutions skill param + Client.Health() + Client.Executions(skill, limit) + tests
- [x] T2: DaemonConn — `client/daemon.go` DaemonConn struct + Connect (PID flock + 소유자 검증 + health 폴링 200ms×50 + auto-start) + NewDaemonConn(remoteURL) + WebSocketURL() + tests
- [x] T3: WebSocket 스트리밍 + 누락 Client 메서드 — `client/ws.go` StreamChat (nhooyr.io/websocket, OnToken/OnDone/OnError) + ProfileList/ProfileActivate/TeachApprove + tests
- [x] T4: CLI 단순 커맨드 마이그레이션 — connectDaemon() 헬퍼 + status/agent list/log/persona list·apply/skills list·disable·delete → DaemonConn + Client
- [x] T5: CLI 복잡 커맨드 + 최종 정리 — chat (WS 스트리밍) + run (RunSkill, dry-run 로컬) + teach (2단계) + bootstrap/openStore CLI 제거 + go build/vet 검증

### v2 후보 (향후 검토)

- [ ] bleve 백엔드 전환 — camelCase 토크나이징, 한국어 형태소, BM25 랭킹 필요 시
- [ ] 시맨틱 검색 — LLM ranking / 임베딩 벡터 — 스코프 미확정
- [ ] 실시간 파일 감시 — fsnotify 또는 File.write 훅으로 인덱스 자동 갱신
- [ ] File.summary(path) — LLM 기반 파일 요약 + 캐시
- [ ] Permission Checker — 에이전트 파일 접근 규칙 엔진 — 스코프 미확정

## Backlog: CLI 온보딩 + 채팅 모드 🔴 높음 / 작업량 중

온보딩은 브라우저(Web UI)와 CLI 양쪽 모두 가능해야 한다.
CLI 온보딩 완료 후 곧바로 대화형 채팅 상태(REPL)로 진입해야 한다.

- [ ] CLI 온보딩 플로우 — `kittypaw init` 또는 `kittypaw setup`에서 LLM/채널/워크스페이스 설정
- [ ] 온보딩 완료 후 채팅 REPL 자동 진입 — `kittypaw chat` 상태로 전환
- [ ] 브라우저 온보딩과 동일한 설정 결과 보장 (config.toml + DB 상태 일치)

## Backlog: 사용자 프로필 시스템 (kittypaw.yml) 🔴 높음 / 작업량 중

설치 시 기본 사용자 프로필은 `kittypaw.default.yml`.
사용자를 추가할 때 `kittypaw.{username}.yml` 형태로 프로필 파일을 생성한다.

- [ ] `kittypaw.default.yml` 기본 프로필 — 설치 시 자동 생성, 스키마 정의
- [ ] `kittypaw.{username}.yml` 사용자 추가 — CLI/API로 프로필 CRUD
- [ ] 프로필 전환 — 활성 사용자 선택 + 세션별 프로필 바인딩

## Backlog: MoA (Mixture of Agents) 🟡 중간 / 작업량 중

KittyPaw `skill_executor/moa.rs` 대응 — 복수 모델 병렬 질의 + 결과 합성.
config `[[models]]`에 등록된 모든 모델에 동시 질의 후, 기본 모델이 응답을 종합하여 최종 답변 생성.
goroutine 기반 병렬 실행. KittyPaw 원본이 ~100줄 수준으로 비교적 가벼움.

- [ ] `Moa.query` 스킬 — 복수 모델 병렬 질의 (goroutine fan-out)
- [ ] 합성 레이어 — 기본 모델이 복수 응답을 종합
- [ ] sandbox wrapper + executor 등록

## Plan 19: Skill Install System ✅

Plan: `.claude/plans/skill-install.md`
Spec: `.ina/specs/20260414-think-skill-install.md`

- [x] T1: Foundation — Skill provenance fields (SourceURL/SourceHash/SourceText) + PackagesDirFrom(baseDir) + PackageManager baseDir threading + RegistryEntry Hash field
- [x] T2: SKILL.md frontmatter parser — `core/skillmd.go` ParseSkillMd (YAML frontmatter → SkillMdMeta + body) + gopkg.in/yaml.v3 dependency
- [x] T3: GitHub source resolver — `core/github.go` ParseGitHubURL + ResolveGitHubSource (raw URL probing main→master, HTTPS only, redirect block, symlink reject)
- [x] T4: Install orchestrator — `core/installer.go` Install() + DetectSourceFormat + SHA256 verify + cleanup-on-failure + route to PackageManager or teach/prompt pipeline
- [x] T5: Prompt mode executor — `engine/executor.go` buildPromptModeCall + FilterToolsByPermissions + single-turn enforcement + Format branch in skill execution
- [x] T6: CLI `kittypaw install` + API — newInstallCmd + handleInstall endpoint + Client.Install + SkillInstallConfig (md_execution_mode) + mode selection prompt
- [x] T7: CLI `kittypaw search` + API — newSearchCmd + handleSearch endpoint + Client.Search + SearchEntries filter

## Backlog: Desktop GUI 🟢 낮음 / 작업량 대

KittyPaw `kittypaw-gui/` 대응 — Tao/Wry/Muda 네이티브 데스크톱 앱.
Go에서는 Wails 또는 Fyne 등으로 대체 가능. CLI + Web API로 충분한 현 시점에서 우선순위 낮음.

- [ ] 프레임워크 선정 (Wails vs Fyne vs 기타)
- [ ] 데몬 자동 실행 + WebView 기반 채팅 UI
- [ ] 번들 패키지 첫 실행 자동 설치
