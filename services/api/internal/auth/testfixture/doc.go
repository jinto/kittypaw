// Package testfixture provides test helpers for auth flows.
//
// IssueTestJWT signs a JWT directly via auth.SignForAudiences, bypassing the OAuth provider.
// This is intentional for plans that exercise authenticated request paths
// (rate limiting, /auth/me, proxy with Bearer). OAuth provider regressions
// are NOT caught here — those belong to oauth_e2e_*_test.go (Plan 15, deferred).
//
// SeedTestUser creates a unique user via the configured UserStore. Each call
// produces a distinct provider_id (UnixNano) so collisions are impossible.
// Teardown is intentionally omitted: UserStore has no Delete method, and
// integration tests assume a refreshable PostgreSQL (docker-compose or fixture
// reset). Adding Delete to UserStore is out of scope for Plan 11 (production
// code changes forbidden).
package testfixture
