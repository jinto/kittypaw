# Account Add Kakao Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `kittypaw account add` create Telegram, KakaoTalk, or combined-channel personal accounts without duplicating the existing setup wizard's Kakao relay logic.

**Architecture:** Keep account filesystem/config creation in `core.InitAccount`, and move Kakao relay registration/pairing into a small CLI helper shared by `kittypaw setup` and `kittypaw account add`. Store the Kakao relay WS URL through `core.WizardResult`/`SaveWizardSecretsTo` so setup and account creation use the same secret format.

**Tech Stack:** Go CLI (`cobra`), core config/secrets, Kakao relay discovery APIs, existing `go test` suites.

---

### Task 1: Extend Core Account Creation

**Files:**
- Modify: `core/wizard.go`
- Modify: `core/account_setup.go`
- Test: `core/account_setup_test.go`

- [ ] **Step 1: Write failing tests**

Add tests proving `InitAccount` can create a Kakao channel, saves `channel/kakao.ws_url`, saves the default API URL, and rejects duplicate Kakao WS URLs before creating a second account.

- [ ] **Step 2: Verify RED**

Run:

```bash
go test ./core -run 'TestInitAccount_Kakao|TestInitAccount_DuplicateKakao'
```

Expected: fail because `AccountOpts` has no Kakao fields and no secrets are written.

- [ ] **Step 3: Implement core support**

Add `KakaoEnabled`, `KakaoRelayWSURL`, and `APIServerURL` to `AccountOpts`/`WizardResult`. Teach `MergeWizardSettings` to add the existing `kakao_talk` channel when enabled, and teach `SaveWizardSecretsTo` to write both host-scoped and `channel/kakao` WS URL secrets.

- [ ] **Step 4: Verify GREEN**

Run the same focused core test and then `go test ./core`.

### Task 2: Share Kakao Pairing Logic

**Files:**
- Create: `cli/kakao_pairing.go`
- Modify: `cli/init_wizard.go`
- Test: `cli/cmd_setup_test.go`

- [ ] **Step 1: Write failing tests**

Update the existing setup Kakao tests to expect `WizardResult.KakaoRelayWSURL` instead of direct early secret writes. Add a helper-level test with stubbed discovery/registration functions.

- [ ] **Step 2: Verify RED**

Run:

```bash
go test ./cli -run 'TestWizardKakao|TestPrepareKakao'
```

Expected: fail because the helper/result field does not exist yet.

- [ ] **Step 3: Implement helper**

Create a `prepareKakaoPairing` helper that fetches discovery, registers a relay session, builds `core.WSURLFromRelay`, and returns `core.WizardResult` fields plus pairing display data. Keep browser/clipboard/pair-wait in one reusable presenter.

- [ ] **Step 4: Verify GREEN**

Run the focused CLI tests and `go test ./cli`.

### Task 3: Add Account-Add Channel Selection

**Files:**
- Modify: `cli/account_wizard.go`
- Modify: `cli/cmd_account.go`
- Test: `cli/account_wizard_test.go`
- Test: `cli/cmd_account_test.go`

- [ ] **Step 1: Write failing tests**

Add wizard tests for Kakao-only and both-channel paths. Add command tests proving Kakao-only personal accounts are allowed and no-token/no-Kakao personal accounts are still rejected.

- [ ] **Step 2: Verify RED**

Run:

```bash
go test ./cli -run 'TestPromptAccountSetup.*Kakao|TestRunAccountAdd.*Kakao|TestRunAccountAdd_NoTokenRejected'
```

Expected: fail because `accountAddFlags` does not carry Kakao state and `runAccountAdd` still requires Telegram.

- [ ] **Step 3: Implement CLI flow**

Add channel choice to the account wizard, set `kakaoEnabled`/`kakaoRelayWSURL` from the shared helper, and relax the personal-account validation to require at least one channel: Telegram token or Kakao enabled.

- [ ] **Step 4: Verify GREEN**

Run focused CLI tests and `go test ./cli`.

### Task 4: Release Readiness

**Files:**
- Review: `.github/workflows/release.yml`
- Review: `.goreleaser.yml`
- Review: `install-kittypaw.sh`

- [ ] **Step 1: Full verification**

Run:

```bash
go test ./...
```

- [ ] **Step 2: Check release trigger**

Confirm the namespaced `kittypaw/v0.4.5` release path still produces GitHub release assets that `install-kittypaw.sh` discovers.

- [ ] **Step 3: Commit and tag only after tests pass**

Commit the feature, then tag `kittypaw/v0.4.5` if the worktree is clean and the user confirms release push timing.

---

Self-review: Scope is limited to account creation and shared Kakao pairing. No cross-app contract or DB changes are required.
