# JWKS Device Verifier Design

Date: 2026-05-02

## Goal

KittyChat accepts daemon device credentials issued by kittyapi as RS256 JWTs verified through a JWKS endpoint. This removes the shared HS256 secret dependency from the daemon connection path while preserving static-token fallback for local smoke and staged rollout.

## Contract

Device JWTs use:

- `alg=RS256` with a `kid` header.
- `iss=https://api.kittypaw.app/auth`
- `aud=https://chat.kittypaw.app`
- `scope=["daemon:connect"]`
- `v=2`
- `sub=device:<device_id>`
- `user_id=<user_id>`

`device_id` is parsed from `sub`. `local_accounts` is not a JWT claim; the daemon hello frame supplies the current local accounts. If static-token credentials include a configured account allow-list, the hello frame remains constrained by that allow-list. RS256 device JWTs do not carry an account allow-list, so their hello accounts are scoped by `user_id` and `device_id`.

## Architecture

Add a JWKS-backed credential verifier in `internal/identity`. It fetches a JWK set from a configured URL with a bounded timeout, caches keys for 10 minutes, selects public keys by `kid`, and refetches once when a token presents an unknown `kid`. Repeated unknown `kid` misses are backed off for one second to avoid refetch loops. Network I/O happens outside the verifier mutex so stale cached keys can still authenticate while a refresh is in flight. Failed refreshes and JWKS responses with no usable RS256 keys do not replace the previous cache. The verifier enforces RS256, issuer, audience, 60 second clock skew, version 2, device subject shape, `user_id`, and `daemon:connect`.

The existing in-memory static verifier remains unchanged for static API/device tokens. Server wiring chains the JWKS verifier before the static verifier when `KITTYCHAT_JWKS_URL` is configured.

## Errors

All invalid token, missing key, bad audience, wrong scope, wrong version, and malformed subject cases return `identity.ErrUnauthorized`. JWKS fetch or decode failures also authenticate as unauthorized so `/daemon/connect` and OpenAI routes keep their existing 401 behavior.

## Testing

Tests generate an RSA key at runtime, serve a local JWKS endpoint, and sign JWTs with the fixture key. Tests cover valid device credentials, unknown `kid` refetch, unknown `kid` backoff, JWKS timeout, stale-cache behavior on refresh failure, bad audience, bad scope, wrong algorithm, expired token, malformed subject, and daemon WebSocket acceptance.
