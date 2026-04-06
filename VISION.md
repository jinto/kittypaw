# KittyPaw Vision

> "시작은 3분, 성장은 평생."

## Philosophy

KittyPaw는 기술 인접 파워유저를 위한 데스크톱 AI 자동화 앱이다.

모든 에이전트 프레임워크의 숨겨진 전제: **AI가 주인공이다.** Hermes는 "에이전트가 너를 기억하고 성장한다." OpenClaw는 "에이전트가 자율적으로 행동한다." Pi는 "에이전트가 자기를 확장한다."

KittyPaw는 다르다. **주인공은 결과다.** 개선 과정은 적극적으로 보여주되, 에이전트가 아니라 개선 결과가 주인공.

- Hermes 사용자는 에이전트를 자랑한다: "내 에이전트가 이걸 학습했어"
- KittyPaw 사용자는 개선 결과를 자랑한다: "API 타임아웃 자동 수정됨." "실행 시간 30% 단축."

## Design Principles

1. **설정이 아니라 설치.** config.yaml이 아니라 "설치" 버튼 하나.
2. **세션이 아니라 결과.** 대화 기록이 아니라 "오늘 뭐가 실행됐고 뭐가 나왔는지."
3. **개선 과정은 적극 공유.** 자동 수정, 패턴 감지, 최적화를 알림으로 보여준다. 사용자가 가치를 느끼게.
4. **대시보드 퍼스트.** 메인 화면은 자동화 현황판. 채팅은 스킬 설정 시에 보조.
5. **같은 엔진, 다른 차.** Self-evolve, Swarm, Night maintenance 메커니즘을 차용하되, 보이는 건 결과.
6. **하드코딩 금지.** LLM 프롬프트에 구체적인 값(chat_id, 토큰 등)을 주입하지 않는다. LLM은 "무엇을 할지"만 결정하고, "어떻게"는 executor가 config/secrets에서 자동 해결한다. 프리미티브 ABI는 최소한의 인자만 받는다.

## Competitive Philosophy Map

| 프로젝트 | 철학 | AI의 역할 |
|---------|------|----------|
| Hermes Agent | "에이전트가 너와 함께 자란다" | 주인공 — 기억, 학습, 진화 |
| Pi | "적을수록 강하다" | 장인 — 도구 4개로 모든 걸 만듦 |
| OpenClaw | "자유를 주면 강해진다" | 전사 — 제한 없이 행동 |
| **KittyPaw** | **"AI는 사라져야 한다"** | **전기 — 보이지 않지만 모든 걸 켜줌** |

## Milestones

### v1: Silent Engine (현재)
- 큐레이션 스킬 스토어 + 원클릭 설치 + 스케줄러
- **Silent memory:** 스킬 실행 기록(SQLite) → 패턴 감지 → 조용히 결과 개선
- **대시보드 퍼스트 GUI:** 메인 화면은 자동화 현황판, 채팅은 설정 모드
- 검증 목표: "5분 안에 설치하고, 1주 후 AI가 있다는 걸 잊었는가"
- 실행 전략: "시작은 3분, 성장은 평생" (이전 버전에서 유지)

### v2: Deeper Silence (v1 검증 후)
- 스킬 간 컨텍스트 공유 (위치, 선호, 패턴)
- 자동 스킬 제안 (실행 패턴 기반, 사용자에게 보이지 않게)
- 실패한 스킬 자동 수정 (GEPA 방식이지만 조용히)
- 멀티채널 (Telegram/Discord/Slack) — "결과 알림"으로, "대화"가 아니라
- FTS5 전문 검색 + LLM 요약으로 세션간 기억

### v3: Invisible Infrastructure (v2 안정화 후)
- 자연어 자동화 조합 ("아침 날씨 + 뉴스 + 일정을 합쳐서 브리핑해줘")
- 모델 자동 라우팅 (사용자는 모름)
- 커뮤니티 스킬 마켓플레이스

## Design Docs

- Latest: `~/.gstack/projects/jinto-kittypaw/jinto-main-design-20260330-154436.md` (APPROVED)
- Prior: `~/.gstack/projects/jinto-kittypaw/jinto-main-design-20260330-005526.md` (Superseded)
