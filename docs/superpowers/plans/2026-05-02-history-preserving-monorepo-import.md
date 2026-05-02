# History-Preserving Monorepo Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Import the existing `kittypaw`, `kittyapi`, `kittychat`, and `kittykakao` repositories into `kitty/` with their commit history preserved under monorepo paths.

**Architecture:** Initialize `kitty/` as the new monorepo, commit the skeleton, then import each source repository through a temporary local clone rewritten with `git-filter-repo --to-subdirectory-filter`. This preserves path-level history under `apps/kittypaw`, `services/api`, `services/chat`, and `services/kakao` without mutating the original repositories.

**Tech Stack:** Git, git-filter-repo, Go multi-module via future `go.work`, Rust/Cargo for Kakao, GitHub Actions namespaced release tags.

---

## Current Inputs

Source repositories:

| Source path | Current branch | Remote | Target prefix |
| --- | --- | --- | --- |
| `../kittypaw` | `main` | `git@github.com:kittypaw-app/kittypaw.git` | `apps/kittypaw` |
| `../kittyapi` | `main` | `git@github.com:kittypaw-app/kittyapi.git` | `services/api` |
| `../kittychat` | `main` | `https://github.com/kittypaw-app/kittychat.git` | `services/chat` |
| `../kittykakao` | `main` | `git@github.com:kittypaw-app/kittykakao.git` | `services/kakao` |

Tooling preflight result:

- `git-filter-repo` is installed at `/opt/homebrew/bin/git-filter-repo`.
- Only `kittypaw` currently has release tags. Existing `v*` tags must become `kittypaw/v*` inside the monorepo.
- No git submodules are present in these four source repos.
- `../kittykakao` currently has uncommitted changes. User decision for this run:
  import committed `main` history only and leave uncommitted changes in the
  original repository.
- Current committed import heads observed before writing this plan:
  - `kittypaw`: `084491b fix(chat-relay): hide device credential controls`
  - `kittyapi`: `c777190 test(deploy): E2E device contract verifier (Plan 23 PR-D follow-up)`
  - `kittychat`: `e00bcb5 fix: harden hosted chat app`
  - `kittykakao`: `4fa65ff fix: send Kakao image callbacks`

## Import Policy

- Use local committed `main` HEADs as import sources.
- Do not import uncommitted working tree changes. Dirty source worktrees are
  acceptable only when the user explicitly decides to import committed history
  only.
- Do not mutate the original repositories.
- Do not use plain `v*` release tags in the monorepo.
- Import one repository per merge commit so rollback and review stay simple.
- If a merge conflict appears, stop and inspect. Do not resolve by overwriting blindly.

## File Structure

Created or modified by this migration:

- Modify: `kitty/.git/` by initializing a new git repository.
- Modify: `kitty/docs/migration/placeholders/*.md` by preserving skeleton placeholder README files before importing real repo roots.
- Import: `kitty/apps/kittypaw/**` from `../kittypaw`.
- Import: `kitty/services/api/**` from `../kittyapi`.
- Import: `kitty/services/chat/**` from `../kittychat`.
- Import: `kitty/services/kakao/**` from `../kittykakao`.
- Create: `kitty/go.work` after Go services are imported.
- Modify: `kitty/TASKS.md` to mark import progress.

Temporary working clones:

- `kitty/tmp/import/kittypaw`
- `kitty/tmp/import/kittyapi`
- `kitty/tmp/import/kittychat`
- `kitty/tmp/import/kittykakao`

`kitty/tmp/` is ignored by `kitty/.gitignore`.

---

### Task 1: Initialize the Monorepo Git Repository

**Files:**
- Modify: `kitty/.git/`
- Track: existing `kitty/**` skeleton files

- [ ] **Step 1: Enter the skeleton directory**

Run:

```bash
cd /Users/jinto/projects/kittypaw/kitty
```

Expected: shell is now inside `/Users/jinto/projects/kittypaw/kitty`.

- [ ] **Step 2: Initialize git with `main`**

Run:

```bash
git init -b main
```

Expected: output includes `Initialized empty Git repository`.

- [ ] **Step 3: Commit the skeleton**

Run:

```bash
git add .
git commit -m "chore: bootstrap kitty monorepo skeleton"
```

Expected: commit succeeds and includes the skeleton docs, contracts, and directory README files.

- [ ] **Step 4: Verify clean status**

Run:

```bash
git status --short
```

Expected: no output.

---

### Task 2: Block on Dirty Source Repositories

**Files:**
- Read-only: `../kittypaw`
- Read-only: `../kittyapi`
- Read-only: `../kittychat`
- Read-only: `../kittykakao`

- [ ] **Step 1: Check source repository status**

Run from `kitty/`:

```bash
git -C ../kittypaw status --short
git -C ../kittyapi status --short
git -C ../kittychat status --short
git -C ../kittykakao status --short
```

Expected:

- `../kittypaw`: no output
- `../kittyapi`: no output
- `../kittychat`: no output
- `../kittykakao`: may show uncommitted changes that are intentionally excluded
  from this import.

- [ ] **Step 2: Stop if any source is dirty**

If any repo other than `../kittykakao` prints changes, stop the import. For
`../kittykakao`, continue with committed `main` only unless the user changes
that decision.

- [ ] **Step 3: Confirm import heads**

Run:

```bash
git -C ../kittypaw log -1 --oneline
git -C ../kittyapi log -1 --oneline
git -C ../kittychat log -1 --oneline
git -C ../kittykakao log -1 --oneline
```

Expected: four commit lines. Record these in the PR or final migration note.

---

### Task 3: Move Placeholder README Files Out of Import Paths

**Files:**
- Move: `apps/kittypaw/README.md`
- Move: `services/api/README.md`
- Move: `services/chat/README.md`
- Move: `services/kakao/README.md`
- Create: `docs/migration/placeholders/apps-kittypaw.md`
- Create: `docs/migration/placeholders/services-api.md`
- Create: `docs/migration/placeholders/services-chat.md`
- Create: `docs/migration/placeholders/services-kakao.md`

- [ ] **Step 1: Create placeholder archive directory**

Run:

```bash
mkdir -p docs/migration/placeholders
```

Expected: directory exists.

- [ ] **Step 2: Move placeholder files**

Run:

```bash
git mv apps/kittypaw/README.md docs/migration/placeholders/apps-kittypaw.md
git mv services/api/README.md docs/migration/placeholders/services-api.md
git mv services/chat/README.md docs/migration/placeholders/services-chat.md
git mv services/kakao/README.md docs/migration/placeholders/services-kakao.md
```

Expected: `git status --short` shows four renames.

- [ ] **Step 3: Commit placeholder move**

Run:

```bash
git commit -m "chore: move import placeholders out of service paths"
```

Expected: commit succeeds.

---

### Task 4: Import `kittypaw` History Into `apps/kittypaw`

**Files:**
- Import: `apps/kittypaw/**`
- Tags: `kittypaw/v*`

- [ ] **Step 1: Create a temporary clone**

Run:

```bash
mkdir -p tmp/import
git clone --no-local ../kittypaw tmp/import/kittypaw
```

Expected: `tmp/import/kittypaw` is a standalone clone.

- [ ] **Step 2: Rewrite the clone into the target prefix**

Run:

```bash
cd tmp/import/kittypaw
git filter-repo --to-subdirectory-filter apps/kittypaw --tag-rename '':'kittypaw/' --force
cd ../../..
```

Expected:

- files in the temporary clone now live under `apps/kittypaw/`
- tags that were `v0.3.0` are now `kittypaw/v0.3.0`

- [ ] **Step 3: Merge rewritten history**

Run:

```bash
git remote add import-kittypaw tmp/import/kittypaw
git fetch import-kittypaw --tags
git merge --allow-unrelated-histories --no-ff import-kittypaw/main -m "chore: import kittypaw history"
git remote remove import-kittypaw
```

Expected: merge succeeds and `apps/kittypaw/go.mod` exists.

- [ ] **Step 4: Verify path history and tags**

Run:

```bash
git log --oneline -- apps/kittypaw/README.md | head
git tag --list 'kittypaw/v*' | tail
```

Expected:

- log shows more than the import merge commit
- tag list includes the former Kittypaw release tags with `kittypaw/` prefix

---

### Task 5: Import `kittyapi` History Into `services/api`

**Files:**
- Import: `services/api/**`
- Tags: `api/*` if source tags ever exist

- [ ] **Step 1: Create a temporary clone**

Run:

```bash
git clone --no-local ../kittyapi tmp/import/kittyapi
```

Expected: `tmp/import/kittyapi` is a standalone clone.

- [ ] **Step 2: Rewrite the clone into the target prefix**

Run:

```bash
cd tmp/import/kittyapi
git filter-repo --to-subdirectory-filter services/api --tag-rename '':'api/' --force
cd ../../..
```

Expected: files in the temporary clone now live under `services/api/`.

- [ ] **Step 3: Merge rewritten history**

Run:

```bash
git remote add import-api tmp/import/kittyapi
git fetch import-api --tags
git merge --allow-unrelated-histories --no-ff import-api/main -m "chore: import api service history"
git remote remove import-api
```

Expected: merge succeeds and `services/api/go.mod` exists.

- [ ] **Step 4: Verify path history**

Run:

```bash
git log --oneline -- services/api/README.md | head
```

Expected: log shows more than the import merge commit.

---

### Task 6: Import `kittychat` History Into `services/chat`

**Files:**
- Import: `services/chat/**`
- Tags: `chat/*` if source tags ever exist

- [ ] **Step 1: Create a temporary clone**

Run:

```bash
git clone --no-local ../kittychat tmp/import/kittychat
```

Expected: `tmp/import/kittychat` is a standalone clone.

- [ ] **Step 2: Rewrite the clone into the target prefix**

Run:

```bash
cd tmp/import/kittychat
git filter-repo --to-subdirectory-filter services/chat --tag-rename '':'chat/' --force
cd ../../..
```

Expected: files in the temporary clone now live under `services/chat/`.

- [ ] **Step 3: Merge rewritten history**

Run:

```bash
git remote add import-chat tmp/import/kittychat
git fetch import-chat --tags
git merge --allow-unrelated-histories --no-ff import-chat/main -m "chore: import chat service history"
git remote remove import-chat
```

Expected: merge succeeds and `services/chat/go.mod` exists.

- [ ] **Step 4: Verify path history**

Run:

```bash
git log --oneline -- services/chat/README.md | head
```

Expected: log shows more than the import merge commit.

---

### Task 7: Import `kittykakao` History Into `services/kakao`

**Files:**
- Import: `services/kakao/**`
- Tags: `kakao/*` if source tags ever exist

- [ ] **Step 1: Re-check `kittykakao` status**

Run:

```bash
git -C ../kittykakao status --short
```

Expected: the command may print uncommitted changes. They remain in the original
repo and are not included in the temporary clone import.

- [ ] **Step 2: Create a temporary clone**

Run:

```bash
git clone --no-local ../kittykakao tmp/import/kittykakao
```

Expected: `tmp/import/kittykakao` is a standalone clone.

- [ ] **Step 3: Rewrite the clone into the target prefix**

Run:

```bash
cd tmp/import/kittykakao
git filter-repo --to-subdirectory-filter services/kakao --tag-rename '':'kakao/' --force
cd ../../..
```

Expected: files in the temporary clone now live under `services/kakao/`.

- [ ] **Step 4: Merge rewritten history**

Run:

```bash
git remote add import-kakao tmp/import/kittykakao
git fetch import-kakao --tags
git merge --allow-unrelated-histories --no-ff import-kakao/main -m "chore: import kakao service history"
git remote remove import-kakao
```

Expected: merge succeeds and `services/kakao/Cargo.toml` exists.

- [ ] **Step 5: Verify path history**

Run:

```bash
git log --oneline -- services/kakao/README.md | head
```

Expected: log shows more than the import merge commit.

---

### Task 8: Add Go Workspace

**Files:**
- Create: `go.work`

- [ ] **Step 1: Initialize Go workspace**

Run:

```bash
go work init ./apps/kittypaw ./services/api ./services/chat
```

Expected: `go.work` is created with three modules.

- [ ] **Step 2: Sync workspace**

Run:

```bash
go work sync
```

Expected: command exits 0.

- [ ] **Step 3: Commit workspace file**

Run:

```bash
git add go.work
git commit -m "chore: add go workspace"
```

Expected: commit succeeds.

---

### Task 9: Run Import Verification

**Files:**
- Read-only verification across imported paths

- [ ] **Step 1: Verify repository status**

Run:

```bash
git status --short
```

Expected: no output.

- [ ] **Step 2: Verify imported module roots**

Run:

```bash
test -f apps/kittypaw/go.mod
test -f services/api/go.mod
test -f services/chat/go.mod
test -f services/kakao/Cargo.toml
```

Expected: all commands exit 0.

- [ ] **Step 3: Verify JSON contracts still parse**

Run:

```bash
make contracts-check
```

Expected: command exits 0.

- [ ] **Step 4: Run focused build/test smoke**

Run:

```bash
go test ./services/chat/... -count=1
go test ./services/api/... -short -count=1
cargo test --manifest-path services/kakao/Cargo.toml
```

Expected: all commands exit 0 or reveal path-assumption issues to fix in a
follow-up migration commit before continuing.

Do not run full `apps/kittypaw` tests yet unless the official skills checkout is
available under the expected relative path. That test suite has cross-repo
fixtures that should be handled in the next CI migration plan.

---

### Task 10: Record Import State

**Files:**
- Modify: `TASKS.md`
- Create: `docs/migration/import-state.md`

- [ ] **Step 1: Write import state document**

Create `docs/migration/import-state.md` with this shape:

```markdown
# Import State

Date: 2026-05-02

## Imported Sources

| Source | Imported HEAD | Target |
| --- | --- | --- |
| kittypaw | 084491b fix(chat-relay): hide device credential controls | apps/kittypaw |
| kittyapi | c777190 test(deploy): E2E device contract verifier (Plan 23 PR-D follow-up) | services/api |
| kittychat | e00bcb5 fix: harden hosted chat app | services/chat |
| kittykakao | 4fa65ff fix: send Kakao image callbacks | services/kakao |

## Tag Mapping

- `kittypaw/v*` tags were created from original Kittypaw `v*` tags.
- API, Chat, and Kakao had no source tags at import time.

## Verification

- `make contracts-check`
- `go test ./services/chat/... -count=1`
- `go test ./services/api/... -short -count=1`
- `cargo test --manifest-path services/kakao/Cargo.toml`
```

If Task 2 records different import heads because a source repository changed
before execution, write the Task 2 values instead of the values shown above.

- [ ] **Step 2: Update task list**

Edit `TASKS.md`:

```markdown
- [x] Decide whether existing repositories will be imported with history or as snapshots.
- [x] Import existing repositories with history.
```

- [ ] **Step 3: Commit import notes**

Run:

```bash
git add TASKS.md docs/migration/import-state.md
git commit -m "docs: record monorepo import state"
```

Expected: commit succeeds.

---

## Follow-Up Plans After Import

Do these after the history import is stable:

1. Root CI with path filters.
2. `apps/kittypaw` namespaced release workflow for `kittypaw/v*`.
3. Install script update to select the latest `kittypaw/v*` release.
4. Contract tests that validate `contracts/` examples against producer and consumer code.
5. Decision on whether old repositories become archived, read-only mirrors, or temporary bidirectional sync sources.

## Rollback Strategy

Every import is a merge commit. If an import goes wrong immediately after a
merge and before any later commits, inspect `HEAD` and revert that merge only:

```bash
git log --merges --oneline -1
git revert -m 1 HEAD
```

If the monorepo is still local and no useful work should be kept, the safer
option is to stop and ask before deleting or recreating `kitty/.git`.
