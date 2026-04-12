## Plan 1: Skill Scheduler Wiring ✅

Plan: `.claude/plans/skill-scheduler-wiring.md`

- [x] Guard `Scheduler.Stop()` with `sync.Once` — prevent double-close panic
- [x] Add in-flight guard (`sync.Map`) — prevent concurrent runs of same skill
- [x] Handle `SetLastRun` failure for one-shot — skip execution if dedup record fails
- [x] Wire Scheduler into `server.Server` — start with cancelable ctx, stop on shutdown
- [x] Write `engine/schedule_test.go` — parseCronInterval, isDue, backoff, concurrency

## Plan 2: LLM Provider Resilience ← 현재

Plan: `.claude/plans/llm-resilience.md`

- [ ] Add jitter to Claude `doWithRetry` backoff + ctx cancellation test
- [ ] Add `doWithRetry` to OpenAI provider (429/503 retry + jitter) + tests
- [ ] Fix scanner buffer in both `parseSSEStream` (64KB→1MB max) + overflow test
- [ ] Handle SSE error events in Claude `parseSSEStream` + tests (0 tokens, N tokens)
- [ ] Handle SSE error events in OpenAI `parseSSEStream` + tests (0 tokens, N tokens)
