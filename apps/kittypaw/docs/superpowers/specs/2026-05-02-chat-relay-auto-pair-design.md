# Chat Relay Auto Pair Design

Date: 2026-05-02

## Goal

Make hosted chat relay usable after API login without requiring users to know about `kittypaw chat-relay pair`.

## Scope

This slice is daemon/CLI-side only. Kittyapi still owns device credential issuance. Kittychat still verifies device access tokens.

## Behavior

After `kittypaw login` or setup step `[6/6]` obtains a user access token, the CLI attempts chat relay device pairing once when:

- discovery has provided a non-empty `chat_relay_url`
- no complete chat relay device token set is already stored
- the user access token is non-empty

The pairing request uses the stored `auth_base_url` and a host-derived device name. Success stores `chat_relay_device_id`, `chat_relay_access_token`, and `chat_relay_refresh_token`.

## Error Handling

Pairing is best-effort. Failure does not fail login or setup because the API endpoint may not be deployed yet, and local Telegram/Kakao/CLI workflows must remain usable. Failures are reported as a short warning with the manual fallback command:

`kittypaw chat-relay pair`

## Out Of Scope

This does not add API-side device revocation, OS keyring storage, or automatic pairing from `kittypaw serve`.
