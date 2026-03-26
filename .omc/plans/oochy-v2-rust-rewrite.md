# Oochy v2: Rust Rewrite Implementation Plan

**Date:** 2026-03-26
**Status:** DRAFT v2 -- RALPLAN-DR Consensus (DELIBERATE mode -- full rewrite + security-critical sandbox)
**Supersedes:** `.omc/plans/oochy-v2-compete-openclaw.md` (Bun/TypeScript -- invalidated, now Rust)
**Revision:** Addresses Architect (ITERATE) and Critic (REJECT) feedback from consensus round 1.

---

## 1. RALPLAN-DR Summary

### Principles (5)

1. **Single binary, zero dependencies** -- `./oochy` must run with nothing else installed. No Node.js, no Python, no Docker. The binary IS the platform.
2. **Dual-layer sandbox is non-negotiable** -- LLM-generated code runs inside QuickJS (VM isolation) AND kernel-level restrictions (Landlock on Linux, Seatbelt on macOS). Neither layer alone is sufficient. QuickJS prevents language-level escape; kernel sandbox prevents syscall-level escape.
3. **Code generation is the moat** -- Every architecture decision must protect and enhance the generate-execute loop. This is what OpenClaw cannot do.
4. **Local-first, cloud-optional** -- Default experience works on a developer laptop with only an LLM API key. No cloud accounts, no containers, no orchestrators.
5. **Incremental compilation, incremental value** -- Each phase must produce a usable binary. No "big bang" where nothing works until everything works.

### Decision Drivers (Top 3)

1. **Build speed and developer iteration** -- A Rust rewrite that takes 20 minutes to compile on every change will kill momentum. Crate structure must optimize incremental compilation.
2. **Sandbox correctness before features** -- A sandbox escape in production is an existential risk. The dual sandbox must be proven correct before adding channels or dashboards.
3. **Portability (Linux + macOS)** -- Landlock (Linux) and Seatbelt (macOS) are used directly via their respective crates/syscalls. The abstraction layer must work on both without #[cfg] spaghetti.

### Viable Options

#### Option A: Monolithic Single Crate

| Pros | Cons |
|------|------|
| Simplest Cargo.toml, no workspace config | Full recompile on any change (~970 lines is small but will grow) |
| No circular dependency concerns | Cannot compile crates in parallel |
| Single `cargo build` target | Test isolation is poor -- all tests share one binary |
| Easy for new contributors | Feature flags become the only modularity mechanism |

#### Option B: Rust Workspace with Multiple Crates (CHOSEN)

| Pros | Cons |
|------|------|
| Parallel compilation of independent crates | More boilerplate (multiple Cargo.toml files) |
| Incremental builds: changing `oochy-channels` doesn't recompile `oochy-sandbox` | Must manage inter-crate dependency graph |
| Clean separation of concerns; each crate has focused tests | Slightly more complex CI setup |
| Can publish crates independently (e.g., `oochy-sandbox` as a library) | Over-splitting into too many crates is a real risk |

#### Option C: Hybrid -- Rust Core + Embedded Scripting for Extensibility

| Pros | Cons |
|------|------|
| Plugin system via embedded Lua/Rhai/WASM -- users extend without recompiling | Two scripting runtimes (QuickJS for sandbox + another for plugins) adds confusion |
| Most flexible long-term | Performance overhead of plugin bridge |
| Community can contribute plugins without Rust knowledge | Security surface area doubles -- plugins need their own sandbox |
| Matches OpenClaw's plugin ecosystem play | Premature optimization for extensibility before core is solid |

### Recommendation: Option B (Workspace) with a narrow, phased scope

**Why:** The codebase will grow well beyond 970 lines. Workspace structure pays for itself immediately in compile times and test isolation. Option A becomes painful at ~5,000 lines. Option C is premature -- plugin extensibility is a Phase 3+ concern after the core generate-execute loop is proven.

**Phase 1 workspace structure (4 crates only):**
```
oochy/
  Cargo.toml              (workspace root)
  crates/
    oochy-core/            (types, config, error handling, agent state)
    oochy-llm/             (LLM provider abstraction: Claude, GPT)
    oochy-sandbox/         (QuickJS + Landlock/Seatbelt dual sandbox)
    oochy-cli/             (binary entry point, CLI args, config loading, SQLite store)
```

Four crates for Phase 1. `oochy-cli` depends on the other three and produces the single binary. Additional crates are introduced only when their phase begins:
- **Phase 2:** Add `oochy-channels` (Telegram, Discord adapters)
- **Phase 3:** Add `oochy-web` (axum HTTP server, dashboard, static assets)
- **Phase 4:** (no new crates -- hardening and release)

SQLite state persistence lives in `oochy-cli` initially (simple module, not a full crate) and can be extracted to `oochy-store` if/when complexity warrants it.

### Pre-Mortem (6 Failure Scenarios)

**Scenario 1 (Build): QuickJS + kernel sandbox integration is harder than expected.**
The `rquickjs` crate may not expose enough control over the JS runtime. Landlock/Seatbelt abstraction may leak platform differences. *Mitigation:* Phase 0 spike validates rquickjs async behavior. Phase 1 builds QuickJS sandbox first without kernel sandbox. Phase 2 adds Landlock/Seatbelt. Fork-child-process model isolates sandbox concerns.

**Scenario 2 (Build): LLM generates JavaScript that QuickJS cannot execute.**
QuickJS supports ES2020 but not all modern JS features (e.g., certain TC39 proposals, top-level await nuances). LLM-generated code may use unsupported syntax. *Mitigation:* System prompt specifies exact JS subset (ES2020). Build a conformance test suite early. If critical features are missing, consider `boa` (Rust-native JS engine) as alternative.

**Scenario 3 (Build): Compile times kill developer velocity.**
Rust compile times with `reqwest` + `axum` + `rusqlite` + `rquickjs` can easily exceed 5 minutes for clean builds. *Mitigation:* Workspace structure enables incremental builds. Use `cargo-nextest`. Pin dependency versions. Use `mold` linker on Linux and `zld` on macOS.

**Scenario 4 (Product): LLMs generate worse JavaScript than Python, code quality drops.**
The current system uses Python code generation. LLMs have been trained heavily on Python and may produce higher-quality Python than JS. Switching to JS could degrade the quality of generated code -- more bugs, worse error handling, less idiomatic patterns. *Mitigation:* Phase 0 spike includes LLM code generation quality comparison (same 10 prompts, Python vs JS). If JS quality is substantially worse, reconsider the pivot. System prompt engineering can partially compensate. ES2020 is well-represented in training data.

**Scenario 5 (Security): Skill executor has no rate limiting, rogue agent spams external APIs.**
A compromised or poorly-prompted agent could generate code that calls `Telegram.sendMessage()` in a tight loop, exhausting API quotas or getting banned. *Mitigation:* Capability model (ADR below) includes per-skill rate limits. Default: 10 calls/minute per skill per agent. Configurable in `oochy.toml`.

**Scenario 6 (Scope): Rewrite takes 4x longer than estimated, Python codebase rots.**
Full rewrites are notorious for scope creep. While the Rust rewrite progresses, the Python codebase receives no updates, users on the Python version get no fixes. *Mitigation:* Phase 1 targets a working binary in 2-3 weeks. If Phase 1 is not complete in 4 weeks, reassess scope. Python codebase remains functional as-is -- it is not being actively developed regardless.

### Expanded Test Plan

**Unit tests (per crate):**
- `oochy-core`: Type serialization roundtrips, config parsing, event construction, capability model validation
- `oochy-llm`: Response parsing, code fence stripping, retry logic (mock HTTP)
- `oochy-sandbox`: QuickJS execution (success, timeout, syntax error, resource limits), skill stub capture, async promise resolution
- `oochy-cli` (store module): SQLite CRUD, WAL mode verification, migration up/down

**Integration tests:**
- Full agent loop: mock LLM response -> QuickJS execution -> state persistence -> response
- Skill capability enforcement: agent attempts unauthorized skill call -> rejected with clear error
- Sandbox escape attempts at kernel layer: file read outside allowed paths via raw syscall (not `require('fs')` which doesn't exist in QuickJS), network socket creation (not `fetch()` which doesn't exist in QuickJS)
- Async skill roundtrip: JS `await Telegram.sendMessage(...)` -> host executes -> Promise resolves in JS

**E2E tests:**
- Binary starts, accepts stdin event, responds with result
- Telegram bot responds to a real test message (requires test bot token, CI-only)
- Web dashboard serves static assets, WebSocket connects (Phase 3)

**Observability:**
- Structured logging via `tracing` crate (spans for each agent loop phase)
- Metrics: request count, LLM latency, sandbox execution time, error rates, skill call counts
- Health endpoint with uptime, version, active agents count

---

## 2. ADR: Python-to-JavaScript Code Generation Pivot

### Decision

Switch the LLM code generation target from Python to JavaScript (ES2020), executed in QuickJS.

### Drivers

1. **Single binary requirement** -- Python requires a Python interpreter. Embedding CPython in Rust is possible (via `pyo3`) but adds ~30MB to the binary and requires bundling the Python stdlib. QuickJS adds ~1MB.
2. **Sandbox model** -- QuickJS provides a clean, minimal JS runtime where capabilities are explicitly injected. Python's stdlib is enormous and exposes filesystem, network, subprocess, and OS primitives by default. Sandboxing Python means removing hundreds of modules; sandboxing QuickJS means adding only what you want.
3. **Startup performance** -- QuickJS context creation is <1ms. Python interpreter startup is 30-100ms. For a system that may execute code on every message, this matters.

### Alternatives Considered

- **Keep Python, sandbox via subprocess** -- Fork a Python subprocess with seccomp/Landlock restrictions. Pros: LLMs generate excellent Python; mypy type-checking possible. Cons: Requires Python installed on host (violates single-binary principle); subprocess IPC adds latency and complexity; Python stdlib attack surface is massive even with restrictions.
- **Keep Python, embed via pyo3** -- Embed CPython in the Rust binary. Pros: No external Python needed. Cons: Binary size balloons (~30MB+); Python GIL complicates async; still must restrict Python stdlib modules one by one.
- **WASM sandbox** -- Compile JS/Python to WASM, run in wasmtime. Pros: Strong sandbox. Cons: WASM toolchain adds build complexity; debugging WASM-compiled code is poor; LLM-generated code targeting WASM is unusual.

### Impact Assessment

| Dimension | Python (current) | JavaScript (proposed) |
|-----------|-----------------|----------------------|
| LLM code quality | Excellent -- Python is the most common LLM training target | Good -- JS is #2; ES2020 is well-represented in training data |
| Type safety | mypy static checking possible | No TypeScript in QuickJS; runtime-only type errors |
| Stdlib richness | Massive (and a security liability) | None by default (and a security advantage) |
| Binary size impact | +30MB (embedded CPython) | +1MB (QuickJS) |
| Sandbox complexity | High (must deny-list hundreds of modules) | Low (must allow-list only desired capabilities) |
| Async model | asyncio (complex, GIL) | Promises (simple, single-threaded) |

### Acknowledged Losses

- **mypy type-checking** of generated code is lost. Compensated by: runtime error retry loop (feed errors back to LLM for correction).
- **Python ecosystem familiarity** -- many users expect Python. Compensated by: JS is the world's most widely-known language; the code is generated by LLM, not written by users.
- **Scientific computing libraries** (numpy, pandas) are unavailable. Acceptable for v1 scope (agent tasks, not data science).

### Consequences

- System prompt must be rewritten: "You are Oochy, an AI agent that helps users by writing JavaScript (ES2020) code"
- All skill type stubs must be rewritten from Python `.pyi` to JS JSDoc annotations
- Test suite must validate JS code generation quality
- Phase 0 spike must include LLM code quality comparison

### Follow-ups

- Post-v1: Evaluate SWC/oxc transpilation to support TypeScript in QuickJS
- Post-v1: Consider multi-language support (Python subprocess as opt-in non-sandboxed mode for trusted environments)

---

## 3. ADR: Skill Capability Model

### Decision

Implement a declarative, per-agent capability model that governs which skills an agent can invoke, with rate limiting, argument validation, and permission scoping.

### Design

#### Capability Declaration (in `oochy.toml`)

```toml
[agents.support-bot]
name = "Support Bot"
system_prompt = "You help users with product questions."

[agents.support-bot.capabilities]
# Explicit allow-list of skills this agent can use
skills = ["Telegram.sendMessage", "Telegram.sendPhoto", "Http.get"]

# Per-skill rate limits (calls per minute)
[agents.support-bot.capabilities.rate_limits]
"Telegram.sendMessage" = 10
"Telegram.sendPhoto" = 5
"Http.get" = 20

# Argument constraints
[agents.support-bot.capabilities.constraints]
"Http.get.url" = { allow_patterns = ["https://api.example.com/*"] }
"Telegram.sendMessage.chat_id" = { allow_values = ["123456789", "987654321"] }
```

#### Enforcement Architecture

```
JS code calls: await Telegram.sendMessage(chatId, text)
        |
        v
QuickJS stub captures: { skill: "Telegram.sendMessage", args: [chatId, text] }
        |
        v
Host Rust receives captured calls after sandbox exits
        |
        v
Capability Checker (oochy-core):
  1. Is "Telegram.sendMessage" in this agent's allowed skills? NO -> CapabilityDenied error
  2. Rate limit check: has this agent exceeded 10 calls/min? YES -> RateLimitExceeded error
  3. Argument validation: does chatId match allow_values? NO -> ArgumentConstraintViolation error
  4. All checks pass -> execute real Telegram API call
        |
        v
Result flows back to agent loop (NOT back into JS -- sandbox has already exited)
```

#### Error Behavior

| Scenario | Behavior |
|----------|----------|
| JS calls nonexistent skill (e.g., `Slack.post()`) | Stub is not injected into QuickJS globals. JS gets `ReferenceError: Slack is not defined`. Error fed back to LLM for retry. |
| JS calls existing but unauthorized skill | Stub IS injected (so LLM sees it in type stubs). Call is captured. Host rejects at capability check. Error reported in `ExecutionResult.skill_errors`. |
| Rate limit exceeded | Call rejected. Error includes retry-after hint. Logged as warning. |
| Argument constraint violation | Call rejected. Error describes which constraint failed. |

#### Default Capabilities

If no `[capabilities]` section is configured for an agent, defaults apply:
- All registered skills are allowed
- Rate limit: 30 calls/minute per skill
- No argument constraints

This ensures backward compatibility and a frictionless getting-started experience.

### Drivers

1. Security: A rogue or poorly-prompted agent must not be able to spam external APIs or access resources outside its scope.
2. Multi-tenancy: Different agents serve different purposes and should have different permissions.
3. Debuggability: Capability denials must produce clear, actionable error messages.

### Consequences

- Every skill call goes through a capability check (minimal overhead: HashMap lookup + counter check).
- Configuration complexity increases for multi-agent setups.
- Skill stubs are always injected regardless of permissions (so LLM knows they exist), but execution is gated.

### Follow-ups

- Post-v1: Dynamic capability grants (e.g., user approves a skill call via dashboard)
- Post-v1: Capability audit log (which agent called which skill, when, with what args)

---

## 4. Intentional Feature Losses (Python v1 -> Rust v2)

The following features from the Python codebase are intentionally dropped in v2. Each is documented with rationale.

| Dropped Feature | Where in Python Codebase | Why Acceptable |
|----------------|-------------------------|----------------|
| **mypy type-checking** of generated code | Type hints in Pydantic models, `.pyi` stubs | Replaced by runtime error retry loop. LLM corrects JS errors via feedback. |
| **MQTT integration** | `channels/mqtt.py` | Low adoption. Can be added as a channel adapter post-v1 if demand exists. |
| **S3 state storage** | `store/s3.py` | Local-first principle. SQLite WAL is the primary store. S3 backup is a post-v1 feature. |
| **Lambda sandbox execution** | `executor.py` (Lambda-based execution) | Replaced by local QuickJS + kernel sandbox. No cloud dependency. This is a deliberate architectural shift, not a loss. |
| **JWT auth for web API** | `web/auth.py` | v2 dashboard is localhost-only by default. Auth is a post-v1 concern when remote access is added. |
| **Python code generation** | `prompt.py` ("writing Python code") | Replaced by JavaScript (ES2020) generation. See ADR above. |
| **Docker-based isolation** | `sandbox/docker.py` | Replaced by Landlock/Seatbelt kernel sandbox. No Docker dependency. |

---

## 5. Error Recovery Model

### QuickJS Errors

| Error | Detection | Recovery |
|-------|-----------|----------|
| **JS syntax error** | `rquickjs` returns `Err` with parse error | Feed error message back to LLM for code correction (max 3 retries) |
| **JS runtime error** (TypeError, ReferenceError, etc.) | `rquickjs` returns `Err` with runtime error | Feed error message back to LLM for code correction (max 3 retries) |
| **JS infinite loop** | `tokio::time::timeout(30s)` fires | Kill QuickJS context. Return timeout error to agent loop. Do NOT retry (infinite loops are not fixable by LLM). |
| **JS OOM inside QuickJS** | `rquickjs` memory limit callback fires | QuickJS aborts execution. Return OOM error. Do NOT retry. Log warning with code snippet. |
| **QuickJS panic (rquickjs bug)** | `std::panic::catch_unwind` around sandbox call | Log panic with backtrace. Return internal error. Do NOT retry. File bug against rquickjs. |

### Sandbox Process Errors

Since the kernel sandbox uses a **fork-child-process model** (see Architecture section):

| Error | Detection | Recovery |
|-------|-----------|----------|
| **Child process crash** | `waitpid` returns abnormal exit | Log crash. Return sandbox error. Retry once (transient issues). |
| **Child process hang** | Timeout on child (35s, slightly longer than JS timeout) | Kill child via SIGKILL. Return timeout error. |
| **Fork failure** | `fork()` returns error | Log error. Return sandbox unavailable. Retry after 1s backoff (max 3). |

### LLM API Errors

| Error | Detection | Recovery |
|-------|-----------|----------|
| **HTTP 429 (rate limit)** | Response status code | Exponential backoff: 1s, 2s, 4s, 8s, 16s. Max 5 retries. Use `Retry-After` header if present. |
| **HTTP 500/502/503 (server error)** | Response status code | Retry 3 times with 2s backoff. If persistent, return error to user ("LLM service temporarily unavailable"). |
| **HTTP 401 (auth error)** | Response status code | Do NOT retry. Log error. Return clear message: "Invalid API key." |
| **Timeout (no response in 60s)** | `reqwest` timeout | Retry once. If second attempt also times out, return error to user. |
| **Malformed response (no code block)** | Response parsing | Feed back to LLM: "Your response must contain a JavaScript code block." Retry (counts toward 3 retry limit). |

### Channel Delivery Errors

| Error | Detection | Recovery |
|-------|-----------|----------|
| **Telegram API error** | HTTP error from Telegram API | Retry 2 times with 1s backoff. If persistent, log error. Do NOT crash the agent loop. |
| **Discord gateway disconnect** | WebSocket close event | Reconnect with exponential backoff (serenity/twilight handle this internally). |
| **WebSocket client disconnect** | Connection close | Clean up session. No retry needed (client will reconnect). |

### SQLite Errors

| Error | Detection | Recovery |
|-------|-----------|----------|
| **Lock contention (SQLITE_BUSY)** | `rusqlite` error | Retry with `busy_timeout(5000)` (5s). WAL mode minimizes this. If persistent, log warning. |
| **Disk full** | Write error | Log critical error. Continue operating with in-memory state (degraded mode). Alert via dashboard. |
| **Corruption** | Integrity check failure on startup | Log critical error. Attempt `PRAGMA integrity_check`. If unrecoverable, start with fresh database and log data loss warning. |

### Tokio Supervision Strategy

The top-level Tokio runtime supervises all tasks:

```
tokio::main
  |
  +-- Agent Loop Task (one per agent)
  |     On panic: log, restart after 5s backoff, max 3 restarts then disable agent
  |
  +-- Channel Listener Tasks (one per channel)
  |     On panic: log, restart after 5s backoff, max 5 restarts
  |
  +-- Web Server Task (one)
  |     On panic: log, restart after 1s (dashboard is non-critical)
  |
  +-- Graceful Shutdown Handler
        On SIGTERM/SIGINT: cancel all tasks, drain in-flight requests (10s timeout), exit
```

Channel backpressure: event channels (`mpsc`) are bounded at 256 messages. Drop policy: newest message dropped when full (log warning). This prevents a burst of incoming messages from exhausting memory.

---

## 6. Implementation Phases

### Phase 0: Feasibility Spike (BEFORE any Phase 1 code)

**Goal:** Validate the two highest-risk technical assumptions before committing to the architecture. This is a throwaway spike -- code quality does not matter, only proving/disproving feasibility.

**Duration:** 2-3 days maximum. If either spike fails, reassess the architecture.

**Spike 0.1: rquickjs Async Skill Roundtrip**

Create a minimal Rust binary that proves:

1. A JavaScript function `await Telegram.sendMessage(chatId, text)` can be called inside rquickjs
2. The `Telegram.sendMessage` host function suspends JS execution
3. The host Rust code executes a real HTTP call (mock Telegram API endpoint) as a Tokio future
4. The result flows back as a resolved Promise in JS
5. JS code after the `await` line receives the result and continues
6. This works cooperatively with the Tokio runtime (no thread blocking)

**Acceptance criteria:**
- Working Rust binary (single file, no crate structure needed)
- JS code: `const result = await Telegram.sendMessage("123", "hello"); console.log(result.ok);`
- Host function makes actual HTTP request (to httpbin.org or similar)
- Promise resolves with the HTTP response data
- Total roundtrip completes without deadlock or thread starvation
- Works with `#[tokio::main]` (multi-threaded runtime)

**If spike fails:** Evaluate alternatives: (a) `boa` engine (Rust-native JS, different async model), (b) synchronous skill model (JS returns skill call descriptors, host executes after JS exits, no `await`), (c) message-passing model (JS posts skill request to channel, polls for result).

**Spike 0.2: Landlock/Seatbelt Fork-Child Sandbox**

Create a minimal Rust binary that proves:

1. Parent process forks a child process
2. Child process applies Landlock restrictions (Linux) or Seatbelt profile (macOS) -- restrictions are IRREVERSIBLE per-process, hence the fork
3. Child process executes QuickJS code within the restricted context
4. Child attempts to read `/etc/passwd` -- fails with permission error
5. Child attempts to open a network socket -- fails with permission error
6. Child returns execution result to parent via pipe/shared memory
7. Parent process remains unrestricted

**Acceptance criteria:**
- Working Rust binary (single file)
- On Linux: uses `landlock` crate (not nono.sh)
- On macOS: uses `sandbox-exec` / `sandbox_init` (Seatbelt)
- Filesystem restriction verified (read outside allowed paths fails)
- Network restriction verified (socket creation fails)
- Parent-child IPC works (result communicated back)
- Child process exits cleanly after execution

**If spike fails:** (a) Use `seccomp` instead of Landlock on Linux, (b) Use process-level `pledge` on supported systems, (c) Fall back to QuickJS-only sandbox with no kernel layer (documented security limitation).

**Spike 0.3: LLM JavaScript Code Quality Comparison**

Using the existing Python codebase's prompt structure, test 10 representative prompts with Claude:

1. "Send a welcome message to the user"
2. "Fetch weather data and summarize it"
3. "Parse the user's message for a date and set a reminder"
4. "Handle an error gracefully and inform the user"
5. (6 more covering the range of expected agent tasks)

For each prompt, generate code in both Python (current) and JavaScript (ES2020). Compare:
- Correctness (does the code do what was asked?)
- Idiomatic quality (is it natural, not weird?)
- Error handling quality
- Use of unsupported features (top-level await, require(), etc.)

**Acceptance criteria:**
- JS quality is within 80% of Python quality across all 10 prompts (subjective but documented assessment)
- No more than 2 prompts produce JS code using features unsupported by QuickJS
- System prompt adjustments identified that improve JS code quality

**If spike fails:** Reconsider Python-to-JS pivot. Options: (a) embedded Python via pyo3 (accept binary size cost), (b) dual-language support (JS for sandbox, Python for trusted mode).

---

### Phase 1: Foundation -- Core Loop + QuickJS Sandbox
**Goal:** `./oochy` binary that accepts a hardcoded event, calls Claude API, generates JS code, executes in QuickJS, returns result. No channels, no web UI, just the beating heart.

**Complexity:** HIGH (Rust project setup + QuickJS embedding + LLM integration)

**Tasks:**

**1.1 Initialize Rust workspace and crate structure**
- Create workspace with 4 crates: `oochy-core`, `oochy-llm`, `oochy-sandbox`, `oochy-cli`
- Set up shared dependencies: `tokio`, `serde`, `thiserror`, `tracing`
- Configure `cargo-nextest`, `clippy`, `rustfmt`
- *Acceptance:* `cargo build` succeeds. `cargo test` runs (empty tests pass). `cargo clippy` clean. Binary at `target/release/oochy` exists.

**1.2 Port core types and capability model to Rust (oochy-core)**
- Port `AgentState`, `ConversationTurn`, `Event`, `EventType`, `LLMMessage`, `SkillDefinition` from Python Pydantic models to Rust structs with `serde::Serialize/Deserialize`
- Implement `AgentState::add_turn()` and `AgentState::recent_turns(n)`
- Define `OochyError` enum with `thiserror` for unified error handling
- Define `Config` struct (LLM API key, model, timeout, allowed paths, allowed hosts) loaded from env vars and optional `oochy.toml`
- Implement `CapabilityChecker`: skill allow-list, rate limiter (token bucket per skill per agent), argument validator
- *Acceptance:* All types serialize/deserialize to JSON. `AgentState` conversation management works. Config loads from env and file with env override. Capability checker rejects unauthorized skills, enforces rate limits, validates arguments.

**1.3 Implement LLM provider abstraction (oochy-llm)**
- Define `LlmProvider` trait with `async fn generate(&self, messages: Vec<LlmMessage>) -> Result<String>`
- Implement `ClaudeProvider` using `reqwest` (direct HTTP, not the Anthropic SDK -- avoid heavy dependency)
- Implement `OpenAiProvider` for GPT models (same `reqwest` approach)
- Implement code fence stripping (new utility -- not a port; `_strip_code_fences()` does not exist in the current Python codebase)
- *Acceptance:* Given messages, calls Claude API and returns generated JS code. Code fences stripped. HTTP errors handled with retries (exponential backoff for 429, 3 retries for 5xx). Provider is swappable via config.

**1.4 Implement QuickJS sandbox with fork-child-process model (oochy-sandbox)**
- Embed QuickJS via `rquickjs` crate
- Implement fork-child-process execution model:
  - Parent forks child process
  - Child applies Landlock (Linux) or Seatbelt (macOS) restrictions -- these are IRREVERSIBLE, hence fork
  - Child creates QuickJS runtime and executes code
  - Child returns result to parent via pipe (serde JSON serialization)
  - Parent remains unrestricted
- Implement `Sandbox::execute(code: &str, context: serde_json::Value, capabilities: &AgentCapabilities) -> Result<ExecutionResult>`
- Inject context as global `_context` object in JS runtime
- Implement 30-second timeout via `tokio::time::timeout` (parent kills child after 35s as fallback)
- Restrict QuickJS runtime: limit memory (configurable, default 64MB)
- Expose skill stubs as JS globals (Telegram.sendMessage, etc.) that use async/await pattern validated in Phase 0 spike
- On macOS: graceful fallback if Seatbelt is unavailable (QuickJS-only with logged warning)
- On Linux: graceful fallback if kernel is too old for Landlock (QuickJS-only with logged warning)
- *Acceptance:* JS code executes and returns result. Timeout fires at 30s. Memory limit enforced. Skill calls captured via async stubs and returned to host for execution. Syntax errors reported cleanly. Filesystem access outside allowed paths blocked by kernel sandbox. Network access blocked by kernel sandbox.

**1.5 Implement SQLite state store (module in oochy-cli)**
- Use `rusqlite` with `bundled` feature (zero external deps)
- WAL mode enabled on connection
- `busy_timeout(5000)` for lock contention handling
- Schema: `agents` table (agent_id, state_json, created_at, updated_at), `conversations` table (id, agent_id, role, content, code, result, timestamp)
- Implement `Store::load_state(agent_id)`, `Store::save_state(state)`, `Store::recent_turns(agent_id, n)`
- Simple migration system (SQL files embedded via `include_str!`)
- *Acceptance:* State persists across process restarts. WAL mode confirmed. `busy_timeout` configured. 1000 conversation turns write/read in <100ms.

**1.6 Wire the agent loop (oochy-cli)**
- Implement `run_agent_loop()`: event -> load state -> build prompt -> LLM generate -> QuickJS execute -> capability check on captured skill calls -> execute approved skill calls on host -> save state
- Implement retry loop with max 3 type-error retries (feed JS errors back to LLM)
- Implement `build_prompt()` with skill context injection and JavaScript (ES2020) system prompt
- System prompt: "You are Oochy, an AI agent that helps users by writing JavaScript (ES2020) code. You have access to the following skills: ..."
- CLI accepts a test event via stdin JSON for manual testing
- *Acceptance:* `echo '{"type":"web_chat","payload":{"text":"hello"}}' | ./oochy` calls Claude, generates JS, executes in QuickJS sandbox (with kernel isolation), captured skill calls pass capability check, approved calls executed on host, result printed. Retry loop works on type errors.

**Dependencies:** 1.1 -> 1.2 -> 1.3 + 1.4 + 1.5 (parallel) -> 1.6

---

### Phase 2: Channels
**Goal:** Telegram and Discord bots respond to real messages.

**Complexity:** MEDIUM

**Tasks:**

**2.1 Implement channel adapter trait and Telegram channel (new crate: oochy-channels)**
- Create `oochy-channels` crate
- Define `Channel` trait: `async fn start(&self, event_tx: Sender<Event>)`, `async fn send(&self, agent_id: &str, response: &str)`
- Implement `TelegramChannel` with both webhook mode (for servers) and long-polling mode (for local dev)
- Implement `Telegram.sendMessage()` and `sendVoice()` as skill implementations that execute real API calls when the host processes captured skill calls
- Bounded channel: 256 message buffer, drop newest on overflow with warning log
- *Acceptance:* Send message to Telegram bot -> LLM generates JS -> QuickJS executes -> skill calls captured -> capability check passes -> host executes Telegram API call -> result sent back to Telegram chat. Long-polling works without a public URL.

**2.2 Implement Discord channel (oochy-channels)**
- Implement `DiscordChannel` using Discord Gateway API via `serenity` or `twilight` crate
- Support DM and server channel messages
- Skill implementations for Discord-specific actions (send message, add reaction)
- *Acceptance:* Discord bot comes online, responds to messages with code-generated responses. Works in both DMs and server channels.

**2.3 Implement WebSocket channel for web chat (oochy-channels)**
- WebSocket endpoint at `/ws/chat` (requires minimal axum setup in oochy-cli, not full web crate yet)
- Simple message protocol: `{type: "message", text: "...", session_id: "..."}`
- Session management via SQLite (session_id -> agent_id mapping)
- *Acceptance:* WebSocket client connects, sends message, receives agent response. Sessions persist across reconnects.

**Dependencies:** 2.1, 2.2, 2.3 can run in parallel. All depend on Phase 1 completion.

---

### Phase 3: Web Dashboard + Skill System Polish
**Goal:** Web dashboard at localhost:3000 shows conversations and generated code. Skill system fully documented and configurable.

**Complexity:** MEDIUM

**Tasks:**

**3.1 Implement embedded web dashboard (new crate: oochy-web)**
- Create `oochy-web` crate
- Serve static assets embedded in binary via `rust-embed` or `include_dir`
- Dashboard: **vanilla HTML/CSS/JS only** -- no framework, no build step, no npm. Preserves zero-deps-at-build-time principle.
- Pages: Agent list, Conversation view (messages + generated code + execution results), System status
- API routes: `GET /api/agents`, `GET /api/agents/:id/conversations`, `GET /api/health`
- SQLite connection pooling: use `r2d2-sqlite` or a simple `Arc<Mutex<Connection>>` pool for concurrent dashboard reads alongside agent writes
- *Acceptance:* `./oochy` starts, open `localhost:3000`, see list of agents, click into conversation, see messages with generated JS code and execution output side by side.

**3.2 Implement skill stub system (oochy-core + oochy-sandbox)**
- Skill definitions as JSDoc-annotated JavaScript snippets embedded in the binary (NOT `.d.ts` files -- QuickJS does not support TypeScript)
- When building the LLM prompt, include skill type information so the LLM knows available methods and their signatures
- Skill registry loads available skills and builds prompt context
- *Acceptance:* LLM generates `const result = await Telegram.sendMessage(chatId, "hello")` in JS. QuickJS async stub captures the call. Host Rust code executes the real Telegram API call (after capability check). Result available in `ExecutionResult.skill_results`.

**3.3 Configuration and multi-agent support (oochy-core + oochy-cli)**
- `oochy.toml` configuration file: agents (name, system prompt, channels, capabilities), LLM settings, sandbox settings, channel credentials
- Multiple agents with different system prompts and capability sets routed to different channels
- CLI commands: `./oochy serve` (default), `./oochy config check`, `./oochy agent list`
- *Acceptance:* Configure two agents in `oochy.toml` -- one for Telegram (limited skills), one for Discord (different skills). Each responds with its own personality and permissions. `./oochy config check` validates config including capability definitions.

**Dependencies:** 3.1, 3.2, 3.3 can run in parallel. All depend on Phase 2 completion.

---

### Phase 4: Hardening + Release
**Goal:** Security audit, performance testing, documentation, first public release.

**Complexity:** MEDIUM

**Tasks:**

**4.1 Security hardening**
- Sandbox escape test suite (comprehensive):
  - Filesystem traversal via raw syscall (child process level, not JS `require('fs')` which doesn't exist)
  - Network exfiltration via raw socket creation (child process level, not JS `fetch()` which doesn't exist)
  - Resource exhaustion (memory bomb, CPU spin)
  - Prototype pollution in QuickJS
  - QuickJS CVE checks against pinned version
  - Capability model bypass attempts (skill call injection, rate limit circumvention)
- Fuzz testing with `cargo-fuzz` on sandbox input (JS code strings)
- Audit Landlock policy (Linux) and Seatbelt profile (macOS) for completeness
- *Acceptance:* Zero sandbox escapes in test suite. Zero capability bypasses. Fuzz testing runs for 1 hour with no crashes.

**4.2 Observability and operational readiness**
- Structured logging with `tracing` + `tracing-subscriber` (JSON output for production)
- Metrics endpoint (`/metrics` in Prometheus format via `metrics` crate)
- Graceful shutdown (drain connections, finish in-flight agent loops, 10s timeout)
- *Acceptance:* Logs show structured spans for each agent loop phase. Metrics endpoint reports request count, latency percentiles, error rate, skill call counts per agent. SIGTERM triggers clean shutdown.

**4.3 Documentation and release**
- README with quickstart: download binary, set API key, run `./oochy`
- `oochy.toml` reference documentation (including capabilities configuration)
- GitHub Actions CI: build matrix (Linux x86_64, Linux aarch64, macOS x86_64, macOS aarch64), test, release binaries
- *Acceptance:* GitHub release with pre-built binaries for 4 targets. README gets user from zero to working Telegram bot in under 5 minutes.

**Dependencies:** 4.1 + 4.2 parallel, then 4.3.

---

## 7. Architecture Decisions

### Async Runtime
**Tokio** (multi-threaded). The agent loop is I/O-bound (LLM API calls, channel webhooks, SQLite). Tokio is the ecosystem standard; axum and reqwest both require it. Use `#[tokio::main]` in `oochy-cli`.

### Error Handling
Unified `OochyError` enum in `oochy-core` using `thiserror`. Each crate defines its own error type that converts `Into<OochyError>`. Public API boundaries return `Result<T, OochyError>`. Internal crate code uses `anyhow` for convenience where error variants don't matter to callers.

### Configuration
Layered: defaults -> `oochy.toml` -> environment variables (env overrides file). Use `config` crate or simple custom loader. Config struct is validated at startup -- fail fast on bad config.

### QuickJS + Kernel Sandbox Integration (Fork-Child Model)

```
Agent Loop
  |
  v
oochy-sandbox::execute(code, context, capabilities)
  |
  +-- 1. Parent forks child process
  |
  +-- [CHILD PROCESS - from here, restrictions are IRREVERSIBLE]
  |     2. Apply Landlock policy (Linux) or Seatbelt profile (macOS)
  |        - Filesystem: only allowed_paths readable/writable
  |        - Network: all socket creation denied
  |        - No new process creation
  |     3. Create QuickJS runtime with memory limit (64MB default)
  |     4. Inject context as JS global `_context`
  |     5. Inject skill stubs as async JS globals (Telegram, Discord, Http, etc.)
  |     6. Execute code
  |     7. Collect skill call descriptors captured by stubs
  |     8. Serialize result (output + skill calls + errors) to pipe
  |     9. Exit child process
  |
  +-- [PARENT PROCESS - unrestricted]
  |     10. Read result from pipe (with 35s timeout, kill child on timeout)
  |     11. For each captured skill call:
  |         a. Capability check (allow-list, rate limit, argument validation)
  |         b. If approved: execute real API call in host Rust
  |         c. If denied: record error in ExecutionResult
  |     12. Return ExecutionResult { success, output, skill_results, skill_errors }
```

Key insights:
- **Landlock restrictions are IRREVERSIBLE per-process.** The fork-child model ensures the parent (and thus the main Oochy process) is never restricted.
- **Skill calls are NOT executed inside the sandbox.** The JS stubs capture call descriptors (method name + args). After sandbox execution completes and the child exits, the parent executes approved calls outside the sandbox.
- **Cross-platform consistency:** Both Linux (Landlock) and macOS (Seatbelt) use the same fork-child pattern, differing only in which kernel API is called in step 2.
- **Async skill calls in JS:** The Phase 0 spike validates that `await skillCall()` works in rquickjs. If the synchronous fallback model is needed (stubs return descriptors, no `await`), the architecture still works -- just with a different JS API surface.

### Embedded Web Assets
Use `rust-embed` to include the dashboard's HTML/CSS/JS at compile time. The dashboard is vanilla HTML/CSS/JS with no framework and no build step. Assets are served by axum from memory. Zero filesystem dependency at runtime.

### Channel Adapter Architecture
```rust
#[async_trait]
pub trait Channel: Send + Sync {
    /// Start listening for incoming events, send them to event_tx
    async fn start(&self, event_tx: mpsc::Sender<Event>) -> Result<()>;

    /// Send a response back through this channel
    async fn send_response(&self, agent_id: &str, response: &str) -> Result<()>;

    /// Channel identifier (e.g., "telegram", "discord", "web")
    fn name(&self) -> &str;
}
```

Channels are started as Tokio tasks. They push `Event`s into a bounded `mpsc` channel (capacity: 256). Drop policy: log warning and drop newest message when buffer is full. The agent loop consumes events, processes them, and calls `send_response()` on the originating channel. This decouples the agent loop from any specific channel implementation.

---

## 8. Risk Analysis

| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| **rquickjs async/await doesn't work as expected** | CRITICAL | MEDIUM | Phase 0 spike validates this BEFORE any Phase 1 code. Fallback: synchronous skill descriptor model. |
| **Landlock/Seatbelt integration complexity** | HIGH | MEDIUM | Phase 0 spike validates fork-child model. Direct crate usage (not nono.sh). Fallback: QuickJS-only sandbox with security disclaimer. |
| **LLMs generate worse JS than Python** | HIGH | MEDIUM | Phase 0 spike compares code quality. If JS is substantially worse, reconsider pivot. System prompt engineering compensates. |
| **QuickJS ES2020 limitations** | MEDIUM | LOW | Document supported JS subset in system prompt. Build conformance test suite. Most LLM-generated code uses basic syntax. |
| **Sandbox escape via QuickJS vulnerability** | CRITICAL | LOW | Dual-layer defense (kernel sandbox catches what QuickJS misses). Pin QuickJS version. Monitor CVE database. Fuzz test in Phase 4. |
| **Compile time degradation** | LOW | HIGH | Workspace structure helps. Use `mold`/`zld` linker. Profile compile times in CI. Split `reqwest` features. |
| **Rewrite takes 4x longer, Python rots** | HIGH | MEDIUM | Phase 1 targets 2-3 week completion. Reassess at 4 weeks. Python codebase is stable as-is. |
| **Skill executor rate limiting bypassed** | MEDIUM | LOW | Capability model enforces per-skill rate limits outside the sandbox. Token bucket algorithm. Unit tested. |
| **SQLite WAL contention under dashboard load** | LOW | MEDIUM | `busy_timeout(5000)`. Connection pooling for dashboard reads. WAL mode allows concurrent reads. |

### Security-Specific Risks

| Attack Vector | Sandbox Layer | Mitigation |
|---------------|--------------|------------|
| File read outside allowed paths | Kernel (Landlock/Seatbelt) in child process | Allowlist-only filesystem access. Irreversible per-child. |
| Network exfiltration | Kernel (socket creation denied) + skill interception | No network access from child process. All network goes through captured skill calls executed by parent after capability check. |
| Infinite loop / resource exhaustion | QuickJS memory limit + parent timeout | 64MB memory cap. 30s JS timeout. 35s child kill timeout. |
| Prototype pollution / JS engine escape | QuickJS VM isolation + kernel sandbox | QuickJS runs in separate child process with kernel restrictions. Even if QuickJS is compromised, kernel sandbox limits damage. |
| Skill call spam (API abuse) | Capability model rate limiting | Token bucket per skill per agent. Default 30 calls/min. Configurable. |
| Unauthorized skill access | Capability model allow-list | Per-agent skill allow-list in `oochy.toml`. Default: all skills allowed with rate limits. |
| Timing side-channels | Out of scope for v1 | Document as known limitation. |

---

## 9. ADR: Architecture Decision Record

### Decision
Rewrite Oochy as a Rust workspace producing a single binary with embedded QuickJS (VM isolation) + Landlock/Seatbelt (kernel isolation) dual sandbox in a fork-child-process model, SQLite state store, declarative capability model for skill access control, and channel adapters for Telegram, Discord, and Web.

### Drivers
1. Single binary distribution with zero external dependencies is the strongest possible developer experience for a local-first tool
2. Rust's safety guarantees and performance are critical for a system that executes untrusted LLM-generated code
3. Dual-layer sandboxing (VM + kernel) in a fork-child model provides defense-in-depth with irreversible restrictions
4. Declarative capability model prevents rogue agents from abusing external APIs

### Alternatives Considered
- **Bun/TypeScript (previous plan):** Good DX, `npx` distribution. But requires Node.js/Bun runtime installed. Sandbox relies on Docker or OS-level subprocess isolation -- no embedded VM. Single binary not achievable. *Invalidated by Rust decision.*
- **Go single binary:** Faster compile times than Rust. But no QuickJS bindings as mature as `rquickjs`. GC pauses in the agent loop. Less ecosystem for embedding JS runtimes. *Viable but inferior for sandbox embedding.*
- **Python code generation with pyo3:** Keep Python as generation target, embed CPython. Binary size +30MB, Python stdlib attack surface massive, GIL complicates async. *Rejected -- see Python-to-JS pivot ADR.*
- **nono.sh for kernel sandbox:** Immature, may not be embeddable, unclear Rust SDK status. *Rejected in favor of direct Landlock/Seatbelt usage. Can be re-evaluated post-v1.*
- **Monolithic single crate (Option A):** Simpler but compile times degrade quickly. No parallel compilation. *Rejected in favor of workspace.*
- **Hybrid Rust + plugin scripting (Option C):** Premature extensibility. Two scripting runtimes adds confusion and security surface. *Deferred to post-v1.*

### Why Rust Workspace (Option B) with 4 Initial Crates
- Incremental compilation keeps developer iteration fast as codebase grows
- Each crate has focused tests and clear API boundaries
- `oochy-sandbox` can be published independently as a library
- Single binary output despite multi-crate structure (one `[[bin]]` in `oochy-cli`)
- Starting with 4 crates (not 7) avoids premature abstraction; additional crates added when their phase begins

### Consequences
- Higher initial setup cost than monolith (4 Cargo.toml files, growing to 6)
- Contributors must understand workspace structure
- Inter-crate API changes require coordinating across crate boundaries
- Full clean build will be slow (~3-5 min); incremental builds should be <30s
- Python-to-JS pivot may reduce LLM code generation quality (mitigated by Phase 0 spike)
- Fork-child model adds IPC overhead (~1-2ms per execution)

### Follow-ups
- Evaluate `boa` (Rust-native JS engine) as potential QuickJS replacement if `rquickjs` proves limiting
- Investigate WASM plugin system for post-v1 extensibility (replaces Option C's embedded scripting)
- Benchmark fork-child overhead -- if >5ms per sandbox invocation, consider process pooling
- Define `oochy-sdk` crate for third-party skill development once the skill interface stabilizes
- Evaluate local LLM support (Ollama integration) for v2 -- deferred from v1
- Evaluate SWC/oxc transpilation for TypeScript support in QuickJS (post-v1)
- Re-evaluate nono.sh once it matures (could replace direct Landlock/Seatbelt code)

---

## 10. Success Criteria (Testable)

| # | Criterion | Test |
|---|-----------|------|
| 1 | Single binary, zero deps | `./oochy` runs on fresh Linux/macOS with only an LLM API key set |
| 2 | End-to-end code generation | Telegram message -> LLM generates JS -> QuickJS executes -> result sent back |
| 3 | Filesystem isolation (kernel layer) | Child process attempts `open("/etc/passwd", O_RDONLY)` syscall -> blocked by Landlock/Seatbelt |
| 4 | Network isolation (kernel layer) | Child process attempts `socket(AF_INET, SOCK_STREAM, 0)` syscall -> blocked by Landlock/Seatbelt |
| 5 | Skill capability enforcement | Agent with `skills = ["Telegram.sendMessage"]` generates code calling `Discord.send()` -> capability denied |
| 6 | Skill rate limiting | Agent exceeds configured rate limit -> subsequent calls rejected with rate limit error |
| 7 | Type error retry loop | Deliberately broken JS -> LLM gets error feedback -> fixes code (max 3 retries) |
| 8 | State persistence | Kill `./oochy`, restart, conversation history intact in SQLite |
| 9 | Web dashboard | `localhost:3000` shows agents, conversations, generated code |
| 10 | Execution timeout | Infinite loop JS code killed after 30 seconds |
| 11 | Dual sandbox active | Both QuickJS memory limit AND kernel restrictions enforced simultaneously in fork-child process |
| 12 | Async skill roundtrip | JS `await Telegram.sendMessage(...)` -> host executes -> result available in ExecutionResult |

---

## 11. Open Questions

- [ ] **QuickJS async/await behavior in rquickjs** -- Phase 0 spike will resolve. If async doesn't work, synchronous skill descriptor model is the fallback.
- [ ] **Landlock minimum kernel version** -- Landlock v1 requires Linux 5.13+. Determine minimum supported kernel version and document it.
- [ ] **Seatbelt API stability on macOS** -- `sandbox_init` is technically a private API. Determine if Apple has deprecated it and what the alternative is.
- [ ] **License choice: MIT vs Apache 2.0** -- Affects community strategy. Decision needed before Phase 4.3 release.
- [ ] **Local LLM support timeline** -- Deferred to post-v1, but Ollama integration could be a quick win if the LLM provider trait is clean.
- [ ] **Connection pooling strategy for SQLite** -- `r2d2-sqlite` vs `deadpool-sqlite` vs simple `Arc<Mutex<>>`. Decision needed at Phase 3.1 when dashboard adds concurrent reads.
