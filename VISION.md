# KittyPaw Vision

> "시작은 3분, 성장은 평생"

## Philosophy

KittyPaw는 기술 인접 파워유저를 위한 데스크톱 AI 자동화 앱이다.
권한 부여나 간단한 트러블슈팅은 할 의지가 있지만, 터미널/config 파일/Docker는 싫어하는 사람.

핵심 믿음: **원클릭 시작과 성장하는 생태계를 동시에 잡은 제품이 아직 없다.**

- 원클릭 시작 도구들(Atomic Bot, Manus Desktop): 성장 루프 없음
- 성장하는 생태계(Hermes Agent, OpenClaw): 시작이 어려움
- KittyPaw: 스킬 하나 설치하는 것부터 시작하고, 쓸수록 강력해진다

## Competitive Philosophy Map

| 축 | 대표 프로젝트 | 핵심 믿음 |
|---|---|---|
| 미니멀리즘 | Pi (Mario Zechner) | 적게 만들수록 강하다 |
| 자율성 | OpenClaw (NVIDIA) | 풀어줄수록 강하다 |
| 성장 | Hermes Agent (NousResearch) | 오래 쓸수록 강하다 |
| **KittyPaw** | **미니멀 + 성장의 교차점** | **단순하게 시작하고, 점진적으로 강력해진다** |

## Milestones

### v1: Skill Store First (현재)
- 큐레이션된 10개 스킬 마켓 + 원클릭 설치/실행 + 스케줄러
- 검증 목표: "5분 안에 설치하고 믿고 돌아오는가"
- 성공 기준: 기술 인접 파워유저 10명 중 7명이 5분 내 첫 실행, 5명이 1주 후 재방문

### v2: Growing Agent (v1 검증 후)
- 스킬 자기 개선 루프 (실행 실패 -> LLM에 에러 전달 -> 코드 수정 -> 재실행)
- 사용자 모델링 (사용 패턴 기반 스킬 추천)
- 멀티채널 확장 (Discord, Slack, 카카오톡)
- 커뮤니티 스킬 마켓플레이스 (유저 제작 스킬 공유)

### v3: Recipe Remix (v2 안정화 후)
- 자연어 레시피 조합 (기존 스킬을 자연어로 커스터마이즈)
- 모델 자동 라우팅 (작업 복잡도별 모델 교체)

## Design Doc

Full design doc: `~/.gstack/projects/jinto-kittypaw/jinto-main-design-20260330-005526.md`

Generated 2026-03-30, APPROVED.
