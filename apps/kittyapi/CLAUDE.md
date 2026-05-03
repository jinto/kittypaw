# KittyAPI

공공데이터 프록시 서비스. kittypaw 스킬 패키지의 zero-config 데이터 접근을
위한 `/v1/*` resource API를 제공한다.

인증, OAuth, users, refresh/device credentials, discovery, JWKS는
`apps/portal` 소유다. 이 앱은 portal split 이후 identity route를 제공하지
않으며 `/auth/*`, `/discovery`, `/.well-known/jwks.json`은 404가 맞다.

## Architecture

```
cmd/server/    Entry point (Chi router)
cmd/seed-*     Offline seed/import helpers
cmd/benchmark-resolve/
internal/
  config/      Env-based configuration
  proxy/       Data proxy handlers (AirKorea, KASI, KMA, geo)
  cache/       In-memory TTL cache
  ratelimit/   Anonymous/IP rate limiting for upstream protection
  model/       Resource DB models + queries (pgx, raw SQL)
migrations/    Resource-data SQL migration files
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
- Identity is deliberately outside this service. Use `apps/portal` for OAuth,
  discovery, JWKS, user tokens, and chat relay device credentials.
- Anonymous/IP rate limiting protects upstream public APIs. Add resource auth
  only through an explicit contract change.
- Source: private (not open source)
