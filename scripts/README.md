# Scripts

Repository-level helper scripts live here.

Service-specific scripts should stay inside their service directory unless they
coordinate multiple services.

## `smoke-local.sh`

Runs the repeatable local cross-service smoke used after monorepo contract or
boundary changes:

```bash
make smoke-local
```

It validates contracts, checks deploy script syntax, runs Go/Rust package tests,
and runs the Chat in-process e2e smoke. It intentionally does not run production
endpoint smoke scripts or DB-backed integration tests.

## `e2e-local.sh`

Runs the Docker-backed local auth/chat E2E:

```bash
make e2e-local
```

It starts a disposable PostgreSQL container, migrates Portal's schema from the
Go harness, starts real Portal and Chat binaries, uses a fake Google OAuth
server, connects a Kittypaw chat relay connector, and verifies the Chat BFF
session can reach `/app/api/*` without a browser `Authorization` header.

Set `KITTY_E2E_KEEP_DB=1` to keep the database container after the run. Set
`KITTY_E2E_SKIP_COMPOSE=1` and provide `DATABASE_URL` to use an already-running
test database.
