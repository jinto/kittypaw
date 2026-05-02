# Hosted Chat App Design

Date: 2026-05-02

## Goal

Make `https://chat.kittypaw.app` the user-facing chat surface instead of a manual QA page. A logged-in user should land in a hosted chat app that uses the same relay path already verified by `/manual/`, but through a BFF session: HttpOnly chat session cookie -> server-side API token -> `/v1/routes` -> selected daemon/account -> OpenAI-compatible chat completion.

## Current State

- `/manual/` is an operator QA surface. It requires pasting a bearer token.
- `kittychat` already verifies API-issued RS256 user JWTs through JWKS.
- `kittychat` already accepts daemon WSS connections with API-issued RS256 device JWTs.
- `api.kittypaw.app` device credential E2E passed in prod.
- `kittyapi` Plan 25 contract is Authorization Code with PKCE. The API redirects only `code` + `state` back to chat; `kittychat` exchanges the code server-to-server and keeps the token pair out of browser JavaScript.

## Routes

- `/`
  - Public entry page.
  - If `/app/api/session` reports an existing server-side session, redirect to `/app/`.
  - Otherwise show a concise login state.
  - The primary login link points to `/auth/login/google`.

- `/auth/login/google`
  - Starts a PKCE login.
  - Generates `state`, `code_verifier`, and S256 `code_challenge`.
  - Stores the verifier server-side for a short TTL.
  - Redirects to `api.kittypaw.app/auth/web/google` with `redirect_uri`, `state`, `code_challenge`, and `code_challenge_method=S256`.

- `/auth/callback`
  - Server-side callback endpoint.
  - Accepts only `code` + `state`.
  - Validates and consumes the pending PKCE state.
  - Calls API `POST /auth/web/exchange` server-to-server with `code`, `code_verifier`, and `redirect_uri`.
  - Stores the returned token pair in an in-memory server-side session.
  - Sets `kittychat_session` as `HttpOnly; Secure; SameSite=Lax`.
  - Redirects to `/app/`.
  - If code/state/exchange validation fails, shows a recoverable auth error and a link back to `/`.

- `/app/`
  - Hosted chat app.
  - Requires a server-side session; otherwise `/app/api/*` returns 401 and the browser redirects to `/`.
  - Calls `/app/api/routes`; the server injects `Authorization: Bearer <access_token>` when proxying to the existing OpenAI-compatible handler.
  - Selects the first available route/account by default and corrects stale selections after every route reload.
  - Sends non-streaming chat completions through `/app/api/nodes/{device}/accounts/{account}/v1/chat/completions`.
  - Shows daemon offline and no-route states explicitly.

- `/auth/logout`
  - Deletes the server-side session and clears the HttpOnly cookie.

- `/manual/`
  - Remains QA-only and unchanged in purpose.

## Browser Storage

Do not store API access or refresh tokens in browser storage.

- `kittychat_session`: HttpOnly cookie carrying only an opaque chat-server session ID.
- `kittychat-app-state-v1`: selected device, account, model, messages. This is product state only, not credential material.

Access and refresh tokens live only in `kittychat` process memory. This is acceptable for the current single-instance deployment. A durable/distributed session store is a future scaling task, not needed for the first hosted web slice.

## API Contract Needed

Hosted login uses API Plan 25:

```text
GET https://api.kittypaw.app/auth/web/google
  ?redirect_uri=https%3A%2F%2Fchat.kittypaw.app%2Fauth%2Fcallback
  &state=<chat_state>
  &code_challenge=<S256(code_verifier)>
  &code_challenge_method=S256
```

On success, API redirects to:

```text
https://chat.kittypaw.app/auth/callback?code=<one_time_code>&state=<chat_state>
```

Then `kittychat` exchanges the code:

```text
POST https://api.kittypaw.app/auth/web/exchange
{ "code": "...", "code_verifier": "...", "redirect_uri": "https://chat.kittypaw.app/auth/callback" }
```

Refresh uses the existing API endpoint:

```text
POST https://api.kittypaw.app/auth/token/refresh
{ "refresh_token": "..." }
```

## Error Handling

- Non-2xx HTTP responses are displayed as transport/system errors, not assistant messages.
- JSON errors include their server message, e.g. `HTTP 503 Service Unavailable: device offline`.
- HTML proxy error pages are summarized by HTTP status only.
- If routes are empty, the app shows an offline/setup state instead of a blank chat.
- If the BFF session is missing or refresh fails, `/app/api/*` returns 401, clears the cookie, and the app returns to `/`.
- If the proxied OpenAI handler returns 401, the BFF refreshes once and retries before returning 401.

## Testing

- Router tests for `/`, `/app/`, mounted web handler routes, and existing `/manual/`.
- `internal/webapp` tests for PKCE login redirect, callback exchange, HttpOnly session cookie, server-side bearer proxying, and refresh-before-proxy.
- Static JS tests for:
  - no browser token storage helper exposure
  - app calls `/app/api/*` without an `Authorization` header
  - stale route correction
  - HTTP error formatting
- Existing `make build`, `make test`, `make lint`, and `make smoke-local` remain required.
