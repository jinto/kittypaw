# Oochy on Cloudflare Workers — Rewrite Plan

## Requirements Summary

Oochy를 AWS Lambda/S3/SQS/IoT Core 기반에서 Cloudflare Workers 생태계로 완전히 재작성한다.
Python → TypeScript, 동일한 아키텍처 패턴(이벤트 기반, LLM 코드 생성, 샌드박스 실행)을 유지하되 Cloudflare 네이티브 서비스로 대체한다.

### Core Decisions
- **Language**: Python 3.13 → TypeScript (strict mode)
- **Runtime**: AWS Lambda → Cloudflare Workers
- **Sandbox**: subprocess + mypy → Dynamic Workers (V8 isolate) + TypeScript compiler
- **State**: S3 → R2
- **Queue**: SQS FIFO → Cloudflare Queues
- **Realtime**: AWS IoT Core (MQTT) → Durable Objects + WebSocket
- **API**: FastAPI + API Gateway → Workers (Hono framework)
- **Infra**: AWS CDK → wrangler.toml
- **Migration**: Clean rewrite (not incremental)

---

## Architecture Mapping

```
현재 (AWS)                          →  Cloudflare
─────────────────────────────────────────────────────────
Lambda (Web API)                    →  Worker (API)
Lambda (Agent Loop)                 →  Worker (Agent Loop) + Queue Consumer
Lambda (Code Sandbox)               →  Dynamic Worker (V8 isolate)
API Gateway                         →  Workers Routes (내장)
SQS FIFO                           →  Cloudflare Queues
S3 (state bucket)                   →  R2
IoT Core (MQTT)                     →  Durable Objects + WebSocket
CDK 4 stacks                        →  wrangler.toml
mypy --strict                       →  TypeScript compiler (strict)
subprocess isolation                →  V8 isolate (Dynamic Workers)
```

---

## Implementation Steps

### Phase 1: Project Scaffolding & Core Types (1단계)

**Goal**: TypeScript 프로젝트 세팅 + 핵심 타입 정의

**Step 1.1**: 프로젝트 초기화
- `wrangler init oochy-workers` 또는 수동 세팅
- `package.json` with: `wrangler`, `typescript`, `hono`, `@anthropic-ai/sdk`, `vitest`
- `tsconfig.json` with `strict: true`, `target: "ES2022"`, `module: "ESNext"`
- `wrangler.toml` 기본 구성 (Workers Paid plan 필요)
- 디렉토리 구조:
  ```
  src/
    index.ts              # Main worker entry (Hono router)
    agent-loop/
      loop.ts             # Core agent loop
      llm.ts              # Claude API client
      prompt.ts           # Prompt builder
      skill-registry.ts   # Skill loader
    sandbox/
      executor.ts         # Dynamic Worker invocation
    core/
      types/
        event.ts          # Event type definitions
        agent-state.ts    # Agent state model
        skill.ts          # Skill definitions
        message.ts        # LLM message types
      skills/
        telegram.ts       # Telegram skill
        chat.ts           # Web chat skill
        desktop.ts        # Desktop skill (WebSocket)
        voice.ts          # Voice skill (stub)
      storage/
        r2.ts             # R2 state persistence
    api/
      routes/
        chat.ts           # POST /chat/send
        events.ts         # POST /events/telegram/webhook
        auth.ts           # POST /auth/login
        status.ts         # GET /status/agent/:id
    realtime/
      desktop-do.ts       # Durable Object for desktop WebSocket
  test/
    unit/
    integration/
  ```

**Step 1.2**: Core Types (Python Pydantic → TypeScript interfaces)
- `event.ts`: `EventType` enum + `Event` interface
  - 현재: `src/core/types/event.py` (EventType enum, Event Pydantic model)
  - 변환: Pydantic → Zod schema + TypeScript interface
- `agent-state.ts`: `AgentState`, `ConversationTurn` interfaces
  - 현재: `src/core/types/agent_state.py`
  - `add_turn()`, `recent_turns(n)` 메서드 유지
- `skill.ts`: `SkillDefinition` interface
  - 현재: `src/core/types/skill.py`
- `message.ts`: `LLMMessage` interface
  - 현재: `src/core/types/message.py`

**Acceptance Criteria**:
- [ ] `wrangler dev` 로 빈 Worker가 로컬에서 실행됨
- [ ] 모든 core type이 TypeScript strict 모드에서 컴파일됨
- [ ] `vitest` 테스트 러너가 동작함
- [ ] Event, AgentState 타입에 대한 단위 테스트 통과

---

### Phase 2: State Storage — R2 (2단계)

**Goal**: S3 상태 저장을 R2로 대체

**Step 2.1**: R2 바인딩 설정
- `wrangler.toml`에 R2 bucket 바인딩 추가
  ```toml
  [[r2_buckets]]
  binding = "STATE_BUCKET"
  bucket_name = "oochy-state"
  ```

**Step 2.2**: Storage 모듈 구현
- `src/core/storage/r2.ts`
  - 현재: `src/core/utils/s3.py` (boto3 S3 client, load_state/save_state)
  - `loadState(bucket: R2Bucket, agentId: string): Promise<AgentState>`
  - `saveState(bucket: R2Bucket, state: AgentState): Promise<void>`
  - Key pattern 동일: `state/{agentId}/latest.json`
  - R2 API는 S3 호환이므로 개념적으로 1:1 매핑

**Acceptance Criteria**:
- [ ] `loadState`가 존재하지 않는 agent에 대해 빈 상태 반환
- [ ] `saveState` → `loadState` 라운드트립이 데이터 무결성 유지
- [ ] JSON 직렬화/역직렬화가 datetime 필드 올바르게 처리
- [ ] R2 바인딩이 `wrangler dev`에서 동작 (로컬 R2 에뮬레이션)

---

### Phase 3: LLM Integration + Prompt System (3단계)

**Goal**: Claude API 호출 + 프롬프트 빌드

**Step 3.1**: LLM Client
- `src/agent-loop/llm.ts`
  - 현재: `src/agent_loop/llm.py` (anthropic.AsyncAnthropic, generate_code)
  - `@anthropic-ai/sdk` 사용
  - `generateCode(messages: LLMMessage[]): Promise<string>`
  - 모델: `claude-sonnet-4-20250514` (환경변수로 설정 가능)
  - Max tokens: 4096
  - Markdown code fence 제거 로직 동일

**Step 3.2**: Prompt Builder
- `src/agent-loop/prompt.ts`
  - 현재: `src/agent_loop/prompt.py` (build_prompt, format_event)
  - 시스템 프롬프트: **TypeScript 코드 생성**으로 변경
    - "Write valid TypeScript code" (Python이 아닌)
    - async/await 패턴 동일
    - 스킬 인터페이스를 TypeScript로 제공
  - `buildPrompt(state, event, skills): LLMMessage[]`
  - `formatEvent(event): string` — 포맷 동일 (Telegram, WebChat, Desktop)
  - 최근 20개 대화 턴 포함

**Step 3.3**: Skill Registry
- `src/agent-loop/skill-registry.ts`
  - 현재: `src/agent_loop/skill_registry.py` (load .pyi stubs)
  - `.pyi` 스텁 대신 **TypeScript interface 문자열**을 LLM에 제공
  - 각 스킬의 타입 정의를 문자열로 관리

**Acceptance Criteria**:
- [ ] Claude API 호출이 TypeScript 코드를 반환
- [ ] 프롬프트에 스킬 인터페이스가 포함됨
- [ ] 대화 히스토리가 올바르게 포함됨 (최근 20턴)
- [ ] 이벤트 포맷이 현재와 동일 (`[Telegram] user (chat_id=x): text`)

---

### Phase 4: Code Sandbox — Dynamic Workers (4단계)

**Goal**: LLM이 생성한 TypeScript 코드를 V8 isolate에서 안전하게 실행

**Step 4.1**: Dynamic Worker Loader 설정
- Cloudflare Dynamic Workers API 사용
- `wrangler.toml`에 Dynamic Worker 설정 추가

**Step 4.2**: Sandbox Executor 구현
- `src/sandbox/executor.ts`
  - 현재: `src/code_sandbox/runner.py` (subprocess) + `checker.py` (mypy)
  - 새로운 흐름:
    1. LLM이 생성한 TypeScript 코드 수신
    2. TypeScript 컴파일 검증 (Dynamic Worker 로딩 시 자동)
    3. Dynamic Worker에 코드 전달 → V8 isolate에서 실행
    4. 결과 반환: `{success: boolean, result: string, output: string}`
  - 타임아웃: 30초 (현재와 동일)
  - 컨텍스트 주입: 이벤트 데이터를 `_context` 변수로 전달

**Step 4.3**: 코드 래핑
- 현재 `_wrap_async()` 함수의 TypeScript 버전
  - 스킬 함수들을 주입 (Telegram.sendMessage 등)
  - `_context` 변수 주입
  - 에러 핸들링 래핑

**Step 4.4**: 타입 체크 피드백 루프
- 현재: mypy 에러 → LLM에 피드백 → 재생성 (max 3회)
- 새로운: TypeScript 컴파일 에러 → LLM에 피드백 → 재생성 (max 3회)
- Dynamic Worker 로딩 실패 시 에러 메시지를 LLM에 전달

**Acceptance Criteria**:
- [ ] LLM이 생성한 TypeScript 코드가 Dynamic Worker에서 실행됨
- [ ] V8 isolate 격리가 동작 (파일시스템 접근 불가, 네트워크 제한)
- [ ] 타입 에러 발생 시 LLM 피드백 루프가 동작 (최대 3회)
- [ ] 30초 타임아웃 동작
- [ ] 스킬 함수가 코드 내에서 호출 가능

---

### Phase 5: Agent Loop (5단계)

**Goal**: 핵심 에이전트 루프를 Workers에서 실행

**Step 5.1**: Agent Loop Worker
- `src/agent-loop/loop.ts`
  - 현재: `src/agent_loop/loop.py` (run_agent_loop)
  - 동일한 흐름:
    1. R2에서 상태 로드
    2. 이벤트 기록
    3. 프롬프트 빌드
    4. Claude API 호출 → TypeScript 코드 생성
    5. Dynamic Worker에서 타입 체크 + 실행 (재시도 루프)
    6. R2에 상태 저장
    7. 결과 반환

**Step 5.2**: Queue Consumer
- Cloudflare Queues consumer로 Agent Loop 트리거
  - 현재: SQS → Lambda 트리거 (`src/agent_loop/handler.py`)
  - `wrangler.toml`에 Queue consumer 바인딩:
    ```toml
    [[queues.consumers]]
    queue = "oochy-events"
    max_batch_size = 1
    max_retries = 3
    dead_letter_queue = "oochy-events-dlq"
    ```
  - `queue()` handler에서 메시지 파싱 → `runAgentLoop()` 호출

**Acceptance Criteria**:
- [ ] Queue 메시지 수신 → 에이전트 루프 실행 → 결과 저장 전체 플로우 동작
- [ ] 상태가 R2에 올바르게 저장/로드됨
- [ ] LLM 코드 생성 → 타입 체크 → 실행 루프 동작
- [ ] max_batch_size=1로 순차 처리 보장 (FIFO 대체)
- [ ] DLQ로 실패 메시지 이동

---

### Phase 6: Web API (6단계)

**Goal**: HTTP API를 Hono + Workers로 구현

**Step 6.1**: Hono Router 설정
- `src/index.ts` — Main Worker entry
  - 현재: `src/web_api/app.py` (FastAPI + Mangum)
  - Hono 프레임워크 사용 (Workers에 최적화된 경량 웹 프레임워크)
  - CORS 미들웨어 설정
  - 바인딩 타입 정의 (R2, Queue, DO, Dynamic Workers)

**Step 6.2**: API Routes
- `POST /chat/send` — Web Chat
  - 현재: `src/web_api/routes/chat.py`
  - 세션 ID 생성, Event 생성, 에이전트 루프 직접 실행, 결과 반환
  - 응답: `{session_id, response, code, success}`

- `POST /events/telegram/webhook` — Telegram Webhook
  - 현재: `src/web_api/routes/events.py`
  - Telegram payload 파싱, Event 생성, Queue에 전송
  - 응답: `{status: "queued" | "ignored"}`

- `POST /auth/login` — Google OAuth
  - 현재: `src/web_api/routes/auth.py`
  - Google token 검증, JWT 발급
  - JWT 서명: Workers 환경변수 (secrets)

- `GET /status/agent/:id` — Agent Status
  - 현재: `src/web_api/routes/status.py`
  - R2에서 상태 로드, 최근 5턴 반환

- `GET /health` — Health Check

**Acceptance Criteria**:
- [ ] 모든 API 엔드포인트가 현재와 동일한 요청/응답 포맷
- [ ] `/chat/send`가 에이전트 루프를 실행하고 결과 반환
- [ ] `/events/telegram/webhook`이 Queue에 메시지 전송
- [ ] CORS가 올바르게 동작
- [ ] JWT 인증이 Workers secrets 기반으로 동작

---

### Phase 7: Skills (7단계)

**Goal**: 4개 스킬을 TypeScript로 재구현

**Step 7.1**: Telegram Skill
- `src/core/skills/telegram.ts`
  - 현재: `src/core/skills/telegram.py` (httpx → Telegram Bot API)
  - `sendMessage(chatId: string, text: string): Promise<{message_id: number}>`
  - `sendVoice(chatId: string, audioUrl: string): Promise<{message_id: number}>`
  - Workers의 `fetch()` API 사용 (httpx 대체)
  - BOT_TOKEN: Workers 환경변수

**Step 7.2**: Chat Skill
- `src/core/skills/chat.ts`
  - 현재: `src/core/skills/chat.py` (placeholder)
  - Durable Objects를 활용한 WebSocket 기반 실시간 채팅으로 업그레이드 가능
  - `sendMessage(sessionId: string, text: string): Promise<{status: string}>`

**Step 7.3**: Desktop Skill
- `src/core/skills/desktop.ts`
  - 현재: `src/core/skills/desktop.py` (MQTT publish)
  - Durable Objects + WebSocket으로 대체
  - `bash(command: string): Promise<{stdout: string, stderr: string, exit_code: number}>`
  - `appleScript(script: string): Promise<{result: string}>`
  - **양방향 통신**: DO가 WebSocket으로 데스크톱 클라이언트에 명령 전송, 응답 대기

**Step 7.4**: Voice Skill
- `src/core/skills/voice.ts`
  - 현재: `src/core/skills/voice.py` (placeholder)
  - Stub 유지

**Step 7.5**: Skill Interface Strings (LLM용)
- 각 스킬의 TypeScript interface를 문자열로 관리
  - 현재의 `.pyi` 스텁 역할
  - LLM 프롬프트에 포함되어 타입 정보 제공

**Acceptance Criteria**:
- [ ] Telegram skill이 Workers fetch()로 메시지 전송 성공
- [ ] Desktop skill이 Durable Object WebSocket으로 명령 전송/응답 수신
- [ ] 스킬 인터페이스 문자열이 프롬프트에 올바르게 포함
- [ ] LLM이 스킬 함수를 올바르게 호출하는 코드 생성

---

### Phase 8: Realtime — Durable Objects (8단계)

**Goal**: Desktop 클라이언트와의 실시간 양방향 통신

**Step 8.1**: Desktop Durable Object
- `src/realtime/desktop-do.ts`
  - Durable Object class: `DesktopSession`
  - WebSocket 연결 관리 (데스크톱 클라이언트 ↔ DO)
  - 명령 전송: Worker → DO → WebSocket → 데스크톱 클라이언트
  - 응답 수신: 데스크톱 클라이언트 → WebSocket → DO → Worker
  - 상태: 연결 상태, 대기 중인 명령 큐

**Step 8.2**: WebSocket 엔드포인트
- `GET /ws/desktop/:clientId` — WebSocket upgrade
  - 데스크톱 클라이언트가 연결
  - Durable Object에 라우팅

**Step 8.3**: wrangler.toml DO 설정
```toml
[durable_objects]
bindings = [
  { name = "DESKTOP_SESSION", class_name = "DesktopSession" }
]

[[migrations]]
tag = "v1"
new_classes = ["DesktopSession"]
```

**Step 8.4**: 데스크톱 클라이언트 업데이트
- 현재 MQTT 클라이언트 → WebSocket 클라이언트로 변경
- 프로토콜: JSON 메시지 (`{type: "bash"|"applescript", command: string}`)
- 연결 URL: `wss://oochy.{domain}/ws/desktop/{clientId}`

**Acceptance Criteria**:
- [ ] 데스크톱 클라이언트가 WebSocket으로 연결 성공
- [ ] Worker에서 DO를 통해 명령 전송 → 응답 수신 왕복 동작
- [ ] 연결 끊김 시 재연결 처리
- [ ] 동시 다중 데스크톱 클라이언트 지원

---

### Phase 9: Infrastructure & Deployment (9단계)

**Goal**: wrangler.toml 완성 + 배포 파이프라인

**Step 9.1**: wrangler.toml 통합 설정
```toml
name = "oochy"
main = "src/index.ts"
compatibility_date = "2025-03-01"
node_compat = true

[vars]
LLM_MODEL = "claude-sonnet-4-20250514"

# Secrets (wrangler secret put):
# ANTHROPIC_API_KEY, TELEGRAM_BOT_TOKEN, JWT_SECRET, GOOGLE_CLIENT_ID

[[r2_buckets]]
binding = "STATE_BUCKET"
bucket_name = "oochy-state"

[[queues.producers]]
queue = "oochy-events"
binding = "EVENT_QUEUE"

[[queues.consumers]]
queue = "oochy-events"
max_batch_size = 1
max_retries = 3
dead_letter_queue = "oochy-events-dlq"

[durable_objects]
bindings = [
  { name = "DESKTOP_SESSION", class_name = "DesktopSession" }
]

[[migrations]]
tag = "v1"
new_classes = ["DesktopSession"]
```

**Step 9.2**: Secrets 설정
```bash
wrangler secret put ANTHROPIC_API_KEY
wrangler secret put TELEGRAM_BOT_TOKEN
wrangler secret put JWT_SECRET
wrangler secret put GOOGLE_CLIENT_ID
```

**Step 9.3**: 배포
```bash
wrangler deploy
```

**Step 9.4**: Telegram Webhook 설정
- 새 Workers URL로 Telegram Bot webhook 업데이트

**Acceptance Criteria**:
- [ ] `wrangler deploy`로 단일 명령 배포 성공
- [ ] 모든 바인딩 (R2, Queue, DO) 정상 동작
- [ ] Secrets 올바르게 주입
- [ ] Custom domain 설정 (선택)

---

### Phase 10: Testing & Verification (10단계)

**Goal**: 전체 시스템 검증

**Step 10.1**: Unit Tests (vitest)
- Core types 직렬화/역직렬화
- R2 storage 라운드트립
- Prompt builder 출력 검증
- Event formatting
- Skill registry 로딩

**Step 10.2**: Integration Tests
- Telegram webhook → Queue → Agent Loop → R2 상태 저장
- `/chat/send` → 에이전트 루프 → 응답
- Desktop WebSocket 연결 → 명령 전송 → 응답

**Step 10.3**: E2E Verification
- Telegram에서 메시지 전송 → 응답 수신
- 웹 채팅으로 대화 → 코드 생성/실행 확인
- Desktop 클라이언트 WebSocket 연결 → bash 명령 실행

**Acceptance Criteria**:
- [ ] Unit test 커버리지 80%+
- [ ] 모든 API 엔드포인트 현재와 동일한 동작
- [ ] Telegram 봇 정상 동작
- [ ] 코드 생성 → 타입 체크 → 실행 전체 파이프라인 동작
- [ ] Cold start < 10ms

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Dynamic Workers API가 beta에서 변경될 수 있음 | 높음 | Executor를 추상화 레이어로 감싸서 구현 교체 가능하게 |
| Cloudflare Queues에 FIFO 보장이 없음 | 중간 | agent_id별 Durable Object로 순서 보장 가능, 또는 메시지에 시퀀스 번호 추가 |
| LLM이 TypeScript 코드 생성 품질 | 중간 | Claude는 TS에 능숙. 프롬프트에 충분한 타입 정보 제공. 피드백 루프 3회 유지 |
| Workers CPU 30초 제한 | 낮음 | Agent loop는 대부분 I/O 대기 (Claude API). CPU 사용은 미미 |
| Workers 128MB 메모리 제한 | 낮음 | 상태는 R2에 저장, Worker 메모리 사용은 최소 |
| Durable Objects 비용 | 중간 | DO는 요청당 + 저장당 과금. 데스크톱 연결이 적으면 비용 미미 |

---

## Verification Steps

1. `wrangler dev`로 로컬에서 전체 시스템 동작 확인
2. `vitest run`으로 전체 테스트 통과 확인
3. `wrangler deploy`로 프로덕션 배포
4. Telegram Bot webhook을 새 URL로 변경 후 메시지 왕복 확인
5. `/chat/send` API 호출로 웹 채팅 동작 확인
6. Desktop WebSocket 클라이언트 연결 후 명령 실행 확인
7. R2에 상태가 올바르게 저장되는지 확인
8. Cold start 시간 측정 (목표: < 10ms)
9. 이전 CDK 인프라 정리 (확인 후)

---

## Phase Execution Order & Dependencies

```
Phase 1 (Scaffolding)
  ↓
Phase 2 (R2 Storage)  ←── Phase 3 (LLM + Prompt) [병렬 가능]
  ↓                           ↓
Phase 4 (Sandbox - Dynamic Workers)
  ↓
Phase 5 (Agent Loop) ←── Phase 7 (Skills) [병렬 가능]
  ↓                           ↓
Phase 6 (Web API)
  ↓
Phase 8 (Realtime - Durable Objects)
  ↓
Phase 9 (Infrastructure)
  ↓
Phase 10 (Testing)
```

병렬 가능: Phase 2+3, Phase 5+7

---

## Environment Variables

| Variable | Source | Purpose |
|----------|--------|---------|
| `ANTHROPIC_API_KEY` | Secret | Claude API 인증 |
| `LLM_MODEL` | Var | 모델 선택 (기본: claude-sonnet-4-20250514) |
| `TELEGRAM_BOT_TOKEN` | Secret | Telegram Bot API |
| `JWT_SECRET` | Secret | JWT 서명 |
| `GOOGLE_CLIENT_ID` | Secret | Google OAuth |

바인딩:
- `STATE_BUCKET` — R2 bucket
- `EVENT_QUEUE` — Queue producer
- `DESKTOP_SESSION` — Durable Object namespace

---

## What Gets Removed

- `infra/` 디렉토리 전체 (CDK stacks)
- `pyproject.toml`, `uv.lock`, `.venv`
- `src/` Python 소스 전체
- boto3, moto, mangum 등 Python 의존성
- AWS IAM 설정, Lambda 레이어

## What Gets Preserved

- 아키텍처 패턴: 이벤트 → 큐 → 에이전트 루프 → 샌드박스 → 상태 저장
- API 엔드포인트 및 요청/응답 포맷
- 스킬 시스템 (인터페이스 → 런타임 분리)
- 대화 상태 구조 (AgentState, ConversationTurn)
- LLM 프롬프트 구조 (시스템 프롬프트 + 히스토리 + 이벤트)
- 타입 체크 → 실행 → 피드백 루프 (3회 재시도)
