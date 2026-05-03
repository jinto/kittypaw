# Fake Daemon Testkit

Reserved package for reusable fake Kittypaw daemon helpers.

Current cross-service E2E tests use the real Kittypaw dispatcher with fake
registry and upstream services. Add code here only when a test needs to emulate
the daemon/device side instead of booting the real Kittypaw service.
