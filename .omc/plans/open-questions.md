# Open Questions

## oochy-v2-rust-rewrite - 2026-03-26 (v2 revision)

- [ ] **QuickJS async/await behavior in rquickjs** -- Phase 0 spike will resolve. If async doesn't work, the entire skill interception model needs a synchronous fallback (skill descriptors instead of await). Architecture-critical.
- [ ] **Landlock minimum kernel version** -- Landlock v1 requires Linux 5.13+. Need to determine and document minimum supported kernel version. Affects which Linux distros/versions are supported.
- [ ] **Seatbelt API stability on macOS** -- `sandbox_init` is technically a private API. Apple may have deprecated it. Need to verify current status and identify alternatives if deprecated. Affects macOS sandbox layer.
- [ ] **License choice: MIT vs Apache 2.0** -- Affects community strategy and contribution model. Decision needed before Phase 4.3 release.
- [ ] **Local LLM support timeline** -- Deferred to post-v1, but Ollama integration could be a quick win if the LLM provider trait is clean. Worth revisiting after Phase 1.
- [ ] **SQLite connection pooling strategy** -- `r2d2-sqlite` vs `deadpool-sqlite` vs simple `Arc<Mutex<>>`. Decision needed at Phase 3.1 when dashboard adds concurrent reads alongside agent writes.

## oochy-v2-compete-openclaw - 2026-03-26 (superseded by rust-rewrite)

- [ ] **License choice: MIT vs Apache 2.0 vs AGPL?** -- MIT matches OpenClaw and maximizes adoption, but AGPL could protect against proprietary forks. This affects community growth strategy.
- [ ] **Local LLM support priority: Phase 1 or Phase 2?** -- Supporting Ollama/llama.cpp makes Oochy truly free (no API key cost), but code generation quality with small local models may be poor. Shipping a degraded experience early could hurt perception.
- [x] **Python code generation vs TypeScript-only?** -- RESOLVED: JavaScript (ES2020) via QuickJS. See ADR: Python-to-JavaScript Code Generation Pivot in rust-rewrite plan.
- [ ] **WhatsApp integration legal risk** -- Baileys is a reverse-engineered WhatsApp Web client. Meta could block or send cease-and-desist. Deferred to post-v1 in Rust rewrite.
- [x] **Monorepo tool: Bun workspaces vs Turborepo?** -- RESOLVED: N/A. Rust workspace (Cargo) replaces Bun/Turborepo.
- [x] **Sandbox: Docker required or optional?** -- RESOLVED: Neither. Dual sandbox uses QuickJS VM + Landlock/Seatbelt kernel restrictions. No Docker dependency.
