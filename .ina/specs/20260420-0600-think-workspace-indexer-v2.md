# Workspace Indexer v2 — Live Filesystem Watching

**Type**: think
**Date**: 2026-04-20 06:00 KST (UTC 2026-04-19 21:00)
**Source**: Plan 16 (v1) follow-up — TASKS.md "Plan 17 v2 후보" 중 "실시간 파일 감시 — fsnotify 또는 File.write 훅으로 인덱스 자동 갱신" 선행.
**Predecessor**: `.ina/specs/20260414-2200-think-workspace-indexer.md`

## Goal

Workspace 파일 변경을 **실시간으로 감지**하여 FTS5 인덱스를 자동 갱신한다. 수동 `File.reindex()` 없이 "저장 → 즉시 검색 반영" 체감 제공.

## Context

- Plan 16(v1) 은 **부팅 시 + workspace CRUD 시에만** walk → 이후 파일 수정은 stale.
- Agent 가 `File.reindex()` 를 잊어버리면 검색 결과 오래된 상태. 사용자 체감 품질 저하.
- 이미 `FTS5Indexer.Index` 는 upsert 기반 — 단일 파일 insert/delete 로 확장 가능.
- Multi-tenant: 각 tenant의 `TenantDeps` 에 Indexer 1개 — watcher도 같은 단위로 배치.
- 현재 `Indexer` 인터페이스는 **전체 workspace walk** 만 제공 — 단일 파일 부분 업데이트 API 부재.

## Acceptance Criteria

**AC-1: 실시간 감지 (파일 변경)**
- workspace root 내 파일 create → 500ms 이내에 `File.search` 로 검색됨
- 파일 content 수정 → 500ms 이내에 수정된 body 로 검색됨
- 파일 delete → 500ms 이내에 검색 결과에서 제외됨

**AC-2: 디렉토리 재귀 커버**
- 런타임에 신규 하위 디렉토리 생성 시 해당 디렉토리도 감시 시작
- excluded dirs (`.git`, `node_modules`, `vendor`, `__pycache__`, `build`, `dist`) 는 감시 대상 제외 (기존 v1 정책 계승)

**AC-3: Debounce**
- 같은 파일에 500ms 내 연속 write 이벤트 → 1회만 재색인
- 서로 다른 파일 동시 변경 → 각각 debounce 타이머 (path 별 독립)

**AC-4: Multi-tenant 격리**
- tenant A의 워처가 tenant B 경로 이벤트 수신 X (`TenantDeps` 경계 유지)
- `TenantDeps.Close` → 해당 tenant 워처 종료, 다른 tenant 영향 X

**AC-5: 리소스 한계 대응 (inotify limit)**
- `watcher.Add` 실패 (watch limit 초과, permission 등) → `slog.warn` + 해당 workspace **lazy mode** (기존 수동 `File.reindex` 로 폴백)
- 다른 workspace 는 정상, 프로세스 크래시 X

**AC-6: 기본 활성화 + 옵션 OFF**
- 기본값: `[workspace] live_index = true`
- `live_index = false` 설정 시 워처 미시작 — v1 동작과 동일

**AC-7: Shutdown 청결**
- `Server.Shutdown` → 모든 워처 Close + 진행 중 debounce 타이머 stop (pending 은 drop)
- goroutine leak 없음 (테스트로 검증)

**AC-8: 관찰성 최소선 (Q6 → slog 레벨로만)**
- `watcher.Add` 실패 → `slog.Warn("live index watch add failed", "path", ..., "error", ...)`
- Lazy mode 전환 → `slog.Warn("workspace entering lazy index mode", "workspace_id", ...)`
- Debounce flush 시 → `slog.Debug("indexed live", "path", ..., "op", ...)`
- 각 tenant 워처 시작/종료 → `slog.Info`

**AC-9: 테스트**
- Unit: Debouncer (fake clock 결정론), Watcher (fake event source), IndexFile/RemoveFile (임시 FS)
- Integration (`//go:build integration`): 실제 fsnotify 로 임시 디렉토리 create/modify/delete E2E
- 기존 Plan 16 테스트는 회귀 없음

## Non-Goals

- **Prometheus 메트릭 / OpenTelemetry 트레이싱** — 이번 X, 후속 PR
- **`kittypaw status` 워처 상태 노출** — 이번 X, 후속
- **HTTP `/api/v1/workspaces/watchers` 엔드포인트** — 이번 X, 후속
- **inotify 초과 시 periodic reindex 폴백** — 이번 X, lazy mode 로 단순 처리
- **File.write 훅 병행** — 이번 X (fsnotify 단독으로 sandbox write 도 OS 레벨에서 커버)
- **File.summary(path) LLM 캐시** — 별도 Plan 17 v2 후보
- **bleve 백엔드 전환** — 별도 Plan 17 v2 후보
- **Cross-platform 재귀 네이티브 감시 최적화 (macOS FSEvents 직접 사용)** — fsnotify 라이브러리 경유만

## Design Decisions

**D1. fsnotify 라이브러리 (vs 직접 syscall)**
→ `github.com/fsnotify/fsnotify` 채택.
이유: 크로스플랫폼 표준, Go 커뮤니티 검증. macOS kqueue (재귀 X), Linux inotify, Windows ReadDirectoryChangesW 모두 지원. 재귀는 애플리케이션 레벨 walk 로 보완.

**D2. Watcher 스코프: tenant 1 개당 1 개**
→ `TenantDeps` 안에 Watcher 1 개. 여러 workspace root 를 동일 Watcher 에 Add.
이유: FD/goroutine 리소스 절약. fault isolation 은 tenant 경계로 충분 (Session 수준 이미 격리).

**D3. Debounce 500ms**
→ 경로별 coalesce. 같은 path 에 500ms 내 재호출 시 타이머 리셋 + 연장 금지 (cap 2s).
이유: SSD 로컬 편집기 저장 시 대략 10~100ms 간격으로 write 이벤트가 연달아 옴. 500ms 면 coalesce + 체감 빠름. cap 없으면 지속적 write 시 영원히 flush 안 됨.

**D4. Partial update API (IndexFile/RemoveFile)**
→ `Indexer` 인터페이스에 2 메서드 추가:
- `IndexFile(ctx, workspaceID, rootPath, absPath) error`
- `RemoveFile(workspaceID, absPath) error`
기존 walker 의 파일별 처리 로직을 `processFile(ctx, tx, workspaceID, rootPath, absPath, info)` 헬퍼로 추출하여 `Index` 와 `IndexFile` 양쪽이 재사용.
이유: 전체 walk 를 단일 파일 이벤트마다 호출하면 O(N files × M events) 비용. IndexFile 는 O(1).

**D5. inotify 한계 초과 시 lazy mode**
→ `watcher.Add` 에러 반환 → `slog.Warn` + 해당 workspace 는 live 감시 없이 수동 `File.reindex` 요구. `TenantDeps` state 에 `laziedWorkspaces map[string]bool` 기록해 재시도 없음.
이유: v2 첫 걸음 스코프 제약. periodic reindex 폴백은 v3 고려.

**D6. 기본 on, Config OFF 가능**
→ `[workspace] live_index = true` (기본).
이유: 가치 실현. CI/리소스 제한 환경은 false 로 off.

**D7. Event → Indexer op 매핑**
→ fsnotify ops:
- `Create` (파일) → `IndexFile` (upsert)
- `Create` (디렉토리) → `watcher.Add(newDir)` + 하위 walk (이미 존재 파일)
- `Write` → `IndexFile` (재색인)
- `Remove` → `RemoveFile` (delete)
- `Rename` → `RemoveFile(old)` + fsnotify 가 new path Create 이벤트를 별도 발행 → 정상 흐름
- `Chmod` → 무시

**D8. Fake clock 기반 Debouncer 테스트**
→ `time.Now` / `time.AfterFunc` 대신 `Clock` 인터페이스 주입.
이유: 결정론적 테스트, CI flaky 방지. `clockwork` 의존성 추가 vs 자체 구현 → 자체 구현 (최소 인터페이스, 기존 style).

**D9. Event 경로 필터링 — 임시 파일 무시**
→ `.swp`, `.swo`, `~`, `.tmp`, `#file#`, `.swx` suffix 감지 후 drop.
이유: vim/emacs/VSCode 의 save 과정이 임시 파일 생성 → 실제 파일 rename 이므로 임시 파일 이벤트는 노이즈.

## Entities

- **Watcher** (신규) — fsnotify 래퍼. tenant 당 1 개. workspace root 들을 Add. 디렉토리 생성 시 재귀 Add.
- **Debouncer** (신규) — 경로별 이벤트 coalesce. fake clock 기반.
- **LiveIndexer** (신규) — Watcher → Debouncer → Indexer 파이프라인 orchestration.
- **Indexer.IndexFile / RemoveFile** (확장) — 기존 Index() 의 파일별 처리 로직을 단일 파일에 적용.
- **Config.WorkspaceConfig.LiveIndex** (신규) — on/off 토글.

## Out-of-Scope 확인

- Agent 의 `File.search` 호출 횟수/패턴 관찰성 — 별도 PR (usage 데이터 선행 확인)
- MoA / Permission Checker / Desktop GUI — TASKS.md Backlog 유지
- `File.write` sandbox 훅 — fsnotify 가 대체. 만약 fsnotify 실패율이 실운영에서 높으면 v3 로 승격

## Known Limitations

- **macOS FSEvents 비활용**: fsnotify 라이브러리는 macOS 에서 **kqueue** 사용 → 네이티브 재귀 X, 대형 디렉토리 초기 Add 비용 ↑. excludedDirs 로 1차 방어, 그래도 대형 monorepo 는 수 초 걸릴 수 있음. **성능 이슈 감지 시 FSEvents 직접 래퍼 v3 고려**.
- **Linux inotify watch limit**: 기본 8192. 대형 workspace 는 sysctl 튜닝 권장 (`fs.inotify.max_user_watches=524288`). 초과 시 lazy mode.
- **Debounce gap**: 500ms 내 save + search 시 아직 반영 안 됐을 수 있음 (AC-1 최대 500ms 보장).
- **네트워크 마운트 (NFS/SMB)**: fsnotify 가 이벤트 잃을 수 있음 — 공식 문서에 명시. lazy mode 로 폴백 가능하지만 사용자 가시성 떨어짐.
- **편집기 atomic-save**: vim `backupcopy=yes` 등 "write temp → rename" 패턴은 Create + Remove 페어로 보임. Rename 이벤트로 정리되지만 debouncer 가 순서 보장 필요 — 테스트 필수.

## Commit

```
feat(engine): live workspace indexing via fsnotify
```

단일 PR. 이유: Watcher + Debouncer + Indexer 확장 + LiveIndexer + Session/TenantDeps 배선이 모두 한 흐름으로 연결 — 분리하면 중간 커밋들이 빌드되지 않거나 의미 없음. 각 내부 커포넌트는 개별 unit test 로 독립 검증.
