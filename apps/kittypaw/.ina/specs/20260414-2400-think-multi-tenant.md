# Multi-Tenant Single Daemon

> 단일 Go 데몬에서 여러 사용자(tenant)를 처리. 데이터는 사용자별 완전 격리.

## 배경

GoPaw는 현재 싱글테넌트. `~/.gopaw/`에 config, DB, skills, profiles, secrets가 하드코딩.
멀티유저 플랫폼으로 전환하면서 한 데몬이 여러 사용자를 처리하되, 데이터는 물리적으로 격리해야 한다.

### 왜 단일 데몬인가 (vs 데몬-퍼-유저)

- Telegram/Slack/Discord 봇은 본질적으로 멀티유저 — 봇 토큰 하나가 여러 사용자의 메시지를 수신
- LLM connection pool, MCP registry 공유로 자원 효율
- 프로세스 1개, 포트 1개, 로그 1개로 운영 단순

### 왜 SQLite tenant-per-DB인가

- WAL contention 제거 (tenant별 독립 writer)
- 물리적 파일 분리로 cross-tenant 데이터 유출 구조적 불가능
- 백업/복원: `cp tenant.db backup/`, 삭제: `rm tenant.db`
- idle tenant는 connection 닫아두면 fd 문제 없음

## 디렉토리 구조

```
~/.gopaw/
  server.toml                    # 서버 레벨 설정 (bind, master api_key)
  tenants/
    default/                     # 기존 데이터 마이그레이션 대상
      config.toml                # 기존 Config 포맷 그대로
      data/gopaw.db
      skills/
      profiles/
      secrets.json
      packages/
    alice/
      config.toml
      data/gopaw.db
      ...
```

## API 라우팅

- `X-Tenant-ID` 헤더로 tenant 지정 (없으면 "default")
- 기존 API 경로 변경 없음 — 100% 하위호환
- 마스터 API key (server.toml) → 모든 tenant 접근
- tenant별 API key → 해당 tenant만 접근

## 핵심 타입

```go
// core/tenant.go
type Tenant struct {
    ID      string
    BaseDir string  // ~/.gopaw/tenants/<id>/
    Config  *Config
}

type TenantRegistry struct {
    mu       sync.RWMutex
    tenants  map[string]*TenantBundle
    baseDir  string
}

type TenantBundle struct {
    Tenant    *Tenant
    Store     *store.Store
    Provider  llm.Provider
    Sandbox   *sandbox.Sandbox
    Session   *engine.Session
    Scheduler *engine.Scheduler
    EventCh   chan core.Event
}
```

## 변경 범위

### Phase 1: 기반 타입
- `core/tenant.go` — 신규: Tenant, TenantRegistry, TenantBundle
- `core/config.go` — ServerConfig 추가, LoadServerConfig()

### Phase 2: 스킬 함수 baseDir 변형
- `core/skill.go` — SkillsDirFrom(), SaveSkillTo(), LoadSkillFrom(), LoadAllSkillsFrom(), DeleteSkillFrom(), DisableSkillFrom(), RollbackSkillFrom()

### Phase 3: Engine BaseDir 전환
- `engine/session.go` — Session.BaseDir 필드 추가, loadProfileForPrompt()에서 사용
- `engine/executor.go` — LoadSkill() → LoadSkillFrom(s.BaseDir)
- `engine/schedule.go` — LoadAllSkills() → LoadAllSkillsFrom(s.BaseDir)
- `engine/teach.go` — SaveSkill() → SaveSkillTo(s.BaseDir)
- `engine/auto_fix.go` — LoadSkill() → LoadSkillFrom(s.BaseDir)
- `engine/evolution.go` — ConfigDir() → s.BaseDir
- `engine/orchestration.go` — ConfigDir() → baseDir 파라미터
- `engine/commands.go` — LoadAllSkills() → baseDir 파라미터

### Phase 4: Server 멀티테넌트화
- `server/middleware.go` — resolveTenant 미들웨어, requireAPIKey 확장
- `server/server.go` — TenantRegistry 기반 리팩토링
- `server/api.go` — 핸들러에서 context로 tenant session 사용
- `server/api_profile.go` — ConfigDir() → tenant BaseDir
- `server/api_setup.go` — ConfigPath() → tenant 기반

### Phase 5: CLI + Client
- `cmd/gopaw/main.go` — bootstrapServer(), runInit(), --tenant 플래그
- `client/daemon.go` — tenant ID 전달

## 검증

```bash
go build ./cmd/gopaw && go test ./...
./gopaw init                   # tenants/default/ 생성 확인
./gopaw serve --bind :3000     # 멀티테넌트 서버 시작
curl -H "X-Tenant-ID: default" http://localhost:3000/health
```
