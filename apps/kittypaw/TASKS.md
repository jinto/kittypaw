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

## Plan 3: E2E Agent Loop Test

Plan: `.claude/plans/e2e-agent-loop.md`

- [ ] Mock provider + test helper + TestE2ESimpleReturn
- [ ] TestE2ESkillCall (Storage round-trip via sandbox → resolveSkillCall)
- [ ] TestE2EErrorRetry (JS error → engine retry → success)

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
