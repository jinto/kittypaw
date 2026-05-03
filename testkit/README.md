# Testkit

Reusable helpers for cross-service tests belong here.

Current coverage:

- Docker-backed Portal + Chat browser-session E2E.
- Chat relay -> real Kittypaw dispatcher -> fake registry skill install E2E.
  The heavier flow covers exchange-rate install/run, installed exchange-rate
  reuse with KRW conversion, weather-now install/run with fake geo resolution,
  and installed weather-now reuse.

Initial candidates:

- JWKS fixture server
- Portal-issued token fixtures
- fake Kakao callback server
- contract validation helpers

Keep testkit independent from service internals. A testkit helper may depend on
`contracts/`, but it should not import private service packages.
