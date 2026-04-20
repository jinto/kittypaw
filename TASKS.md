## Tenant Remove (α) ✅

Plan: `.claude/plans/tenant-remove.md`
Spec: `.ina/specs/20260420-0455-auto-tenant-remove.md`

Goal: `kittypaw tenant remove <name>` — 한 명령으로 tenant 안전 해제 (daemon 비활성화 → family config 정리 → `.trash/` 이동 → BotFather 경고). LIFO 순서로 실패 시 tenant 는 그대로 실행 가능 상태 유지.

- [x] TR1. `server/server.go` 에 `tenantDeps map[string]*TenantDeps` 필드 추가 + `AddTenant` 성공 경로에서 `s.tenantDeps[t.ID] = td` 저장. 테스트: `TestServer_AddTenant_StoresDeps`.
- [x] TR2. `server/admin.go:RemoveTenant(id string) error` — `tenantMu.Lock` (addTenantMu rename) → `Reconcile(id, nil)` drain → `tenants.Remove` → tenantList pop → `tenantRegistry.Unregister` → `td.Close` + delete map. 테스트: `TestServer_RemoveTenant_HappyPath` (AC-RM1 core).
- [x] TR3. `RemoveTenant` unknown ID 처리 + Reconcile 실패 시 state 불변 검증. 테스트: `TestServer_RemoveTenant_NotActive_Returns404Semantic` + `TestRemoveTenant_InvalidIDRejectedStateUnchanged` (AC-RM3 + AC-RM5).
- [x] TR4. `handleAdminTenantRemove` — `POST /api/v1/admin/tenants/{id}/delete` 핸들러 + route 등록 + `Client.TenantRemove` + localhost gate 재사용. 테스트: `TestHandleAdminTenantRemove_Success` + 404/400/non-localhost.
- [x] TR5. `cli/cmd_tenant.go` `newTenantRemoveCmd` + `runTenantRemove` — daemon probe, admin RPC (daemon up일 때), `tenants/<name>` 존재 검증. 테스트: `TestRunTenantRemove_DaemonDown_OfflinePath` (AC-RM2) + `TestRunTenantRemove_TenantNotFound` (AC-RM3).
- [x] TR6. 가족 config 정리 — `DiscoverTenants` → family 찾기 → `LoadConfig` → `delete(cfg.Share, name)` → `WriteConfigAtomic`. 테스트: `TestRunTenantRemove_FamilyConfigScrub` (AC-RM1 d) + `TestRunTenantRemove_NoFamily_NoOp` (AC-RM4).
- [x] TR7. 디렉토리 → `.trash/<name>-<ts>/` 이동 + 충돌 시 `-2`, `-3` suffix + BotFather 경고 stderr + family self-remove extra warning. 테스트: `TestRunTenantRemove_TrashCollision` (AC-RM8) + `TestRunTenantRemove_FamilySelf_ExtraWarning` (AC-RM7).
- [x] TR8. `make test` + `make lint` + CLAUDE.md 갱신 (tenant remove 절차 요약) + 자체 리뷰 → 커밋 (4ee9c95).

### Commit Map
- TR1~8 → `feat(cli): kittypaw tenant remove — safe household decommission`

---

## Family Init Wizard (α) ✅

Plan: `.claude/plans/family-init-wizard.md`
Spec: `.ina/specs/20260420-0431-auto-family-init-wizard.md`

Goal: `kittypaw family init` 인터랙티브 CLI — 한 번에 가족 멤버 N명 온보딩 (user ≡ tenant, scenario A).

- [x] FW1. `cli/cmd_family.go` 뼈대 — 타입 선언 + `familyInitFlags` + TTY guard + `runFamilyInit` 진입점. 테스트: `TestFamilyInit_NonTTY_Rejects`.
- [x] FW2. `scanExistingTenants` — `tenants/*/config.toml` 순회하여 기존 ID + Telegram token 맵 구성 (idempotency + in-run token dedup 데이터소스). 테스트: `TestScanExistingTenants_BuildsSeenSet`.
- [x] FW3. `provisionMember` — `InitTenant` + `activateTenantOnDaemon` + 상태 분류(ok/skipped_existing/failed). 테스트: `TestProvisionMember_AlreadyExistsSkips`, `TestProvisionMember_InvalidTokenFails`.
- [x] FW4. `promptMembers` — name/token/chatID 순차 입력, `ValidateTenantID`·`ValidateTelegramToken`·token dedup·blank/EOF/max 종료. 테스트 5개: 3명 성공, blank stop, max reached, 이름 재프롬프트, 토큰 중복 재프롬프트.
- [x] FW5. `createFamilyTenant` — IsFamily=true + `[share.family]` 빈 allowlist seed, `--no-family` 스킵, idempotent. 테스트 3개: default / skip flag / already exists.
- [x] FW6. `printSummary` + signal handling + Cobra 배선 + E2E — `signal.NotifyContext(Interrupt)`, BotFather 안내 출력, root.go 등록. 테스트: `TestPrintSummary_ExitCodes`, `TestRunFamilyInit_EndToEnd_HappyPath`, `TestRunFamilyInit_InterruptCleansStaging`.
- [x] FW7. `make test` + `make lint` 그린 → CLAUDE.md 갱신 → 자체 리뷰 → 커밋.

### Commit Map
- FW1~7 → `feat(cli): kittypaw family init onboarding wizard`

---

## Multi-user Blockers (A묶음) ✅

Plan: `.claude/plans/multi-user-blockers.md`
Spec: `.ina/specs/20260420-0324-auto-multi-user-blockers.md`

Goal: multi-user 스펙 구현 전 3개 기술 블로커 제거 (작고 독립적, 단일 커밋).

- [x] MB1. `core/tenant.go` validTenantID regex → `^[a-z0-9_][a-z0-9_-]{0,31}$` + `_default_`/`_shared_` accept 테스트
- [x] MB2. `server/spawner.go` spawnerKey 에 `Alias string` 추가 + `ChannelConfig.Alias` (TOML-hidden, 인프라 전용) + Stop/GetChannel/ReplaceSpawn/Reconcile 3-인자화 + 같은 tenant+type+다른 alias 동시 running 테스트
- [x] MB3. `core/types.go` ChatPayload 에 `FromID string` 추가 + Telegram/Slack/Discord/Kakao 채움 + SessionID 주석 명확화 + 채널별 FromID 회귀 테스트
- [x] MB4. `make test` + `make lint` 그린
- [x] MB5. diff self-review + 커밋 (codex adversarial: TOML alias 노출 시 dispatchLoop hardcoded `""` 경로 붕괴 — `toml:"-"` 로 봉인, 라우팅 wiring 은 follow-up)

### Commit Map
- MB1~5 → `feat(core): multi-user routing prerequisites`

---

## Plan 25: Family Multi-Tenant on macOS (S3-lite) ✅

Plan: `.claude/plans/family-multi-tenant.md`
Spec: `.ina/specs/20260418-0450-think-family-multi-tenant.md`

Goal: macOS 단일 데몬에서 7 개인 tenant + 1 family tenant 병렬 서빙.
Scope: S3-lite (cross-tenant read + family→개인 push; 쓰기 없음).
Total: 23 태스크 = 3 commits (Plan A/B/C).

### Plan A: Foundation — Tenant Routing Infrastructure ✅

- [x] A1. `core.Event` 에 `TenantID string` 필드 추가 (`json:"tenant_id,omitempty"`) + 하위호환 JSON 테스트
- [x] A2. `Server.session` → `sessions map[string]*engine.Session` 도입 (default 하나로 legacy 유지) + `server.New` 테스트
- [x] A3. `server/tenant_router.go` 신규 — event.TenantID 로 Session dispatch, mismatch/empty/unknown 은 drop + `tenant_routing_drop` metric; **fallback 금지 (C1)**
- [x] A4. `ChannelSpawner.running` 키 `{tenantID,channelType}` 로 확장; `GetChannel(tenantID, eventType)` + 같은 타입 다른 tenant 7개 running 테스트
- [x] A5. Channel 생성자에 tenantID 주입 — `channel.FromConfig(cfg, tenantID)` + 각 Channel struct 에 `tenantID`, Start 가 내보내는 Event 에 태깅
- [x] A6. 봇 토큰/Kakao account 중복 startup 감지 — fail-fast `duplicate telegram bot_token in tenants [a,b]` (**C3**)
- [x] A7. 통합 테스트 — alice/bob 2 tenant, alice 봇 이벤트 → alice.Store 만 row, bob.eventCh 비어있음 (AC-T3)
- [x] A8. Legacy 마이그레이션 기초 — `~/.kittypaw/config.toml` + `tenants/` 부재 → `tenants/default/` 로 auto-move (AC-T9 기초)

### Plan B: Share + Fanout — Family Specialization ✅

- [x] B1. `Config.IsFamily bool`, `Config.Share map[string]ShareConfig` 추가 + TOML 파싱 테스트 (`[share.family] read=[...]`)
- [x] B2. `core/share.go` 신규 — `ValidateSharedReadPath` + path traversal/symlink/hardlink/absolute 4종 매트릭스 테스트 (AC-T6, **C2**)
- [x] B3. `engine/share.go` 신규 — `Share.read(tenantID, path)` 스킬 (Session.TenantID/TenantRegistry 필드 추가, `cross_tenant_read` 감사 로그)
- [x] B4. family 채널 config 거부 — `core.ValidateFamilyTenants` fail-fast (AC-T4, **C6**)
- [x] B5. `core/fanout.go` 신규 — `Fanout.Send/Broadcast` → `Event{TenantID, Type:"family.push"}` eventCh 투입 (dispatch 는 A3 TenantRouter 로 커버)
- [x] B6. `sandbox` 조건부 JS binding — `Options.ExposeFanout` 로 family Session 에만 `Fanout` 노출 (개인은 `typeof Fanout === "undefined"`)
- [x] B7. E2E 통합 — alice `Share.read` 성공 + bob 거부 + `cross_tenant_read` 감사 로그 + family `Fanout.send` → eventCh 에 `EventFamilyPush` (AC-U2)

### Plan C: Operations + Demo — Tenant add, Isolation, E2E ✅

- [x] C1. `core/health.go` 신규 — `TenantHealth` enum (Ready/Degraded/Stopped) + `HealthState` (atomic, Stopped terminal) + `Session.Health` 배선 ✅ (merged with C2)
- [x] C2. goroutine recover + Degraded 전환 — `engine/recover.go` (`RecoverTenantPanic`/`MarkTenantReady`, nil-safe) + scheduler tickOnce/reflectionTick/runSkill/runPackage + `server.dispatchLoop` per-event recover + AC-T8 isolation test ✅ (merged with C1)
- [x] C3. CLI `kittypaw tenant add <name> --telegram-bot-token=<T>` subcommand + 공통 setup 헬퍼 (OQ7) ✅ — `core.InitTenant` (staging→rename atomic), `resolveTenantToken` (stdin > env > flag), family/token 상호배타, 중복 토큰 사전 검사. OQ7 결정: 새 헬퍼 추출보단 기존 `MergeWizardSettings` + `ValidateTelegramToken` + `FetchTelegramChatID` 를 양쪽이 공유 (TTY wizard 와 one-shot CLI 는 상호작용 모델이 달라 억지 추출 시 복잡도만 증가).
- [x] C4. HTTP `POST /api/v1/admin/tenants` — daemon hot-reload ✅ — `Server.AddTenant` (addTenantMu 직렬화, ValidateTenantChannels 스냅샷 + ValidateFamilyTenants 사전검사, OpenTenantDeps + buildTenantSession 경유로 startup 과 동일 경로, rollback stack 으로 LIFO 언와인드) + `handleAdminTenantAdd` (localhost-only gate on top of /api/v1 api-key, 409/404/400 mapping) + `Client.TenantActivate` + `kittypaw tenant add` 자동 활성화 (daemon 실행 중일 때만; `--no-activate` 로 스테이징만)
- [x] C5. Cross-routing 감지 (AC-T7) ✅ — `core.ChatBelongsToTenant` (AdminChatIDs exact match; 비어있으면 허용 — legacy/web_chat 호환) + `dispatchLoop` 에서 Route 성공 후 ownership 게이트 + `TenantRouter.RecordMismatch/MismatchCount` (per-tenant `sync.Map[string]*atomic.Int64`) + `tenant_routing_mismatch` slog + `TestDispatchLoop_ChatIDMismatch_Drops` (stolen-token 공격 시나리오) + 음성 대조군 `TestDispatchLoop_ChatIDMatch_NoMismatch`.
- [x] C6. Legacy 마이그레이션 완성 (AC-T9) ✅ — `server/tenant_migrate_integration_test.go` 신규: `TestLegacyMigration_PreservesDBRows` 는 legacy `~/.kittypaw/data/kittypaw.db` 에 실제 SaveState 로 row 를 심고 `MigrateLegacyLayout` 후 새 경로에서 `store.Open` + LoadState 로 row 보존 검증 (OpenTenantDeps 경유를 피해 LLM provider wiring 회귀와 혼선 차단). `TestLegacyMigration_ConfigPermissionPreserved` 는 0o640/0o600 mode 가 `os.Rename` 으로 inode 보존되는지 guard (copy-and-delete 회귀 방지).
- [x] C7. E2E AC-U1 ✅ — `server/family_push_test.go` 에 `TestFamilyMorningBrief_FansOutToAllPersonalTenants` (family 가 3× `Fanout.Send` 로 alice/bob/charlie 에게 맞춤 텍스트) + `TestFamilyMorningBrief_BroadcastFansOutToAllPeers` (`Fanout.Broadcast` 가 source 제외 후 모든 peer 에게 동일 텍스트). mock Telegram SendResponse 가 각 tenant 의 AdminChatIDs[0] 로 정확히 호출되는지 검증.
- [x] C8. E2E AC-U3 ✅ — `TestHandleAdminTenantAdd_HotReloadRouterReflectsImmediately`: POST 전 charlie drop → POST (30s context budget, goroutine+select 로 무한 block 회귀 차단) → POST 후 Session("charlie") 즉시 non-nil + Route 성공 + DropCount 불변. 실측 AddTenant 는 ~55ms 로 마무리, 30s 예산은 규격 상한.

#### Plan B → C 이월 (리뷰 findings)

- [x] C9. server bootstrap 에서 Session.TenantID/TenantRegistry/Fanout 3 필드 배선 ✅ (83a986b)
- [x] C10. `ValidateFamilyTenants` 를 server startup (StartChannels 이전) 에서 호출 ✅ (83a986b)
- [x] A8+. Legacy 마이그레이션 + DiscoverTenants 를 bootstrap 에서 실제 호출 ✅ (83a986b — Plan A A8 이 dead code 였던 것 활성화)
- [x] C11. `Share.read` 타겟 family-only 강제 ✅ — `engine/share.go` owner.Config.IsFamily 게이트 + `cross_tenant_read_rejected` 감사 + `sandbox.Options.ExposeShare` defense-in-depth
- [x] C12. `dispatchLoop` 의 `EventFamilyPush` 분기 ✅ — `deliverFamilyPush` helper 가 FanoutPayload 파싱 + 채널 선택 (hint 또는 Channels[0]) + AdminChatIDs[0] 로 SendResponse + 실패 시 pending_responses 큐

### Commit Map
- Plan A → `feat(core): multi-tenant routing foundation`
- Plan B → `feat(core): family tenant with cross-tenant read + fanout`
- Plan C → `feat(ops): kittypaw tenant add + E2E family demos`

---

## Discovery Endpoint Migration ✅

Plan: `.claude/plans/discovery-endpoint-migration.md`

- [x] T1: `core/discovery.go` + tests — `DiscoveryResponse`, `FetchDiscovery` (10s timeout, 64 KiB body cap, redirect ≤3, strict on HTTP/JSON/missing `api_base_url`, trailing-slash trim); 8 httptest cases
- [x] T2: `core/api_token.go` extended + tests — `saveOrDelete` helper, `Save/LoadAPIBaseURL`, `Save/LoadSkillsRegistryURL`, `SaveRelayURL` delete-on-empty; 3 new tests
- [x] T3: `cli/cmd_login.go` Discovery integration — `applyDiscovery(apiURL, mgr) string` called first in both `loginHTTP` and `loginCode`; swallows errors → warns to stderr and falls back to `apiURL`
- [x] T4: Removed `relay_url` from OAuth transports — callback query + exchange JSON; deleted `loginResult`/`tokenResult.relayURL`; `wizardKakao` now reads via `mgr.LoadRelayURL(apiURL)`
- [x] T5: Integration + commit — `go test ./...` / `golangci-lint` / `make build` all clean; CLAUDE.md API Token section updated; no orphaned refs

## Package Context Declaration ✅

Plan: `.claude/plans/package-context.md`
Spec: `.ina/specs/20260416-1300-think-package-context.md`

- [x] 1. core: PackagePermissions에 Context 필드 추가 + UserConfig 구조체 (18cac99)
- [x] 2. engine: event-in-context 헬퍼 + buildUserContext + detectLocale (18cac99)
- [x] 3. engine: runSkillOrPackage에서 user context 주입 + session에서 event 저장 (18cac99, fd71ab0)
- [x] 4. 테스트: detectLocale + buildUserContext + 하위호환 + AC#1 파싱 + AC#3 config-우선 명시
- [x] 5. registry: weather-briefing에 context 선언 + locale 활용 (skills repo 7294df5) — AC#6 실제 LLM 검증은 수동

## Relay Rust Rewrite ✅

Plan: `.claude/plans/relay-rust-rewrite.md`
Spec: `.ina/specs/20260416-1800-think-relay-rust-rewrite.md`

- [x] T0: Cargo 프로젝트 스캐폴드 (TS 파일 삭제, Cargo.toml, .gitignore)
- [x] T1: types.rs — Wire protocol DTOs, Kakao 타입, 한국어 상수, Config
- [x] T2: store.rs — Store trait + SqliteStore (WAL, spawn_blocking)
- [x] T3: state.rs — AppState (DashMap, moka, reqwest, Store)
- [x] T4: routes.rs — 라우트 핸들러 + WS 세션 + callback dispatch
- [x] T5: main.rs — 서버 부트스트랩, sweeper, graceful shutdown
- [x] T6: integration.rs — 8개 E2E 테스트 시나리오

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

## Plan 26: Setup → Chat Auto-Entry (G1+G2) ✅

Plan: `.claude/plans/cli-onboarding-chat.md`
Spec: `.ina/specs/20260420-0500-think-cli-onboarding-chat.md`

Goal: `kittypaw setup` 완료 후 TTY 환경에서 바로 `kittypaw chat` REPL 로 자동 진입 + 실행 중 데몬이 있으면 `POST /api/v1/reload` 로 새 config/채널 reconcile.
Scope: Thin slice — G1 (auto-entry) + G2 (daemon reload) 만. G3 (`onboarding_completed` DB 쓰기) 드롭 — 기존 `isOnboardingCompleted` 의 `LLM.APIKey != ""` 폴백이 load-bearing 임을 AC-DB 테스트로 고정. G4 (Web `EnsureDefaultProfile`), AC-7 (Web↔CLI equivalence matrix) 는 후속 PR.

- [x] T1. `cli/cmd_setup.go` 신설 + `autoChatEligible(flags, stdinTTY, stdoutTTY) bool` + `setupFlags.noChat` 필드 + truth-table 테스트 (AC-1)
- [x] T2. Korean string constants (`setupPromptAutoChat`, `setupMsgReloaded`, `setupMsgDaemonOff`, `setupMsgReloadFailedFmt`) + golden-string 테스트 (AC-STRINGS)
- [x] T3. `daemonSession` interface + `maybeReloadDaemon(dial, stdout, stderr)` + 3개 단위 테스트 (off/happy/error) (AC-4, AC-5, AC-6)
- [x] T4. `server/api_reload_sync_test.go:TestHandleReload_WaitsForReconcile` — blocking spawner barrier 로 handleReload→Reconcile 동기성 계약 고정 + `handleReload` load-bearing 주석 (AC-RELOAD-SYNC)
- [x] T5. `runSetup` 말미 배선 — EnsureDefaultProfile 뒤에 `maybeReloadDaemon` 호출, 힌트 박스 뒤에 `autoChatEligible` 분기 + `promptYesNo(default=Y)` + `runChat`; `--no-chat` flag 등록 + 회귀 테스트 2개 (AC-2, AC-3)
- [x] T6. `TestIsOnboardingCompleted_FallbackToLLMKey` — `user_context.onboarding_completed` 미설정 + `cfg.LLM.APIKey=sk-test` 일 때 `isOnboardingCompleted() == true` 단언 (AC-DB)
- [x] T7. `server/api_reload_race_test.go:TestAutoEntryNoRace` — in-process httptest server + reloadReconcile 훅 + `-race -count 50` 으로 POST /reload → 카운터 읽기 happens-before 고정 (AC-RACE) — 파일 위치를 `cli/` → `server/` 로 조정(훅이 server 패키지 내부 필드)
- [x] T8. `cli/cmd_setup_e2e_test.go` (`//go:build e2e`) — pty 기반 `TestAutoEntry_RestoresCookedMode` + `TestAutoEntry_CtrlC_ExitsWith130`. 기본 CI 에서는 빠진 상태, 로컬 opt-in (AC-TTY-RESTORE, AC-SIGINT). `github.com/creack/pty` 디펜던시 추가 — e2e 태그 전용.
- [x] T9. 품질 게이트: `make test` + `make lint` + `go test -race ./cli/... ./server/...` + `go test -race -count 50 -run TestAutoEntryNoRace ./server/` 그린 → CLAUDE.md "Onboarding → Chat Auto-Entry" 문단 추가 → 자체 리뷰 → 커밋. (race test 경로를 `./cli/` → `./server/` 로 조정 — 훅이 server 패키지 내부 필드이므로 해당 디렉토리에 소속)

### Commit Map
- T1~T9 → `feat(cli): setup → chat auto-entry with daemon hot-reload`

### Follow-up (out of this thin slice)
- [x] `handleReload` 에 `core.ValidateTenantChannels` / `core.ValidateFamilyTenants` 검증 추가 — `StartChannels` / `AddTenant` 와 대칭화. 검증 실패 시 config swap 롤백 + 409 반환. (adversarial review A-2: 0.82) ✅ — pre-commit validation (snapshot of live tenantList + proposed default cfg) 으로 구현. `TestHandleReload_DuplicateTelegramToken_Rejects` + `TestHandleReload_FamilyWithChannels_Rejects` + `TestHandleReload_ValidConfig_SwapsAndReconciles` + 기존 `TestHandleReload_WaitsForReconcile`/`TestAutoEntryNoRace` 회귀 무. `slog.Error(reason=...)` 로 observability 상향.

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
