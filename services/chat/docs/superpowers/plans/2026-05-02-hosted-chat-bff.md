# Hosted Chat BFF Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hosted chat implicit-token receiver with Authorization Code + PKCE and a server-side BFF session.

**Architecture:** `kittychat` owns browser login/session UX. The browser starts `/auth/login/google`, API returns `code/state` to `/auth/callback`, `kittychat` exchanges server-to-server, stores tokens in memory, and proxies `/app/api/*` to the existing OpenAI-compatible handler with a server-injected bearer token.

**Tech Stack:** Go `net/http` + chi, existing `identity.CredentialVerifier`, in-memory session maps, vanilla HTML/JS, Go tests, Node test runner.

---

## Files

- Create `internal/webapp/store.go`: pending PKCE state store and opaque session store.
- Create `internal/webapp/handler.go`: login, callback, logout, session probe, BFF proxy, refresh.
- Create `internal/webapp/handler_test.go`: PKCE redirect, exchange cookie, proxy auth injection, refresh tests.
- Modify `cmd/kittychat/main.go`: wire webapp with existing verifier and OpenAI handler.
- Modify `internal/config/config.go`: add public chat URL and API auth base URL config.
- Modify `internal/server/router.go`: allow webapp to mount dynamic routes before static pages.
- Modify `internal/server/web/*.js` and `index.html`: remove token localStorage and call `/app/api/*`.
- Modify `docs/superpowers/specs/2026-05-02-hosted-chat-app-design.md`: update from implicit-token first slice to PKCE+BFF.

## Tasks

- [x] RED: `internal/webapp` tests fail because handler/session types do not exist.
- [x] GREEN: implement PKCE login redirect, callback exchange, HttpOnly session cookie, BFF route proxy, and refresh-before-proxy.
- [x] RED: config/router/main tests fail until webapp is mounted and defaults exist.
- [x] GREEN: add `KITTYCHAT_PUBLIC_BASE_URL`, `KITTYCHAT_API_AUTH_BASE_URL`, server `WebHandler`, and `cmd/kittychat` wiring.
- [x] RED: frontend tests fail while app still depends on browser bearer localStorage.
- [x] GREEN: remove browser credential helpers, enable `/auth/login/google`, and route app requests through `/app/api/*`.
- [x] VERIFY: `gofmt`, `go test ./...`, Node web tests, `make build`, `make lint`, `make smoke-local`.
- [ ] DEPLOY: commit, push, deploy, and prod-smoke `/`, `/auth/login/google` redirect, `/app/`, `/manual/`.

## Notes

- `kittychat_session` carries only an opaque session ID; access/refresh tokens are process-memory only.
- The first implementation is single-instance. A durable/distributed session store is a future scaling task.
- `/auth/token/refresh` is reused. No chat-specific refresh endpoint and no refresh-token audience column are added on the chat side.
