# Contracts

Contracts are wire-level source of truth files for communication between apps
and services.

They are not a dumping ground for shared service internals.

## What Belongs Here

- JSON Schema files
- OpenAPI fragments
- JWT claim examples
- enum lists for scopes and operations
- WebSocket frame examples
- compatibility notes and version policies

## What Does Not Belong Here

- database entities
- service repositories
- service-specific config structs
- internal handler interfaces
- convenience utilities

## Test Rule

Every contract must have:

- at least one valid example fixture
- producer tests in the service that emits it
- consumer tests in each service or app that reads it
