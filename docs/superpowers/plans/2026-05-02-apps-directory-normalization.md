# Apps Directory Normalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move every deployable product under `apps/` using the approved names: `kittypaw`, `kittyapi`, `chat`, `kakao`, and future `portal`.

**Architecture:** `apps/` becomes the home for independently deployed or released products. `contracts/`, `testkit/`, `docs/`, `deploy/`, and `scripts/` remain repository-level support areas. This is a path normalization only: module import paths, public domains, runtime service names, databases, and deploy behavior stay unchanged unless a path reference must be corrected.

**Tech Stack:** Git path moves with `git mv`, Go workspace, Cargo manifest path for Kakao, root docs, deploy scripts, JSON contracts, bash syntax checks.

---

## File Structure

Final top-level product layout:

```text
apps/
  kittypaw/   # local CLI, daemon, local web UI, GitHub release artifact
  kittyapi/   # api.kittypaw.app hosted Kitty API surface
  chat/       # chat.kittypaw.app hosted chat UI + relay
  kakao/      # kakao.kittypaw.app Kakao webhook/bridge
  portal/     # future portal.kittypaw.app identity/bootstrap service
```

Moves:

```text
services/api   -> apps/kittyapi
services/chat  -> apps/chat
services/kakao -> apps/kakao
```

`apps/portal` is not created by this move. The existing portal extraction plan should be updated to create `apps/portal` instead of `services/portal`.

Do not rewrite historical import records under `docs/migration/` or the historical import plan except to add a short note if needed. Those files describe how the monorepo was imported, not the current tree.

## Task 1: Move Product Directories

**Files:**
- Move: `services/api` to `apps/kittyapi`
- Move: `services/chat` to `apps/chat`
- Move: `services/kakao` to `apps/kakao`
- Modify: `go.work`

- [ ] **Step 1: Verify the target layout is absent**

Run:

```bash
test ! -e apps/kittyapi
test ! -e apps/chat
test ! -e apps/kakao
test -d services/api
test -d services/chat
test -d services/kakao
```

Expected: all commands exit 0.

- [ ] **Step 2: Move directories with history-preserving path moves**

Run:

```bash
git mv services/api apps/kittyapi
git mv services/chat apps/chat
git mv services/kakao apps/kakao
```

Expected: `git status --short` shows renames from `services/*` to `apps/*`.

- [ ] **Step 3: Update Go workspace paths**

Run:

```bash
go work edit -dropuse=./services/api -dropuse=./services/chat
go work use ./apps/kittyapi ./apps/chat
```

Expected `go.work`:

```text
go 1.26.2

use (
	./apps/chat
	./apps/kittypaw
	./apps/kittyapi
)
```

- [ ] **Step 4: Verify path layout**

Run:

```bash
test -f apps/kittyapi/go.mod
test -f apps/chat/go.mod
test -f apps/kakao/Cargo.toml
test ! -e services/api
test ! -e services/chat
test ! -e services/kakao
```

Expected: all commands exit 0.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore(apps): move services under apps"
```

## Task 2: Update Current Repository Documentation

**Files:**
- Modify: `README.md`
- Modify: `ARCHITECTURE.md`
- Modify: `docs/ports.md`
- Modify: `docs/decisions/0001-monorepo-service-boundaries.md`
- Modify: `docs/superpowers/specs/2026-05-02-portal-auth-discovery-design.md`
- Modify: `docs/superpowers/plans/2026-05-02-services-portal-extraction.md`

- [ ] **Step 1: Find current-path references**

Run:

```bash
rg -n "services/(api|chat|kakao|portal)|services and apps|services/" README.md ARCHITECTURE.md docs/ports.md docs/decisions docs/superpowers/specs docs/superpowers/plans/2026-05-02-services-portal-extraction.md
```

Expected: matches show current docs that still describe `services/*`.

- [ ] **Step 2: Update the root shape**

Change `README.md` shape block to:

```text
apps/
  kittypaw/          Local CLI, daemon, and local web UI
  kittyapi/          api.kittypaw.app hosted Kitty API surface
  chat/              chat.kittypaw.app hosted chat UI and daemon relay
  kakao/             kakao.kittypaw.app Kakao webhook and bridge
  portal/            Future portal.kittypaw.app identity/bootstrap service
contracts/           Wire-level schemas, examples, and version policies
testkit/             Cross-app verification helpers and fakes
deploy/              Repository-level deployment notes and shared assets
docs/                Architecture notes, decisions, operations, plans
scripts/             Repository-level helper scripts
```

Also update the status paragraph so it no longer says the directory is a skeleton.

- [ ] **Step 3: Update architecture component names**

In `ARCHITECTURE.md`, replace component headers:

```text
services/api    -> apps/kittyapi
services/portal -> apps/portal
services/chat   -> apps/chat
services/kakao  -> apps/kakao
```

Update the dependency direction block to:

```text
apps
  -> contracts
  -> testkit
```

Update deployment bullets to use `apps/<name>`.

- [ ] **Step 4: Update current decision and portal plan**

In `docs/decisions/0001-monorepo-service-boundaries.md`, use "apps" instead of "services" for deployable units while keeping the rule that each app deploys separately.

In `docs/superpowers/specs/2026-05-02-portal-auth-discovery-design.md` and `docs/superpowers/plans/2026-05-02-services-portal-extraction.md`, change active target paths:

```text
services/api    -> apps/kittyapi
services/portal -> apps/portal
services/chat   -> apps/chat
```

Keep public domains unchanged:

```text
api.kittypaw.app
portal.kittypaw.app
chat.kittypaw.app
kakao.kittypaw.app
```

- [ ] **Step 5: Verify current docs no longer use services paths**

Run:

```bash
rg -n "services/(api|chat|kakao|portal)|services and apps" README.md ARCHITECTURE.md docs/ports.md docs/decisions docs/superpowers/specs docs/superpowers/plans/2026-05-02-services-portal-extraction.md
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add README.md ARCHITECTURE.md docs/ports.md docs/decisions docs/superpowers/specs docs/superpowers/plans/2026-05-02-services-portal-extraction.md
git commit -m "docs(apps): describe deployables under apps"
```

## Task 3: Update Commands, Deploy Notes, and Workflow Notes

**Files:**
- Modify: `.github/workflows/README.md`
- Modify: `deploy/README.md`
- Move or modify: `deploy/api/README.md`
- Modify: `deploy/chat/README.md`
- Modify: `deploy/kakao/README.md`
- Modify: repository-level docs that show test commands

- [ ] **Step 1: Find command references**

Run:

```bash
rg -n "services/(api|chat|kakao)|go test ./services|cargo test --manifest-path services" .github deploy docs README.md Makefile scripts
```

Expected: matches in docs and deploy notes.

- [ ] **Step 2: Rename root deploy placeholder for API**

If keeping root deploy placeholders, align them to app names:

```bash
git mv deploy/api deploy/kittyapi
```

Update `deploy/README.md` tree to:

```text
deploy/
  kittyapi/
  chat/
  kakao/
```

This does not move app-owned deploy scripts under `apps/kittyapi/deploy`, `apps/chat/deploy`, or `apps/kakao/deploy`; those moved with each app in Task 1.

- [ ] **Step 3: Update workflow notes**

Change `.github/workflows/README.md` expected workflow list to:

```text
- release-kittypaw.yml for kittypaw/v*
- release-kittyapi.yml for kittyapi/v*
- release-chat.yml for chat/v*
- release-kakao.yml for kakao/v*
```

Do not rewrite existing Git tags in this task.

- [ ] **Step 4: Update command examples**

Use these command replacements in current operational docs:

```text
go test ./services/api/...              -> go test ./apps/kittyapi/...
go test ./services/chat/...             -> go test ./apps/chat/...
cargo test --manifest-path services/kakao/Cargo.toml -> cargo test --manifest-path apps/kakao/Cargo.toml
services/api/deploy/smoke.sh            -> apps/kittyapi/deploy/smoke.sh
services/api/deploy/e2e_devices.sh      -> apps/kittyapi/deploy/e2e_devices.sh
```

- [ ] **Step 5: Verify non-historical references**

Run:

```bash
rg -n "services/(api|chat|kakao)|go test ./services|cargo test --manifest-path services" .github deploy README.md ARCHITECTURE.md docs/ports.md docs/decisions docs/superpowers/specs docs/superpowers/plans/2026-05-02-services-portal-extraction.md scripts Makefile
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore(apps): update repository path references"
```

## Task 4: Verify Build, Tests, and Script Syntax

**Files:**
- No intended source edits unless verification exposes a path regression.

- [ ] **Step 1: Run Go tests for moved apps**

Run:

```bash
go test ./apps/kittyapi/... -count=1
go test ./apps/chat/... -count=1
go test ./apps/kittypaw/... -count=1
```

Expected: all exit 0.

- [ ] **Step 2: Run Kakao Rust tests**

Run:

```bash
cargo test --manifest-path apps/kakao/Cargo.toml
```

Expected: exit 0.

- [ ] **Step 3: Run contract and script checks**

Run:

```bash
make contracts-check
bash -n apps/kittyapi/deploy/smoke.sh
bash -n apps/kittyapi/deploy/e2e_devices.sh
git diff --check
```

Expected: all exit 0.

- [ ] **Step 4: Inspect workspace status**

Run:

```bash
git status --short --branch
```

Expected: branch is ahead by the new commits and working tree is clean.

## Task 5: Optional Remote Push

**Files:**
- No file changes.

- [ ] **Step 1: Review commit stack**

Run:

```bash
git log --oneline --decorate -8
```

Expected: path normalization commits are on top of the existing portal canonical commits.

- [ ] **Step 2: Push when approved**

Run:

```bash
git push origin main
```

Expected: remote `main` updates successfully.

## Self-Review

- Spec coverage: approved final app names are covered exactly: `kittyapi`, `chat`, `kakao`, `kittypaw`, `portal`.
- Scope: this plan moves paths and references only. It does not extract portal, rename Go module paths, rewrite historical import records, deploy to `second`, or rewrite Git tags.
- Type consistency: public domains stay unchanged; folder names change only where filesystem paths are involved.
