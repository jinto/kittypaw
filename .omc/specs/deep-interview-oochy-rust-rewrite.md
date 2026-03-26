# Deep Interview Spec: Oochy v2 — Rust Single Binary AI Agent

## Metadata
- Interview ID: oochy-go-rust-rewrite
- Rounds: 7
- Final Ambiguity Score: 14%
- Type: brownfield
- Generated: 2026-03-26
- Threshold: 20%
- Status: PASSED

## Clarity Breakdown
| Dimension | Score | Weight | Weighted |
|-----------|-------|--------|----------|
| Goal Clarity | 0.90 | 35% | 0.315 |
| Constraint Clarity | 0.80 | 25% | 0.200 |
| Success Criteria | 0.85 | 25% | 0.213 |
| Context Clarity | 0.85 | 15% | 0.128 |
| **Total Clarity** | | | **0.856** |
| **Ambiguity** | | | **14%** |

## Goal

Oochy를 Rust 단일 바이너리로 재작성한다. `./oochy` 하나로 AI 에이전트 + 채널 어댑터 + 웹 대시보드 + 코드 샌드박스가 모두 실행되며, 외부 의존성(Node.js, Python, Docker)이 전혀 없다. LLM이 JS/TS 코드를 생성하고, QuickJS(VM 격리) + nono.sh(커널 격리) 이중 샌드박스에서 안전하게 실행한다. OpenClaw과의 차별점은 "코드 생성 + 실행"이라는 독보적 기능.

## Constraints

- **언어**: Rust (단일 바이너리 컴파일)
- **샌드박스**: QuickJS 내장 (1차, VM 격리) + nono.sh Rust SDK (2차, 커널 격리)
- **LLM 생성 코드 언어**: JavaScript/TypeScript
- **상태 저장**: 내장 SQLite (WAL mode)
- **채널**: v1은 Telegram + Discord + Web (2-3개)
- **LLM 제공자**: v1은 Claude/GPT API만. 로컬 LLM(Ollama)은 v2
- **타겟 OS**: Linux, macOS (Windows는 WSL2 필요)
- **라이선스**: 미정 (MIT 또는 Apache 2.0 유력)

## Non-Goals

- v1에서 OpenClaw의 모든 채널 매칭 (WhatsApp, iMessage, Mattermost 등)
- v1에서 로컬 LLM(Ollama) 지원
- v1에서 모바일 노드
- v1에서 클라우드 배포 옵션 (CF Workers, Fly.io 등)
- Python 코드 생성/실행 (v1은 JS/TS만)

## Acceptance Criteria

- [ ] `./oochy` 단일 바이너리로 에이전트 + 채널 + 대시보드 + 샌드박스 모두 실행 (외부 의존성 제로)
- [ ] Telegram 메시지 전송 → LLM이 JS/TS 코드 생성 → QuickJS에서 실행 → 결과 응답
- [ ] 생성된 코드가 허용 경로 외 파일 접근 불가 (nono.sh Landlock/Seatbelt 검증)
- [ ] 생성된 코드가 허용 호스트 외 네트워크 접근 불가 (nono.sh 검증)
- [ ] 타입 에러 시 LLM 피드백 루프 동작 (최대 3회 재시도)
- [ ] SQLite에 대화 상태 저장, 재시작 후 복구 확인
- [ ] 웹 대시보드(localhost:3000)에서 대화 내역 + 생성된 코드 확인 가능
- [ ] Discord 봇 메시지 응답 동작
- [ ] 코드 실행 타임아웃 30초 동작

## Assumptions Exposed & Resolved

| Assumption | Challenge | Resolution |
|------------|-----------|------------|
| CF Workers가 최적 | CF Workers는 subprocess 불가, $5/월 필수 | **Invalidated** → Local-first Rust |
| Docker가 샌드박스에 필요 | Docker는 500MB 설치, 원커맨드 위배 | **Rejected** → QuickJS + nono.sh로 Docker 불필요 |
| TypeScript/Bun이 맞다 | Bun은 설치 필요, better-sqlite3 네이티브 컴파일 문제 | **Rejected** → Rust 단일 바이너리가 모든 의존성 제거 |
| OpenClaw 전체 매칭 필요 | 채널 2-3개 + 코드 실행만으로 차별화 가능 | **Resolved** → 코드 실행이 핵심, 채널은 점진적 추가 |
| 로컬 LLM 필수 | 소형 모델의 코드 생성 품질 낮음 | **Deferred to v2** → v1은 API만, 품질 우선 |

## Technical Context

### 현재 코드베이스 (Python, 포팅 대상)
- `src/agent_loop/loop.py` (~80 lines) — 핵심 에이전트 루프
- `src/code_sandbox/runner.py` + `checker.py` (~130 lines) — subprocess + mypy
- `src/core/skills/*.py` + `*.pyi` — 스킬 런타임 + LLM용 스텁
- `src/web_api/` — FastAPI 라우트
- `src/core/utils/s3.py` — S3 상태 저장
- 총 실제 로직: ~500-970 lines

### 새로운 아키텍처
```
./oochy (Rust 단일 바이너리)
├── Agent Loop (이벤트 → 상태 로드 → 프롬프트 → LLM API → 코드 생성 → 샌드박스 → 상태 저장)
├── QuickJS Runtime (내장, LLM 생성 JS/TS 실행, VM 격리)
├── nono.sh (Rust SDK, 커널 레벨 격리 — Landlock/Seatbelt)
├── SQLite (내장, WAL mode, 에이전트 상태 + 대화 히스토리)
├── Channel Adapters
│   ├── Telegram (webhook + long-polling)
│   ├── Discord (gateway API)
│   └── Web Chat (내장 WebSocket)
├── Web Dashboard (내장 HTTP 서버, embedded static assets)
└── Plugin System (WASM 또는 dylib 기반, 향후)
```

### 핵심 Rust crate 후보
- `rquickjs` — QuickJS Rust 바인딩
- `rusqlite` — SQLite with bundled
- `axum` 또는 `actix-web` — HTTP 서버 + WebSocket
- `reqwest` — HTTP 클라이언트 (LLM API, Telegram API)
- `tokio` — async 런타임
- nono.sh Rust SDK — 커널 샌드박스

## Ontology (Key Entities)

| Entity | Type | Fields | Relationships |
|--------|------|--------|---------------|
| Oochy Binary | core domain | language=Rust, version, platforms | contains all components |
| Agent Loop | core domain | prompt, llmResponse, generatedCode, retryCount | invokes QuickJS, uses Skills, stores to SQLite |
| QuickJS Runtime | core domain | memoryLimit, cpuTimeout, contextVars | executes LLM-generated code (1차 격리) |
| nono.sh Sandbox | core domain | allowedPaths, allowedHosts, rollbackEnabled | wraps entire process (2차 커널 격리) |
| Channel Adapter | supporting | type, webhookUrl, pollingInterval | converts platform messages → Events |
| Dashboard | supporting | port, embeddedAssets, wsEndpoint | reads from SQLite, displays to user |
| SQLite Store | supporting | dbPath, walMode, busyTimeout | persists AgentState, ConversationTurn |
| LLM Provider | external system | apiKey, model, endpoint, maxTokens | generates JS/TS code from prompt |
| Skill Interface | supporting | name, tsInterfaceString, runtimeImpl | injected into generated code context |

## Ontology Convergence

| Round | Entity Count | New | Changed | Stable | Stability Ratio |
|-------|-------------|-----|---------|--------|----------------|
| 1 | 7 | 7 | - | - | N/A |
| 2 | 7 | 0 | 0 | 7 | 100% |
| 3 | 7 | 0 | 0 | 7 | 100% |
| 4 | 8 | 1 (QuickJS) | 0 | 7 | 88% |
| 5 | 8 | 0 | 0 | 8 | 100% |
| 6 | 8 | 0 | 0 | 8 | 100% |
| 7 | 9 | 1 (nono.sh) | 0 | 8 | 89% → 100% (converged) |

## Interview Transcript
<details>
<summary>Full Q&A (7 rounds)</summary>

### Round 1
**Q:** "한번에 실행하게"라고 하셨는데 — 이게 사용자가 `./oochy` 하나만 실행하면 모든 채널과 대시보드가 다 뜨는 것을 의미하나요?
**A:** 단일 바이너리, 모든 것 포함
**Ambiguity:** 51% (Goal: 0.65, Constraints: 0.30, Criteria: 0.25, Context: 0.80)

### Round 2
**Q:** Go와 Rust 중 어느 쪽으로 기울고 있나요?
**A:** Rust
**Ambiguity:** 45% (Goal: 0.70, Constraints: 0.50, Criteria: 0.25, Context: 0.80)

### Round 3
**Q:** v1이 완성되었다고 판단할 최소 기준이 뭐예요?
**A:** OpenClaw 동등 + 코드 실행
**Ambiguity:** 37% (Goal: 0.75, Constraints: 0.50, Criteria: 0.50, Context: 0.80)

### Round 4 (Contrarian Mode)
**Q:** LLM이 생성할 코드는 어떤 언어여야 할까요?
**A:** JavaScript/TypeScript
**Ambiguity:** 31% (Goal: 0.80, Constraints: 0.65, Criteria: 0.50, Context: 0.80)

### Round 5 (Contrarian — scope challenge)
**Q:** OpenClaw 전체를 매칭하는 게 정말 필요한가요?
**A:** 코드 실행이 핵심, 채널은 2-3개
**Ambiguity:** 24% (Goal: 0.85, Constraints: 0.65, Criteria: 0.70, Context: 0.80)

### Round 6
**Q:** 로컬 LLM(Ollama) 지원이 v1에 포함되어야 하나요?
**A:** v2로 연기
**Ambiguity:** 20% (Goal: 0.85, Constraints: 0.75, Criteria: 0.75, Context: 0.85)

### Round 7 (nono.sh 통합 + Simplifier Mode)
**Q:** v1 완성 판단 테스트 3개를 골라주세요.
**A:** ./oochy 하나로 전부 실행, LLM 코드 생성 → 샌드박스 실행, 샌드박스 보안 검증
**Ambiguity:** 14% (Goal: 0.90, Constraints: 0.80, Criteria: 0.85, Context: 0.85)

</details>
