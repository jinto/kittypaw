# Architecture Constraints

These rules keep the monorepo from turning into a tightly coupled monolith.

## 1. Independent Deployables

Each deployable unit must be buildable and releasable on its own.

- `apps/kittypaw` releases the local product binary.
- `apps/kittyapi` releases the cloud API service.
- `apps/chat` releases the chat relay service.
- `apps/kakao` releases the Kakao gateway service.

## 2. App-Owned Data

Each app owns its persistence.

- Direct cross-app database access is prohibited.
- Migrations live with the app that owns the database.
- Data sharing happens through published APIs, relay frames, or documented
  events.

## 3. Contracts Before Shared Code

Cross-app agreement starts in `contracts/`, not in shared runtime packages.

Good shared contract material:

- JSON Schema
- OpenAPI fragments
- protocol examples
- enum lists
- JWT claim fixtures
- generated types derived from schemas, when needed

Avoid:

- importing another app's internal Go packages
- sharing database entities across apps
- creating a common package for convenience before contract tests exist

## 4. Minimal Shared Runtime Libraries

Shared code is allowed only when all of the following are true:

- At least two services need the same behavior.
- The behavior is not service-domain logic.
- The shared package has a stable public API.
- The package has tests independent of any one service.

Initial shared runtime packages should be avoided. Start with contracts and
testkit.

## 5. Contract Change Gate

A contract change is incomplete until producer and consumer tests pass together.

Examples:

- Discovery contract change: run API producer tests and Kittypaw consumer tests.
- Auth claim change: run API JWT issuer tests and Chat verifier tests.
- Chat relay frame change: run Chat broker tests and Kittypaw connector tests.
- Kakao frame change: run Kakao gateway tests and Kittypaw Kakao channel tests.

## 6. Release Tag Namespace

Product release tags must be namespaced:

```text
kittypaw/v0.1.0
kittyapi/v0.1.0
chat/v0.1.0
kakao/v0.1.0
```

Plain `v0.1.0` tags are reserved for future repo-wide releases, if ever needed.
