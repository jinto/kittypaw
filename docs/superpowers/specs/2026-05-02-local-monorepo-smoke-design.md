# Local Monorepo Smoke Design

> Historical plan snapshot. This document records the implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

Date: 2026-05-02

## Goal

Provide one local command that checks the monorepo after cross-service changes
without depending on production hosts or interactive OAuth.

## Design

The default local smoke is a deterministic orchestration script:

1. Validate shared contract JSON with `make contracts-check`.
2. Syntax-check deploy scripts that are easy to break during service splits.
3. Compile Fabric deploy files with `python3 -m py_compile`.
4. Run package tests for `apps/kittyapi`, `apps/portal`, `apps/chat`, and
   `apps/kittypaw`.
5. Run `cargo test` for `apps/kakao`.
6. Run the existing Chat in-process e2e smoke via `make -C apps/chat smoke-local`.

This keeps the default command repeatable on a developer laptop. It does not
start PostgreSQL or perform Google/GitHub OAuth. DB-backed integration tests
remain service-owned opt-in commands, and live endpoint smokes remain under each
service's `deploy/` directory.

## Command Shape

Add:

```bash
make smoke-local
```

The Make target calls:

```bash
scripts/smoke-local.sh
```

The script prints section headers, fails fast, and uses a local Go build cache
under `/private/tmp/kitty-go-build` unless `GOCACHE` is already set.

## Boundaries

- Root smoke coordinates services; service-specific live smoke scripts stay in
  `apps/<service>/deploy`.
- The script does not import private service packages.
- The script does not mutate databases.
- The script does not require network access except what individual package
  tests already require locally.

## Follow-Up

A later DB smoke can add an opt-in target that starts a disposable PostgreSQL
container and runs `-tags=integration` suites for `apps/portal` and
`apps/kittyapi` against the same local test database.
