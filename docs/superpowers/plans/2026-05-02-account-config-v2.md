# Account Config V2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current mixed account config with a clean v2 layout: per-account `account.toml`, non-secret `config.toml`, and per-account `secrets.json`.

**Architecture:** The user-visible TOML shape becomes the source of truth for non-secret account settings. Secrets stay in `secrets.json` and are injected into runtime channel/model configs before daemon startup. Local Web UI credentials move from root `auth.json` to `accounts/<id>/account.toml`; the account id remains the folder name.

**Tech Stack:** Go, BurntSushi/toml, existing `core.SecretsStore`, existing `store.Workspace` table, existing `engine.Session` allowed-path cache.

---

### Task 1: Config Shape

**Files:**
- Modify: `core/config.go`
- Test: `core/config_test.go`

- [x] Add `Config.Version`, replace persisted `is_family` with `is_shared`, add `workspace.default`, `workspace.roots`, `llm.default`, `llm.fallback`, and `llm.models`.
- [x] Keep runtime compatibility helpers so existing session code can ask for default/fallback models and shared-account state without reading deprecated fields directly.
- [x] Add tests that TOML with `is_shared`, `[[llm.models]]`, `[[channels]] type`, and `[[workspace.roots]]` loads into the new fields.

### Task 2: Secrets Injection

**Files:**
- Modify: `core/config.go`
- Modify: `server/spawner.go`
- Modify: `server/account_deps.go`
- Test: `core/config_test.go`
- Test: `server/account_deps_test.go` or existing account deps tests

- [x] Add helpers that hydrate model credentials from `secrets.json` namespaces such as `llm/openai`.
- [x] Add helpers that hydrate Telegram tokens from `channel/<id>.bot_token` and Kakao WS URL from `channel/<id>.ws_url`, with existing API-token relay lookup retained as a runtime source.
- [x] Seed workspaces from `workspace.roots` instead of `sandbox.allowed_paths`; runtime `Sandbox.AllowedPaths` becomes derived state.

### Task 3: Local Auth Per Account

**Files:**
- Modify: `core/local_auth.go`
- Test: `core/local_auth_test.go`

- [x] Change `LocalAuthStore` to scan and write `accounts/<id>/account.toml`.
- [x] Store only `password_hash`, `disabled`, `created_at`, and `updated_at` in `account.toml`.
- [x] Keep the public `CreateUser`, `VerifyPassword`, `HasUser`, `HasUsers`, `DeleteUser`, and `SetDisabled` API intact.

### Task 4: Setup Writes Clean Files

**Files:**
- Modify: `core/wizard.go`
- Modify: `cli/main.go`
- Modify: `server/api_setup.go`
- Modify: `server/api_settings.go`
- Test: `cli/cmd_setup_test.go`
- Test: `server/api_setup_account_test.go`
- Test: `server/api_settings_test.go`

- [x] Make setup write LLM API keys, Telegram bot tokens, Firecrawl/Tavily keys, and local server API key to `secrets.json`.
- [x] Make `config.toml` contain only non-secret references and settings.
- [x] Make Kakao config require only `[[channels]] id="kakao" type="kakao_talk"`; pairing stores its WS URL in `secrets.json`.

### Task 5: Verification

**Files:**
- Update tests touched by the new config shape.

- [x] Run targeted tests for `core`, `server`, and `cli`.
- [x] Run `go test ./... -count=1`.
- [x] Run `golangci-lint run`.
- [x] Run `git diff --check`.
