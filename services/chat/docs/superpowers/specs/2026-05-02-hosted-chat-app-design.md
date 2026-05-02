# Hosted Chat App Design

Date: 2026-05-02

## Goal

Make `https://chat.kittypaw.app` the user-facing chat surface instead of a manual QA page. A logged-in user should land in a hosted chat app that uses the same relay path already verified by `/manual/`: user JWT -> `/v1/routes` -> selected daemon/account -> OpenAI-compatible chat completion.

## Current State

- `/manual/` is an operator QA surface. It requires pasting a bearer token.
- `kittychat` already verifies API-issued RS256 user JWTs through JWKS.
- `kittychat` already accepts daemon WSS connections with API-issued RS256 device JWTs.
- `api.kittypaw.app` device credential E2E passed in prod.
- Current `kittyapi` OAuth login endpoints do not yet redirect tokens back to a chat-hosted callback. `/auth/google/callback` issues token JSON on the API host. CLI OAuth can redirect to `127.0.0.1`, but that is not suitable for hosted web chat.

## Routes

- `/`
  - Public entry page.
  - If an access token is already present in browser storage, redirect to `/app/`.
  - Otherwise show a concise login state.
  - The primary login button points to the future API web login endpoint once available.

- `/auth/callback`
  - Browser-only callback receiver.
  - Reads `access_token`, optional `refresh_token`, `token_type`, and `expires_in` from query string or fragment.
  - Stores tokens in browser storage and immediately removes them from the URL with `history.replaceState`.
  - Redirects to `/app/`.
  - If no token is present, shows a recoverable auth error and a link back to `/`.

- `/app/`
  - Hosted chat app.
  - Requires a stored access token; otherwise redirects to `/`.
  - Calls `/v1/routes` with `Authorization: Bearer <access_token>`.
  - Selects the first available route/account by default and corrects stale selections after every route reload.
  - Sends non-streaming chat completions through `/nodes/{device}/accounts/{account}/v1/chat/completions`.
  - Shows daemon offline and no-route states explicitly.

- `/manual/`
  - Remains QA-only and unchanged in purpose.

## Browser Storage

Use `localStorage` for the first slice so refresh/reopen works during manual product testing:

- `kittychat-auth-v1`: access token, optional refresh token, expiration timestamp.
- `kittychat-app-state-v1`: selected device, account, model, messages.

This is a pragmatic first slice. A later hardening pass can move to an HttpOnly cookie/BFF pattern if the product needs stronger browser-token containment.

## API Contract Needed

Hosted login requires one API endpoint or mode that finishes provider OAuth and redirects to chat:

```text
GET https://api.kittypaw.app/auth/web/google?redirect_uri=https%3A%2F%2Fchat.kittypaw.app%2Fauth%2Fcallback
```

On success, API redirects to:

```text
https://chat.kittypaw.app/auth/callback#access_token=...&refresh_token=...&token_type=Bearer&expires_in=900
```

Fragment is preferred over query string so tokens are not sent back to `kittychat` in HTTP request logs. `kittychat` will support both query and fragment for development and compatibility.

Until this API contract exists, `/auth/callback` can be tested manually by opening it with a token fragment, and `/manual/` remains the fully working QA entry point.

## Error Handling

- Non-2xx HTTP responses are displayed as transport/system errors, not assistant messages.
- JSON errors include their server message, e.g. `HTTP 503 Service Unavailable: device offline`.
- HTML proxy error pages are summarized by HTTP status only.
- If routes are empty, the app shows an offline/setup state instead of a blank chat.
- If token verification fails with 401, the app clears stored auth and returns to `/`.

## Testing

- Router tests for `/`, `/app/`, `/auth/callback`, and existing `/manual/`.
- Static JS tests for:
  - callback token parsing from fragment/query
  - URL token removal intent
  - stale route correction
  - HTTP error formatting
- Existing `make build`, `make test`, `make lint`, and `make smoke-local` remain required.
