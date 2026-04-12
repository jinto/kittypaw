# LLM 패키지 테스트 인프라 + OpenAI Usage 버그 수정

## Goal

llm/ 패키지에 테스트 인프라를 구축하고, OpenAI 스트리밍 usage 버그를 수정한다.

## Scope

- **IN**: HTTP 클라이언트 주입 (functional option), OpenAI stream_options, 테스트 작성
- **OUT**: retry 비대칭(#2), scanner 버퍼(#3), SSE error 이벤트(#4), tool calling(#6)

## Changes

### 1. HTTP 클라이언트 주입 (테스트 인프라)

- **Claude**: `NewClaude(apiKey, model, maxTokens, ...ClaudeOption)` variadic option 패턴 도입
  - `WithClaudeHTTPClient(*http.Client)` 옵션 추가
- **OpenAI**: 기존 functional option 패턴에 `WithHTTPClient(*http.Client)` 추가
- Provider 인터페이스 자체는 변경하지 않음

### 2. OpenAI stream_options 추가

- `buildRequestBody`에서 streaming일 때 `"stream_options": {"include_usage": true}` 추가
- **Ollama guard**: `baseURL == openAIDefaultBaseURL`일 때만 추가. 커스텀 URL에서는 생략

### 3. onToken nil-guard

- Claude/OpenAI 양쪽 `parseSSEStream`에서 `onToken != nil` 체크 후 호출

### 4. 테스트

- httptest.NewServer로 Claude/OpenAI SSE 응답 mock
- JSON (non-streaming) 응답 파싱 테스트
- `stream_options.include_usage == true` 값 assertion
- 비-streaming request에 `stream_options` 부재 검증
- registry NewProvider 팩토리 테스트

## Acceptance Criteria

- `go test ./llm/...` 통과
- Claude SSE: message_start → content_block_delta → message_delta 파싱, usage 추출 정확
- OpenAI SSE: data chunks → [DONE], usage 추출 정확 (0이 아닌 값)
- onToken nil 전달 시 패닉 없음

## Multi-Perspective Review

- **Architect**: APPROVED (조건부) — Ollama guard 반영됨
- **Critic**: ITERATE → 피드백 3건 모두 반영됨
- **CEO**: APPROVED — SELECTIVE 스코프, 4/10 위치
