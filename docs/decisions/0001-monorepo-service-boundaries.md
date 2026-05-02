# Decision 0001: Monorepo With Independent Services

Date: 2026-05-02

## Decision

Use a monorepo for KittyPaw product development, but keep deployable services
separate.

## Rationale

The current projects change together through shared wire contracts:

- API discovery is produced by the API service and consumed by Kittypaw.
- JWT/JWKS credentials are issued by the API service and verified by Chat.
- Chat relay frames are produced by Chat and consumed by the Kittypaw daemon.
- Kakao relay frames are produced by the Kakao gateway and consumed by Kittypaw.

Keeping these projects in separate repositories makes it harder to enforce
contract tests across producer and consumer changes. A monorepo improves
verification without requiring a single runtime or shared database.

## Consequences

- Existing deployables remain separate.
- Product release tags must be namespaced.
- Contracts become first-class source files.
- Shared runtime code remains intentionally small.
- Root CI should run affected service tests when contracts change.
