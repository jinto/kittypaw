# Oochy v2: Competing with OpenClaw

**Date:** 2026-03-26
**Status:** DRAFT - Awaiting confirmation
**Mode:** RALPLAN-DR Consensus

---

## 1. Requirements Summary: What "Compete with OpenClaw" Means

OpenClaw's adoption is driven by five concrete things:
1. **Zero cost** -- free, MIT license, no cloud bills
2. **Self-hosted** -- `npx openclaw` or `docker compose up`, runs on your machine
3. **Multi-channel** -- WhatsApp, Telegram, Discord, iMessage, Mattermost out of the box
4. **Agent-native UX** -- tool use, sessions, memory, multi-agent routing, web dashboard
5. **Ecosystem** -- plugin architecture, NVIDIA integration, mobile nodes, 100K+ stars community

To compete, Oochy must match items 1-4 while differentiating on something OpenClaw cannot do: **LLM code generation + sandboxed execution**. OpenClaw routes messages to AI agents with tool-calling. Oochy's agent *writes and runs arbitrary code* -- a fundamentally more powerful paradigm.

### Concrete "compete" criteria:
- Developer can run Oochy locally with a single command, no cloud account required
- Free and open-source (MIT or similar)
- Supports at least: Telegram, Discord, WhatsApp, Web Chat
- Has a web control UI/dashboard
- Has plugin/skill architecture for extensibility
- Has session memory and multi-agent support
- Differentiator: code-generating agent with sandboxed execution is front-and-center

---

## 2. RALPLAN-DR Summary

### Principles (5)

1. **Local-first, cloud-optional** -- The default experience must work on a developer's laptop with zero cloud dependencies. Cloud features are additive, never required.
2. **Code execution is the moat** -- Oochy's unique value is that agents write and execute real code. Every architecture decision must protect and enhance this capability.
3. **One-command onboarding** -- If it takes more than `npx oochy` or `docker compose up` to get running, we lose. Developer experience is a feature.
4. **Channel parity, then channel superiority** -- Match OpenClaw's channel coverage first, then leverage code execution to do things in those channels that OpenClaw cannot.
5. **Extensibility over features** -- A plugin architecture that lets the community build is worth more than 10 built-in features.

### Decision Drivers (Top 3)

1. **Zero-cost self-hosting** -- OpenClaw is free. Any architecture requiring paid cloud services is a non-starter for the primary deployment mode.
2. **Sandbox security on local machines** -- Running LLM-generated code locally is dangerous. The sandbox solution must be robust without requiring cloud isolation (Lambda).
3. **Developer adoption speed** -- Time from `git clone` to "working agent responding on Telegram" must be under 5 minutes.

### Viable Options

#### Option A: Cloudflare Workers (Original Plan)

| Pros | Cons |
|------|------|
| Global edge deployment, low latency | $5/month minimum (Durable Objects on paid plan) |
| No server management | 30s CPU limit (50ms on free tier) -- mypy + code execution may exceed this |
| Built-in KV/D1/R2 for state | Cannot run subprocess isolation (no `child_process`, no Docker) |
| Scales to zero | Fundamentally cloud-dependent -- antithetical to competing with free self-hosted OpenClaw |

**Fit with competing against OpenClaw:** POOR. The entire value proposition of OpenClaw is "free, runs locally." A cloud-dependent, paid architecture is swimming upstream. Additionally, Workers cannot run subprocess-based sandboxing (mypy, subprocess isolation), which breaks Oochy's core differentiator. Would need to rewrite the sandbox entirely for V8 isolates, losing the type-checking loop.

**Verdict: INVALIDATED.** The CPU limits and lack of subprocess support make it technically unfit for Oochy's core loop (generate code -> mypy check -> subprocess execute). The cost model makes it strategically unfit for competing with a free product.

#### Option B: Local-First (Node.js/Bun + Container Sandbox)

| Pros | Cons |
|------|------|
| Zero cost, runs on developer's machine | Sandbox security harder without cloud isolation |
| Matches OpenClaw's deployment model exactly | Developer must provide their own LLM API key (cost shifted, not eliminated) |
| Full OS access: subprocess, Docker, filesystem | No built-in global distribution |
| TypeScript ecosystem aligns with plugin extensibility | Requires maintaining cross-platform compatibility (macOS/Linux/Windows) |

**Sandbox approach:** Use Docker containers or [nono.sh](https://github.com/nicholasgasior/nono) for sandboxed code execution. On machines with Docker, spin up ephemeral containers per execution. Fallback to subprocess isolation with seccomp/landlock on Linux, sandbox-exec on macOS.

**Fit with competing against OpenClaw:** STRONG. Same deployment model, same cost model, but with a fundamentally more powerful agent paradigm.

#### Option C: Hybrid (Local Core + Optional Cloud Edge)

| Pros | Cons |
|------|------|
| Local-first satisfies the free/self-hosted crowd | Two deployment targets to maintain |
| Cloud option enables SaaS offering later | More complex architecture, risk of "lowest common denominator" |
| Edge deployment via CF Workers for latency-sensitive use cases | Cloud path still has the sandbox limitations |
| Revenue path without alienating OSS community | Delays shipping -- building two targets at once |

**Fit with competing against OpenClaw:** GOOD for long-term, but risks splitting focus early. The cloud edge variant would need a different sandbox strategy (e.g., calling out to a remote sandbox service), adding complexity.

### Recommendation

**Option B (Local-First)** for the v2 rewrite, with Option C (Hybrid) as a Phase 3 future concern. Reasons:

1. Cloudflare Workers is technically invalidated (cannot run mypy + subprocess sandbox)
2. Local-first directly counters OpenClaw's positioning
3. The hybrid cloud layer can be added later as an optional deployment adapter without changing the core architecture
4. Shipping one thing well beats shipping two things poorly

---

## 3. Architecture Decision

### Runtime: Bun (TypeScript)

**Why Bun over Node.js:** Faster startup (~2x), built-in TypeScript support, built-in test runner, built-in SQLite (for local state), single binary distribution. Still runs Node.js packages.

**Why TypeScript over Python (current):** The plugin ecosystem, npm distribution (`npx oochy`), and frontend (dashboard) integration all favor TypeScript. Python sandbox (mypy) stays as a supported code generation target -- the agent generates Python/TS code that runs in a container.

### Core Architecture

```
oochy/
  packages/
    core/           # Agent loop, state machine, event bus
    sandbox/        # Code execution: Docker container or subprocess isolation
    channels/       # Channel adapters: telegram, discord, whatsapp, web, mqtt
    dashboard/      # Web UI (lightweight React or vanilla)
    cli/            # CLI entry point: `npx oochy`
    plugins/        # Plugin loader + built-in plugins
  docker/
    sandbox/        # Dockerfile for sandboxed code execution
    compose.yaml    # docker compose up for full stack
```

### Key Architecture Decisions

1. **Agent Loop (port from Python):** Event -> Load State (SQLite) -> Build Prompt -> LLM API -> Generate Code -> Type Check -> Execute in Sandbox -> Save State. Same flow as current `src/agent_loop/loop.py`, but state goes to local SQLite instead of S3.

2. **Sandbox Strategy (tiered):**
   - **Tier 1 (Docker, recommended):** Ephemeral container per execution. Mount generated code, capture stdout. Secure, cross-platform, predictable.
   - **Tier 2 (subprocess, fallback):** For users without Docker. subprocess with timeout + resource limits. Less secure but functional.
   - **Tier 3 (V8 isolate, future):** For TypeScript-only code generation. Fastest, most secure, but limits code to JS/TS.

3. **State:** Local SQLite via Bun's built-in `bun:sqlite`. Agent state, conversation history, session memory all in one DB file. No S3, no cloud. Portable -- just copy the `.oochy/` directory.

4. **Channel Adapters:** Each channel is a plugin that implements a `Channel` interface: `onMessage(msg) -> Event`, `sendMessage(agentId, response)`. Built-in: Telegram, Discord, WhatsApp (via Baileys), Web (WebSocket).

5. **Plugin System:** Plugins are npm packages or local directories. Manifest-based (`oochy-plugin.json`). Can add channels, skills (tools the agent can call), and middleware.

6. **Dashboard:** Lightweight web UI served by the Oochy process itself. Shows agents, conversations, logs, sandbox executions. React or Preact SPA.

---

## 4. Implementation Phases

### Phase 1: Core Engine (Weeks 1-3)
**Goal:** `npx oochy` starts a working agent that responds to web chat with code-generating capabilities.

**Tasks:**
1. **Initialize Bun/TypeScript monorepo** with packages: `core`, `sandbox`, `channels`, `cli`
   - *Acceptance:* `bun run build` succeeds, `bun test` runs, monorepo structure in place

2. **Port agent loop from Python to TypeScript** -- translate `src/agent_loop/loop.py` logic: event -> state -> prompt -> LLM -> code -> type-check -> execute -> save state
   - *Acceptance:* Given a hardcoded event, the agent loop calls Claude API, generates code, and returns a result. Unit tests pass.

3. **Implement sandbox (Docker-first)** -- ephemeral container execution with stdout capture, timeout, resource limits
   - *Acceptance:* `sandbox.execute("print('hello')")` returns `{success: true, result: "hello"}` via Docker container. Subprocess fallback works when Docker is unavailable.

4. **Implement SQLite state store** -- agent state, conversation history, session management using `bun:sqlite`
   - *Acceptance:* State persists across restarts. `oochy` can resume a conversation after being killed and restarted.

5. **Web chat channel + basic dashboard** -- WebSocket-based chat channel, minimal dashboard showing conversation
   - *Acceptance:* Open `localhost:3000`, type a message, get a code-generated response. See conversation history in dashboard.

6. **CLI entry point** -- `npx oochy` or `bunx oochy` starts everything (agent loop, web server, dashboard)
   - *Acceptance:* Fresh machine with Bun installed: `bunx oochy` -> agent is running and accessible at localhost:3000 within 30 seconds.

### Phase 2: Channel Parity (Weeks 4-6)
**Goal:** Match OpenClaw's channel coverage. Telegram, Discord, WhatsApp working.

**Tasks:**
1. **Channel adapter interface + plugin system** -- define `Channel` interface, plugin loader, manifest format
   - *Acceptance:* A third-party developer can create a channel plugin in a separate npm package and Oochy loads it.

2. **Telegram channel** -- port from current `src/skills/telegram.py`, webhook + polling modes
   - *Acceptance:* Send message to Telegram bot, get code-generated response. Media handling (images, voice) works.

3. **Discord channel** -- discord.js integration, slash commands, DM + server channels
   - *Acceptance:* Discord bot responds to messages with code-generated responses in both DMs and server channels.

4. **WhatsApp channel** -- via Baileys (no Business API costs) or similar library
   - *Acceptance:* WhatsApp message -> agent response. QR code pairing flow works from dashboard.

5. **Multi-agent routing** -- configure multiple agents with different system prompts, route messages to the right agent based on channel/rules
   - *Acceptance:* Two agents configured: one for Telegram (assistant), one for Discord (code helper). Messages route correctly.

### Phase 3: Differentiation (Weeks 7-10)
**Goal:** Features that OpenClaw cannot match -- leveraging code execution.

**Tasks:**
1. **Skill/tool system for generated code** -- typed skill stubs (like current `.pyi` files) that the generated code can call: HTTP requests, database queries, file operations, channel-specific actions
   - *Acceptance:* Agent generates code that calls `await telegram.sendPhoto(chatId, url)` and it works. Type checking catches invalid skill usage.

2. **Desktop control channel** -- port MQTT desktop skill, add local mode (direct subprocess on same machine)
   - *Acceptance:* "Open Safari and go to google.com" -> agent generates AppleScript -> executes locally.

3. **Workflow/automation engine** -- agents can create scheduled tasks, react to events from other agents, chain executions
   - *Acceptance:* "Every morning at 9am, check my calendar and send me a Telegram summary" -> cron-like schedule created, runs daily.

4. **Dashboard v2** -- code execution viewer (see what code the agent wrote, execution traces), agent configuration UI, plugin marketplace browser
   - *Acceptance:* Dashboard shows code diff for each agent turn, execution stdout/stderr, timing. Users can install plugins from UI.

### Phase 4: Community and Growth (Weeks 11+)
**Goal:** Open-source launch, community building, optional cloud tier.

**Tasks:**
1. **Open-source prep** -- MIT license, contributing guide, documentation site, example plugins, GitHub Actions CI
2. **Docker Compose distribution** -- `docker compose up` for users who want everything containerized
3. **Optional cloud adapter** -- for users who want hosted Oochy (Fly.io, Railway, or CF Workers for the non-sandbox parts)
4. **Mobile node** -- lightweight client for iOS/Android that connects to a running Oochy instance

---

## 5. Acceptance Criteria (Testable)

| Criteria | Test |
|----------|------|
| Zero-cost self-hosting | `bunx oochy` works with no cloud account, only an LLM API key |
| One-command start | Time from install to working agent < 2 minutes |
| Telegram channel | Send message in Telegram -> receive code-generated response |
| Discord channel | Send message in Discord -> receive code-generated response |
| WhatsApp channel | Send message in WhatsApp -> receive code-generated response |
| Web dashboard | Open localhost:3000 -> see agents, conversations, code executions |
| Code sandbox security | Sandboxed code cannot access host filesystem or network outside allowlist |
| Plugin extensibility | Third-party npm package loaded as channel plugin without modifying core |
| Multi-agent routing | Two agents, two channels, messages route correctly |
| State persistence | Kill and restart Oochy -> conversation history preserved |
| Code execution visibility | Dashboard shows generated code + execution result for each turn |

---

## 6. Risks and Mitigations

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| Sandbox escape on local machine | HIGH -- LLM-generated code accesses host | MEDIUM | Docker containers as default. Subprocess fallback uses OS-level sandboxing (seccomp/landlock/sandbox-exec). Security audit before v1. |
| Bun instability / missing Node.js compat | MEDIUM -- runtime bugs block development | LOW | Bun is mature enough for this. Fallback: the codebase should also work on Node.js with minimal changes (avoid Bun-only APIs except SQLite). |
| LLM API cost scares users | MEDIUM -- "free" but API key costs $$ | HIGH | Support local LLMs (Ollama, llama.cpp) as LLM provider. Code generation quality may degrade but basic functionality works. Document expected costs. |
| Python -> TypeScript rewrite scope creep | HIGH -- rewrite takes too long | MEDIUM | Phase 1 is intentionally minimal. Port the loop logic, not the entire codebase. Current Python is ~500 lines of real logic. |
| OpenClaw ships code execution feature | HIGH -- moat eliminated | LOW | Move fast. OpenClaw's architecture (tool-calling) is fundamentally different from code generation. Retrofitting code execution is hard. |
| Cross-platform Docker availability | MEDIUM -- Windows/older Mac users may not have Docker | MEDIUM | Tier 2 subprocess fallback. Clear docs on security tradeoffs. |

---

## 7. ADR: Architecture Decision Record

**Decision:** Rewrite Oochy as a local-first TypeScript/Bun application, abandoning the Cloudflare Workers plan.

**Drivers:**
1. Competing with OpenClaw requires zero-cost self-hosting
2. Oochy's core loop (code generation + mypy + subprocess execution) is incompatible with CF Workers' runtime constraints
3. Developer adoption speed favors `npx` distribution over cloud deployment

**Alternatives Considered:**
- **Cloudflare Workers:** Invalidated -- cannot run subprocess-based sandbox, $5/month minimum, fundamentally cloud-dependent
- **Hybrid (local + cloud):** Viable but deferred -- splitting focus across two deployment targets delays shipping. Cloud adapter planned for Phase 4.
- **Stay on Python/AWS:** Rejected -- AWS Lambda cold starts hurt UX, S3 state is expensive at scale, `pip install` distribution is worse than `npx`

**Why Local-First Bun/TypeScript:**
- Matches OpenClaw's deployment model (the table stakes)
- `bunx oochy` is the simplest possible onboarding
- Bun's built-in SQLite eliminates external database dependency
- TypeScript enables shared types between agent code generation, plugin system, and dashboard
- Docker-based sandbox is more secure than subprocess isolation and works cross-platform

**Consequences:**
- Full rewrite required (Python -> TypeScript), estimated 6-8 weeks for Phase 1-2
- Lose AWS CDK infrastructure (acceptable -- it was the thing we were replacing anyway)
- Must solve sandbox security without Lambda isolation
- Must support local LLMs to truly be "free" (API key costs remain)

**Follow-ups:**
- Evaluate nono.sh, gVisor, and Firecracker microVMs as sandbox alternatives to Docker
- Benchmark Bun SQLite performance for conversation state at scale (1000+ agents)
- Research Baileys library legal status for WhatsApp integration
- Define plugin API contract and publish as `@oochy/sdk` npm package

---

## Open Questions

See `/Users/jinto/projects/oochy/.omc/plans/open-questions.md`
