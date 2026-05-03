# KittyPaw Connect Gmail OAuth Design

## Summary

KittyPaw Connect is the user-facing surface for linking external services such
as Gmail, Slack, and Notion. For v1 it should run inside the existing
`apps/portal` service and be exposed through `connect.kittypaw.app`.

The internal app name, Go module, binary, service name, and JWT issuer stay
`portal`/`kittyportal`. Only the user-facing product surface is branded as
`KittyPaw Connect`.

## Decisions

- Keep one `apps/portal` binary.
- Add `connect.kittypaw.app` as a second host routed to the same portal
  upstream.
- Keep `portal.kittypaw.app` as the identity, discovery, JWKS, and device
  authority.
- Add Connect routes under `/connect/*`, served only on the Connect host.
- Enforce Connect host routing with a host-only check, not by inferring from
  whether the portal and API hosts are split. Connect must stay isolated even in
  collapsed local/dev deployments.
- Advertise `connect_base_url` from portal discovery so local KittyPaw can find
  the Connect surface without hardcoding host replacement.
- Do not rename `apps/portal` to `apps/connect`.
- Do not create `apps/connect` in v1.
- First provider: Gmail.
- First Gmail scope target: `https://www.googleapis.com/auth/gmail.readonly`,
  because mail summary requires message bodies. This is a restricted scope and
  must be treated as a launch risk.
- Use Connect-specific Google OAuth client settings. Do not reuse the existing
  KittyPaw identity login client for Gmail restricted scopes.
- Portal brokers OAuth and refreshes tokens, but v1 stores third-party Gmail
  tokens locally in the account-scoped KittyPaw secrets store. Portal should
  not persist Gmail access or refresh tokens in v1.

## Why Same Server

Connect depends on infrastructure that portal already owns: OAuth callback
state, user identity, refresh-token discipline, domain discovery, JWKS, device
pairing, deployment, and host boundary checks. Splitting a new service now would
force either direct DB reads across app boundaries or a new portal-to-connect
service contract before the first user-facing Gmail workflow exists.

The safer boundary is:

- same binary and database for now;
- separate hostnames and route groups;
- separate package namespace in code;
- no cross-app DB access;
- clear extraction trigger later.

## Domain And Branding

Use:

```text
portal.kittypaw.app   -> identity/discovery authority
connect.kittypaw.app  -> external account connection UI and callbacks
```

`CONNECT_BASE_URL=https://connect.kittypaw.app` should be added alongside the
existing `BASE_URL=https://portal.kittypaw.app`.

Use separate Google OAuth client environment variables for Connect:

```text
CONNECT_GOOGLE_CLIENT_ID=
CONNECT_GOOGLE_CLIENT_SECRET=
```

The existing `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` remain for KittyPaw
identity sign-in on `portal.kittypaw.app`. This separation keeps restricted
Gmail scope testing from destabilizing the existing login client and lets the
Connect client use its own redirect URI set.

Google's current OAuth branding guidance allows subdomains once the top private
domain is verified as an authorized domain. Therefore `kittypaw.app` should be
the authorized domain, and `connect.kittypaw.app` can be used for Connect
home, privacy, terms, and redirect URLs.

The consent-screen app name should be `KittyPaw Connect` or `KittyPaw`, not a
name that combines Google's product names with a generic app label. Google
branding guidance explicitly warns against app names that look like official
Google products.

## OAuth Policy Constraints

Official Google docs currently classify:

- `gmail.send` as sensitive;
- `gmail.readonly`, `gmail.modify`, `gmail.metadata`, and broad
  `mail.google.com` as restricted.

`gmail.readonly` is the narrowest practical Gmail scope for the first "summarize
my mail" workflow because metadata-only scopes do not provide message body
content. Because `gmail.readonly` is restricted, production launch planning must
include restricted-scope verification and the possibility of an annual security
assessment if Google determines the server can access restricted data.

For development and beta, use a separate Google Cloud project in testing mode
with test users. Do not add unapproved restricted scopes to the production OAuth
project before the app is ready for verification.

The production Connect client can live in the same Google Cloud project as the
identity client only if the team intentionally accepts project-level OAuth
consent-screen coupling. The safer default is a separate Connect project or at
least a separate OAuth client with separate environment variables.

Sources checked:

- Google OAuth branding and authorized domains:
  https://support.google.com/cloud/answer/15549049
- Google Gmail API scopes:
  https://developers.google.com/workspace/gmail/api/auth/scopes
- Google sensitive scope verification:
  https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification
- Google restricted scope verification:
  https://developers.google.com/identity/protocols/oauth2/production-readiness/restricted-scope-verification
- Google OAuth app changes and re-verification:
  https://support.google.com/cloud/answer/13464018
- Google API Services User Data Policy:
  https://developers.google.com/terms/api-services-user-data-policy

## Route Shape

Identity routes stay unchanged:

```text
GET  https://portal.kittypaw.app/
GET  https://portal.kittypaw.app/discovery
GET  https://portal.kittypaw.app/.well-known/jwks.json
GET  https://portal.kittypaw.app/auth/google
GET  https://portal.kittypaw.app/auth/cli/google
```

Portal discovery should add:

```json
{
  "connect_base_url": "https://connect.kittypaw.app"
}
```

Connect routes are new:

```text
GET  https://connect.kittypaw.app/
GET  https://connect.kittypaw.app/connect
GET  https://connect.kittypaw.app/connect/gmail/login?mode=http&port=12345
GET  https://connect.kittypaw.app/connect/gmail/callback
POST https://connect.kittypaw.app/connect/cli/exchange
POST https://connect.kittypaw.app/connect/gmail/refresh
```

The callback must not redirect Google access or refresh tokens through URL
query parameters. Instead, the callback creates a short-lived one-time Connect
code:

- HTTP mode redirects to `http://127.0.0.1:{port}/callback?code=...`.
- Code mode renders a short code for copy/paste.
- The local CLI exchanges that one-time code with portal and receives the Gmail
  token response over HTTPS POST.

This is safer than the current API login query-token callback pattern and should
be the Connect default from the first implementation.

## Code Boundaries

New portal package:

```text
apps/portal/internal/connect
```

Responsibilities:

- provider definitions for external service connections;
- Connect state metadata;
- one-time Connect code store;
- Gmail authorization URL generation;
- Gmail code exchange;
- Gmail refresh-token exchange;
- Connect HTTP handlers.

Existing portal package remains focused on KittyPaw identity:

```text
apps/portal/internal/auth
```

Do not refactor `internal/auth` into a generic provider registry as the first
step. Google login and Gmail connection share OAuth mechanics but differ in
scope, token shape, persistence, and user-facing semantics. Keep duplication
small and obvious until the second provider proves the abstraction.

KittyPaw local changes belong under:

```text
apps/kittypaw/core
apps/kittypaw/cli
apps/kittypaw/engine
apps/kittypaw/mcp
```

Local token storage namespace:

```text
oauth-gmail/access_token
oauth-gmail/refresh_token
oauth-gmail/expires_at
oauth-gmail/scope
oauth-gmail/email
oauth-gmail/connect_base_url
```

This keeps third-party OAuth credentials account-scoped under
`~/.kittypaw/accounts/<accountID>/secrets.json`.

## Local Token Lifecycle

KittyPaw local daemon should own runtime Gmail access.

Flow:

1. User runs `kittypaw connect gmail`.
2. CLI fetches portal discovery and resolves `connect_base_url`.
3. CLI opens `connect.kittypaw.app/connect/gmail/login`.
4. Portal completes Google OAuth and creates a short-lived Connect code.
5. CLI exchanges the Connect code for Gmail token data.
6. CLI stores Gmail tokens in the selected account's `secrets.json`.
7. When a Gmail access token expires, local KittyPaw calls
   `POST /connect/gmail/refresh` with the Gmail refresh token and stores the new
   access token.
8. MCP servers or packages receive only current access tokens via local
   config/env injection.

Portal should not persist Gmail refresh tokens in v1. If Connect later needs a
server-side token vault for web or multi-device sync, that should be a separate
design with encryption-at-rest, revocation, audit, and breach-response
requirements.

## Gmail API And First Workflow

The first user-visible scenario should be read-only:

```text
"오늘 받은 중요한 메일 요약해줘"
```

Use the native KittyPaw package path for the first Gmail workflow. A
deterministic mail-digest package can source `oauth-gmail/access_token` from the
local secrets store and call Gmail over HTTPS directly, which proves OAuth,
refresh, source-bound config, and read-only Gmail access without introducing a
new local MCP server binary.

MCP token injection remains useful for later third-party Gmail, Slack, and
Notion MCP servers, but it should not block the first Gmail digest.

## Error Handling

- Connect routes requested on `portal.kittypaw.app` should 404.
- Identity routes requested on `connect.kittypaw.app` should 404, except
  `/health` if deployment smoke requires host-agnostic health.
- Missing `CONNECT_BASE_URL` should disable Connect routes in production config
  rather than silently reusing `BASE_URL`.
- Gmail Connect should fail clearly when Google scope approval is not available.
- Workspace admin blocks are expected for some Google Workspace users and must
  surface as unsupported-by-admin, not as generic auth failure.
- Refresh failures should instruct the user to rerun `kittypaw connect gmail`.

## Extraction Triggers

Create a separate `apps/connect` only if at least one of these becomes true:

- Connect has an independent UI and release cadence.
- Connect tokens need a server-side vault and stricter operational isolation.
- Connect provider count or traffic needs independent scaling.
- Connect incidents must not affect portal login/discovery/JWKS.
- A separate team or deployment owner exists.

Until then, separate hostnames and code packages are enough.

## Non-Goals

- No Composio dependency.
- No `apps/connect` service in v1.
- No `portal` -> `connect` rename.
- No Gmail send/modify/delete in the first public workflow.
- No server-side Gmail token vault in v1.
- No shared runtime package outside app boundaries.

## Testing Strategy

- Portal unit tests for `CONNECT_BASE_URL`, host boundaries, Connect routes, and
  fake-Google OAuth exchange.
- Portal handler tests to prove tokens never appear in redirect query strings.
- Kittypaw core tests for `oauth-gmail` secret save/load/refresh.
- CLI tests for HTTP and code modes.
- Engine/package tests for source-bound `oauth-gmail/access_token` refresh.
- MCP registry tests for dynamic env injection.
- One gated Gmail live test using a test Google Cloud project and test user.

## Open Operational Items

- Create or choose a Google Cloud test project for Connect.
- Verify `kittypaw.app` in Google Search Console under an owner/editor account.
- Add OAuth app branding pages for KittyPaw Connect: public home, privacy
  policy, and optional terms under `kittypaw.app` or `connect.kittypaw.app`.
- Decide whether beta users are limited to test users until restricted-scope
  verification is complete.
