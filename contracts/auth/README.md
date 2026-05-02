# Auth Contract

Produced by `services/api`.

Consumed by:

- `services/api` middleware for API audience checks
- `services/chat` verifier for Chat audience checks
- `apps/kittypaw` as opaque bearer credentials

The daemon treats access tokens as opaque. Signature verification belongs to
resource servers.
