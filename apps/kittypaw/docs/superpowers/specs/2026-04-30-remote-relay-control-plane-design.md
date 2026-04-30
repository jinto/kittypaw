# Remote Relay Control Plane Design

## Goal

Provide `chat.kittypaw.app` and OpenAI-compatible hosted API access that can reach a user's home or server Kittypaw daemon without requiring Tailscale, port forwarding, or inbound firewall changes.

The hosted service is first-party: account login, device registration, API keys, and relay authorization are owned by Kittypaw. Cloudflare sits in front as the edge/TLS/WAF layer, not as the product's trust model.

## Dependency

This design depends on the local multi-user account identity work:

```text
docs/superpowers/specs/2026-04-30-local-multi-user-account-identity-design.md
docs/superpowers/plans/2026-04-30-local-multi-user-account-identity.md
```

The relay must route to a specific local account on a specific device:

```text
cloud user -> device -> local account -> local OpenAI-compatible handler
```

If local Kittypaw still falls back to `accounts/default/`, the hosted service cannot safely support multiple local users.

## Architecture

```text
Browser / Open WebUI
  -> Cloudflare
  -> kittypaw-relayd
      - first-party cloud auth
      - device registry
      - OpenAI-compatible API gateway
      - WebSocket relay broker
  -> outbound WSS connection
  -> local kittypaw daemon connector
  -> account-scoped local OpenAI-compatible API
```

The local daemon initiates a persistent outbound WebSocket to the relay. The relay never opens inbound connections to the user's machine.

## Components

### Local OpenAI-Compatible API

The local daemon exposes a narrow account-scoped API:

```text
GET  /v1/models
POST /v1/chat/completions
GET  /health
```

The API must not become a generic localhost proxy. It calls existing Kittypaw engine sessions by local account ID.

### Remote Protocol

Daemon and relay communicate with JSON frames over WSS for the MVP:

```json
{"type":"hello","device_id":"dev_...","local_accounts":["alice"],"version":"..."}
{"type":"request","id":"req_...","account_id":"alice","method":"POST","path":"/v1/chat/completions","body":{...}}
{"type":"response_headers","id":"req_...","status":200,"headers":{"content-type":"text/event-stream"}}
{"type":"response_chunk","id":"req_...","data":"data: ...\n\n"}
{"type":"response_end","id":"req_..."}
{"type":"error","id":"req_...","code":"offline","message":"device offline"}
```

Binary/protobuf can be added later. JSON is easier to debug while the trust boundaries settle.

### Cloud Auth

Cloud auth is separate from local auth:

- Cloud users log into `chat.kittypaw.app`.
- Local account passwords are never sent to the cloud.
- Pairing proves local control and grants cloud access to a selected local account.

Cloud credentials:

- browser session cookie for `chat.kittypaw.app`
- device credential for daemon WSS
- OpenAI-compatible API key for Open WebUI

Each credential type is independently revocable.

### Device Pairing

Pairing flow:

1. User logs into `chat.kittypaw.app`.
2. User creates a short-lived pairing code.
3. On the local machine, user runs:

   ```text
   kittypaw remote pair
   ```

4. CLI authenticates to local Kittypaw account or uses an existing local browser session.
5. Relay issues a device credential scoped to:

   ```text
   cloud_user_id, device_id, local_account_id
   ```

6. Local daemon stores the credential under the selected account or device-level remote config.

### Hosted API

The hosted API shape:

```text
GET  https://api.kittypaw.app/nodes/{device_id}/v1/models
POST https://api.kittypaw.app/nodes/{device_id}/v1/chat/completions
```

Open WebUI can configure this as an OpenAI-compatible backend with a Kittypaw API key.

### Hosted Chat

`chat.kittypaw.app` provides:

- login/logout
- device list
- online/offline state
- selected-node chat
- API key management for Open WebUI
- device revoke

The first hosted chat UI can be minimal. It should prove auth, device selection, streaming, and permission request forwarding before polishing.

## Storage

Use Postgres for the hosted control plane:

- users
- sessions
- devices
- device_credentials
- pairing_codes
- api_keys
- audit_events

Use an in-memory connection registry for single-instance MVP. Add Redis or NATS only when running multiple relay instances.

## Cloudflare Role

Cloudflare handles:

- TLS termination
- WAF/rate limits
- WebSocket pass-through
- origin protection
- basic bot filtering

Kittypaw still validates every credential at the Go origin. Do not trust Cloudflare headers as authentication unless a later deployment pins mTLS or signed Access JWT verification.

## Security Rules

- No generic HTTP proxying to localhost.
- No local password transfer to cloud.
- Device credentials are revocable and scoped.
- API keys are user/node scoped.
- Requests must authorize both cloud user ownership and local account scope.
- Relay request size, response size, concurrent streams, and stream lifetime are bounded.
- Audit events record pairing, device connect/disconnect, API key creation, revoke, and remote request metadata.

## Non-Goals

- No multi-relay sharding in MVP.
- No organization/team sharing in MVP.
- No arbitrary local Web UI tunneling in MVP.
- No Cloudflare Tunnel dependency in product flow.
- No Open WebUI hosting inside this repo; expose a compatible API instead.
