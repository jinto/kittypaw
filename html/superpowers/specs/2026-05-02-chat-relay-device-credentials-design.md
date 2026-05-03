# Chat Relay Device Credentials Design

> Historical plan snapshot. This document records the implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

Date: 2026-05-02

## Goal

Let a local server process obtain, store, refresh, and present API-issued device credentials when connecting outbound to `chat.kittypaw.app`.

## Scope

This slice is server/CLI-side only. Kittyapi owns device credential issuance and RS256 signing. Kittychat owns JWT/JWKS verification. The server treats the access token as an opaque Bearer token and does not verify its signature.

## Data Model

Discovery accepts an optional `auth_base_url` field. It is stored under the API namespace and defaults to `<api_url>/auth` when absent.

Per account, under the API namespace, the server stores:

- `chat_relay_url`
- `chat_relay_device_id`
- `chat_relay_access_token`
- `chat_relay_refresh_token`

The previous single `chat_daemon_credential` key is not used by new code.

## Flows

`kittypaw chat-relay pair` loads the user access token, calls `POST {auth_base_url}/devices/pair`, and stores `device_id`, `device_access_token`, and `device_refresh_token`.

`kittypaw server start` loads stored chat relay topology. If the access token is expired or near expiry, it calls `POST {auth_base_url}/devices/refresh` with the device refresh token before opening `/daemon/connect`. Incomplete local token sets are treated as unpaired and skipped.

If the chat relay returns unauthorized during connect, the connector refreshes the credential once and reconnects. If refresh fails, the connector logs and backs off; local server startup still succeeds.

## Out Of Scope

OS keyring storage is a follow-up. For this slice, existing per-account `SecretsStore` remains the backing store.

API-side revoke/delete endpoints are not required for local `disconnect`; local disconnect clears stored device credentials only.
