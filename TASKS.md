# Tasks

## Skeleton

- [x] Create initial monorepo directory shape.
- [x] Document service ownership and architecture constraints.
- [x] Add first contract placeholders and example fixtures.

## Next

- [x] Decide whether existing repositories will be imported with history or as
  snapshots.
- [x] Import existing repositories with history.
- [x] Add root-level contract verification commands.
- [x] Add migration plan for current `kittypaw`, `kittyapi`, `kittychat`, and
  `kittykakao` repositories.
- [x] Add `apps/kittypaw` release workflow plan for `kittypaw/v*` tags.
- [ ] Add initial CI path-filter strategy.

## Plan: OpenAI Function Calling ✅

> Branch `feat/openai-tool-calling` — 커밋 `00d4e48`
> Plan: `.claude/plans/openai-tool-calling.md`

- [x] **T1**: Tool 정의 직렬화 — `convertToolsToOpenAI` + `buildChatRequestBodyWithTools` + AC-10 회귀 단정
- [x] **T2a**: assistant 메시지 변환 — text-only / tool_use only / mixed / parallel / 빈 인자
- [x] **T2b**: user 메시지 변환 — tool_result 단독 / 멀티 순서 보존 / mixed text+tool_result + slog.Warn
- [x] **T3**: 응답 파싱 — tool_calls 디코드 + arguments 타입 분기 + finish_reason 매핑 + usage nil-safe
- [x] **T4**: `GenerateWithTools` end-to-end + multi-turn round-trip + parallel 응답
- [x] **T5**: 회귀 + 3-lane review fix (Marshal panic / empty id error / slog redaction note) + 커밋 완료

## Plan: Cerebras Provider ← 현재

> Plan: `.claude/plans/cerebras-provider.md`
> 변경: `apps/kittypaw/llm/registry.go` (+ `registry_test.go`)

- [ ] **C1**: registry.go에 `case "cerebras"` 추가 — NewOpenAI + base_url + ContextWindow 8192
- [ ] **C2**: envAPIKey에 `CEREBRAS_API_KEY` 추가
- [ ] **C3**: registry_test.go — provider lookup + base_url + context window 단정
- [ ] **C4**: build/test/lint + Cerebras smoke 안내 + 커밋 컨펌
