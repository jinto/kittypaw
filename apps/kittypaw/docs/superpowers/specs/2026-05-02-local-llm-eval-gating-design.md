# Local LLM Eval Gating

## Goal

KittyPaw should keep GitHub CI deterministic while still treating locally-run
LLM eval failures as real release blockers. If a developer chooses to run a live
eval, a failing score must not be waved through as "LLM randomness" without
triage.

## Test Tiers

### CI Deterministic

Runs in GitHub and must be deterministic:

```text
go test ./... -count=1
go test -tags integration ./... -count=1
go test -tags e2e ./... -count=1
golangci-lint run
make build
```

This tier must not require real LLM keys, network-only providers, or local user
state under `~/.kittypaw`.

### Local Eval

Runs manually on a developer machine with real provider credentials and local
KittyPaw state:

```text
eval/secretary_smoke/run.sh
eval/user_vision_flows/run.sh
```

This tier is not part of GitHub CI, but once run it is a gate. A `FAIL` result
means the branch is not ready until the failure is triaged and resolved.

## Eval Result States

Every eval runner should produce one of these states:

- `PASS`: behavior satisfies the rubric/threshold.
- `FAIL`: behavior does not satisfy the rubric/threshold. This is a local gate
  failure.
- `INFRA`: the eval could not judge product behavior because an external
  dependency failed, such as missing API keys, provider outage, network failure,
  server startup failure, or exhausted quota.
- `NOT_RUN`: the eval was intentionally skipped by configuration.

Exit code contract:

- `PASS` -> `0`
- `FAIL` -> `1`
- `INFRA` -> `2`
- `NOT_RUN` -> `3`

The final summary must print the state explicitly. The runner should not bury
infrastructure failures inside ordinary assertion failures.

## Failure Triage

When local eval returns `FAIL`, classify it before changing code:

1. Product regression: fix code, prompt, skill routing, or provider adapter.
2. Stale fixture: update the fixture/rubric in the same commit that explains the
   new intended behavior.
3. Provider-specific behavior: move the expectation into provider-specific
   baselines or a rubric that accepts equivalent behavior.
4. Bad assertion shape: replace brittle exact/substr checks with a contract or
   LLM-judge rubric that evaluates the behavior users care about.

Do not downgrade a `FAIL` to pass because the LLM is nondeterministic. The right
fix is to make the eval judge the correct behavior at the right abstraction
level.

## Assertion Policy

Prefer behavior contracts over exact prose:

- Good: response asks a clarification before fabricating a value.
- Good: response uses an installed skill instead of offering installation again.
- Good: response includes source/timeframe when presenting live data.
- Risky: response contains a specific emoji.
- Risky: response contains a vendor/source name that the skill no longer
  promises to expose.
- Risky: response exactly matches one provider's phrasing.

Substring checks remain acceptable for hard anti-patterns and durable output
tokens:

- Anti-patterns: "제공해주신", "you provided", repeated install offers.
- Durable tool output tokens: a documented package label or field name.

## Provider Policy

Local eval should record the provider and model used for the run. Provider
differences are expected, but they must be explicit:

- Anthropic, OpenAI, and Gemini may have separate baselines where behavior
  differences are meaningful.
- Shared rubrics should test provider-independent user outcomes.
- Provider-specific failures should not mask regressions in shared behavior.

The summary should include:

```text
Provider: openai
Model: gpt-5.5
Account: jinto
Server: local
```

## Runner UX

Add Make targets so the intended commands are discoverable:

```text
make test-ci
make eval-secretary
make eval-user-flows
make eval-local
```

`make test-ci` should run only deterministic checks. `make eval-local` should run
the local eval suite and propagate `PASS`/`FAIL`/`INFRA`/`NOT_RUN` exit codes.

Eval runners should write machine-readable result files under their existing
`results/` directories and a human-readable `summary.md`.

## Current Known Issue

`eval/user_vision_flows/run.sh` currently mixes product behavior checks with
brittle historical substrings such as `✅` and `Frankfurter`. A functional
exchange-rate response can fail the eval because the response path changed from
"install then show source string" to "installed skill dispatch with equivalent
rates". This should be converted into behavior-level checks:

- Did the flow produce exchange-rate data?
- Did the flow avoid repeated install offers when already installed?
- Did the flow avoid fabrication on ambiguous queries?
- Did the flow preserve helpful chitchat behavior?

## Non-Goals

- Running live LLM eval in GitHub CI.
- Making stochastic LLM output deterministic by pinning exact full responses.
- Treating infra failures as product passes.
- Removing deterministic unit/integration coverage that already catches prompt,
  routing, sandbox, and provider-wire regressions.
