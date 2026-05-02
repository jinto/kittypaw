# kittypaw-api

공공데이터 프록시 + OAuth 인증 서버. kittypaw 스킬 패키지의 zero-config 데이터 접근을 위한 중개 서비스.

## Architecture

```
cmd/server/    Entry point (Chi router)
internal/
  config/      Env-based configuration
  auth/        OAuth handlers (Google, GitHub), JWT, middleware
  proxy/       Data proxy handlers (에어코리아 등)
  cache/       In-memory TTL cache
  ratelimit/   Per-IP / per-user rate limiting
  model/       DB models + queries (pgx, raw SQL)
migrations/    SQL migration files
```

## Commands

```bash
make build     # Build binary
make test      # Run all tests
make lint      # Run golangci-lint
make fmt       # Format code (gofmt + goimports)
make run       # Build and run
```

## Conventions

- **Commits**: conventional commits — `feat(scope): 설명`, `fix(scope): 설명`
- **Tests**: `_test.go` suffix, integration tests use `//go:build integration` build tag
- **Lint**: golangci-lint v2, config in `.golangci.yml`
- **Pre-commit**: lefthook — format + lint on pre-commit, conventional commit check on commit-msg

## Key Decisions

- Chi router (same as kittypaw)
- pgx/v5 for PostgreSQL (no ORM, raw SQL)
- golang-migrate for migrations
- OAuth only (Google + GitHub, PKCE) — no email/password
- Anonymous tier (IP-based 5 req/min) + Authenticated tier (per-user 60 req/min)
- JWT access token (15min) + opaque refresh token (7day, rotation)
- Source: private (not open source)
