# Chat Relay Auto Pair Implementation Plan

> Historical plan snapshot. This document records the implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pair chat relay device credentials automatically after successful API login.

**Architecture:** Add a small CLI helper that uses already-discovered `chat_relay_url` and `auth_base_url`, skips if paired, and treats pairing failures as warnings. Call it from `loginHTTP`, `loginCode`, and the setup API step's already-logged-in path.

**Tech Stack:** Go 1.25, `core.APITokenManager`, Cobra CLI/setup wizard tests.

---

## Task 1: Best-Effort Pair Helper

**Files:**
- Modify `cli/cmd_login.go`
- Modify `cli/cmd_login_test.go`

- [x] Add tests for successful auto-pair, already-paired skip, no-relay skip, and failure warning.
- [x] Implement `maybePairChatRelayDevice(apiURL, mgr, accessToken, out)`.
- [x] Run `go test ./cli -run 'TestMaybePairChatRelayDevice|TestApplyDiscovery' -count=1`.

## Task 2: Login And Setup Wiring

**Files:**
- Modify `cli/cmd_login.go`
- Modify `cli/init_wizard.go`
- Modify `cli/cmd_setup_test.go`

- [x] Call the helper after `loginHTTP` saves tokens.
- [x] Call the helper after `loginCode` saves tokens.
- [x] Call the helper in setup when an existing API login is already valid.
- [x] Run `go test ./cli -run 'TestWizardAPIServer|TestRunWizardUsesNamedAccountSecrets|TestMaybePairChatRelayDevice' -count=1`.

## Final Verification

- [x] `go test ./cli -count=1`
- [x] `go test ./...`
- [x] `make build`
- [x] `git diff --check`
