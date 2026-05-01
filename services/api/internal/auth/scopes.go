package auth

// Plan 17 — kittychat credential foundation.
// docs/specs/kittychat-credential-foundation.md (D3 + D4).
//
// scope vocabulary is additive only — never rename or remove existing entries.
// To extend, add new constants here and pin them in the spec.

const (
	ScopeChatRelay     = "chat:relay"
	ScopeModelsRead    = "models:read"
	ScopeDaemonConnect = "daemon:connect"

	AudienceKittyAPI  = "kittyapi"
	AudienceKittyChat = "kittychat"

	ClaimsVersion = 1
)

// DefaultAPIClientScopes is the scope set granted to OAuth-issued
// access tokens (web/CLI users).
var DefaultAPIClientScopes = []string{ScopeChatRelay, ScopeModelsRead}

// DefaultAPIClientAudiences is the audience set for OAuth-issued
// access tokens — the same token validates against kittyapi self-checks
// and kittychat-side verification.
var DefaultAPIClientAudiences = []string{AudienceKittyAPI, AudienceKittyChat}
