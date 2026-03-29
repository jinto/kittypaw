# KittyPaw Tasks

## Completed

### Skill Platform — Phase 0~4 ✅
- [x] Phase 0: SkillResolver (샌드박스 실제 데이터 반환)
- [x] Phase 1: 패키지 포맷 + 매니저 + executor 브릿지
- [x] Phase 2: File.read/write, Telegram.sendDocument, Env.get
- [x] Phase 3: GUI 스킬 갤러리 + 설정 위자드 (Dioxus)
- [x] Phase 4: 예제 패키지 5개 (한국어) + 자동 번들 설치

### GUI: Tauri → Dioxus 전환 ✅
- [x] Tauri + SvelteKit 삭제 (~24k LOC)
- [x] Dioxus 0.6 순수 Rust GUI (~470 LOC)
- [x] `cargo run -p kittypaw-gui` 한 줄 실행
- [x] GUI 채팅 → 실제 LLM 호출 (ClaudeProvider)
- [x] 스킬 Test Run 버튼 (SkillResolver 연동)

### Foundation 기반 기능 4개 ✅
- [x] OS keychain 시크릿 관리 (`keyring` crate)
- [x] 멀티 프로바이더 LLM (OpenAI + Claude + LlmRegistry)
- [x] Web.search / Web.fetch 샌드박스 프리미티브
- [x] 스킬 체이닝 (`[[chain]]` + prev_output 전달)

### 문서 ✅
- [x] README 리뉴얼 (Use Case 중심, 한국어)

### 보안 수정 ✅
- [x] Web.fetch SSRF 리다이렉트 차단
- [x] UTF-8 멀티바이트 truncation 패닉 수정
- [x] 체인 스텝 skill calls 실행 누락 수정

## In Progress

### 배포 준비
- [ ] kittypaw.app 도메인 설정
- [ ] 랜딩 페이지 (GitHub Pages)
- [ ] `kittypaw-skills` GitHub org + registry

### 모델 라우팅 config 연결
- [ ] `kittypaw.toml`에 `[[models]]` 섹션 파싱 → LlmRegistry 등록
- [ ] GUI 채팅에서 등록된 모델 사용 (현재 claude-sonnet 하드코딩)
- [ ] `package.toml`에 `model` 필드 → 스킬별 모델 지정

## Backlog

### 🟠 P1: 모델 자동 라우팅
- teach loop 키워드 분류기 (automation→Haiku, analysis→Sonnet, integration→Opus)
- 2단계 신뢰도 게이팅 (high=자동, medium=추천)
- 대화 중 자동 모델 교체 (Crew 패턴)

### 🟠 P1: 웹 검색 개선
- 검색 프로바이더 폴백 체인 (Exa → DuckDuckGo → 커스텀)
- GUI 검색 큐레이션 (결과 선택 → 스킬 주입)
- Web.fetch 마크다운 추출 개선

### 🟡 P2: 스킬 체이닝 확장
- 병렬 실행 (`parallel()`)
- `converge` 모드 (변경 없으면 조기 종료)
- 체인 단계별 모델 로테이션

### 🟡 P2: AI 비서 프리셋
- 지침 템플릿 시스템 (캐릭터 + 말투 + 배경지식)
- 팩트체크 파이프라인 (복수 LLM 교차검증)
- 콘텐츠 회고 스킬

### 🟡 P2: 추가 채널 어댑터
- Slack 채널 어댑터
- Discord 채널 어댑터
- 크로스 채널 컨텍스트 (사용자 ID 기반 통합)

### 🟢 P3: 자율 최적화 루프
- `kittypaw optimize <skill> --metric <name>`
- 실행 실패 → 자동 코드 수정 → 재실행
- 신뢰도 점수 (MAD 기반)

### 🟢 P3: 한국 특화 스킬 패키지
- SRT/KTX 예약, 배송 조회, 미세먼지
- 로또, KBO, 환율 알림

### 🟢 P3: /daily 모닝 브리핑
- Todoist + Obsidian Tasks 통합
- Google Calendar 미팅 조회
- 데모 (VHS GIF)
