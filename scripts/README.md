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
