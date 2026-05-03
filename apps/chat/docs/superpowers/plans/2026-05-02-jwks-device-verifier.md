# JWKS Device Verifier Implementation Plan

> Historical plan snapshot. This document records an app-local implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and the app README/DEPLOY docs for the current live shape.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add RS256/JWKS verification for kittyapi-issued daemon device JWTs.

**Architecture:** A new JWKS-backed verifier in `internal/identity` verifies RS256 JWTs by `kid`, caches JWKS keys for 10 minutes, and maps device JWT claims into existing `DeviceClaims`. `cmd/kittychat` chains the JWKS verifier before the static fallback when `KITTYCHAT_JWKS_URL` is configured.

**Tech Stack:** Go, `github.com/golang-jwt/jwt/v5`, `net/http`, `crypto/rsa`, standard-library JWK parsing.

---

### Task 1: JWKS Verifier Tests

**Files:**
- Modify: `internal/identity/jwt_verifier_test.go`

- [ ] Write failing tests that create an RSA key, serve `/.well-known/jwks.json`, sign RS256 device tokens, and assert `VerifyDevice` accepts the valid shape.
- [ ] Add negative tests for bad audience, bad scope, unknown `kid`, unknown `kid` refetch backoff, JWKS timeout, empty JWKS stale-cache behavior, expired token, and HS256 algorithm.
- [ ] Run `go test ./internal/identity -count=1` and confirm the new tests fail because the verifier does not support JWKS/RS256 yet.

### Task 2: JWKS Verifier Implementation

**Files:**
- Modify: `internal/identity/jwt_verifier.go`
- Modify: `internal/identity/verifier.go`

- [ ] Add `JWKSURL`, `Audience`, `Issuer`, `HTTPClient`, `CacheTTL`, and `Leeway` fields to `JWTVerifierConfig`.
- [ ] Implement bounded JWKS fetch, RSA JWK parsing, key cache, stale-cache preservation on refresh failure, unknown-`kid` single refetch with one second backoff, and RS256-only key selection.
- [ ] Parse device IDs from `sub=device:<device_id>`.
- [ ] Accept v2 device JWTs without `device_id` or `local_accounts` claims.
- [ ] Run `go test ./internal/identity -count=1` and confirm it passes.

### Task 3: Server Config Wiring

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/kittychat/main.go`
- Modify: `cmd/kittychat/main_test.go`

- [ ] Add `KITTYCHAT_JWKS_URL` config.
- [ ] Treat JWKS URL as a production credential source, alongside static fallback.
- [ ] Chain JWKS verifier before static verifier when both are configured.
- [ ] Add a WebSocket daemon-connect test using an RS256 device JWT and a local JWKS server.
- [ ] Run `go test ./cmd/kittychat ./internal/config -count=1`.

### Task 4: Docs And Full Verification

**Files:**
- Modify: `README.md`
- Modify: `DEPLOY.md`
- Modify: `deploy/env.example`

- [ ] Document `KITTYCHAT_JWKS_URL`, RS256/JWKS, `v=2`, 10 minute JWKS cache, and static fallback.
- [ ] Run `gofmt -w` on changed Go files.
- [ ] Run `go test ./... -count=1`.
- [ ] Run `make smoke-local`.
