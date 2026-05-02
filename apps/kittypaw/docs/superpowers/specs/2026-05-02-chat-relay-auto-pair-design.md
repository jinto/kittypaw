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

Pairing is best-effort. Failure does not fail login or setup because the API endpoint may not be deployed yet, and local Telegram/Kakao/CLI workflows must remain usable. Failures are reported as a short warning. Users should not need to know about chat relay device IDs or manual pairing commands; the next `kittypaw login` or `kittypaw setup` retries automatic pairing.

## Out Of Scope

This does not add user-facing device revocation, user-facing disconnect, OS keyring storage, or automatic pairing from `kittypaw serve`. Stale device cleanup belongs to the API/chat services as an automatic lifecycle policy.
