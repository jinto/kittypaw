# kittypaw-api v1

## Plan 1: Project Scaffolding ← 현재

- [x] **T1: Go module + health endpoint** — `go mod init`, `cmd/server/main.go` (chi + /health), `internal/config/config.go`, 테스트 통과
- [x] **T2: Makefile + .gitignore + .env.example** — build/test/lint/run 타겟
- [x] **T3: golangci-lint** — `.golangci.yml` v2, `make lint` 통과
- [x] **T4: lefthook** — `lefthook.yml` (pre-commit: fmt + lint, commit-msg: conventional commit)
- [x] **T6: GitHub Actions CI + CLAUDE.md** — `.github/workflows/ci.yml` (lint → test)
- [ ] **T7: 첫 커밋** — git init, 전체 파일 스테이징

## Plan 2: Auth

- [ ] DB schema + migrations
- [ ] OAuth flow (Google, GitHub, PKCE)
- [ ] JWT access token + refresh token rotation
- [ ] /auth/* endpoints
- [ ] Auth middleware

## Plan 3: Data Proxy

- [ ] Cache layer (in-memory TTL, stale-while-revalidate)
- [ ] Rate limiting middleware (anonymous + authenticated)
- [ ] /v1/air endpoint (에어코리아 프록시)
- [ ] Integration test (전체 flow)
