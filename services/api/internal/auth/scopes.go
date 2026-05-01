package auth

// Plan 13 — auth authority vs resource server (URL form).
// docs/specs/kittychat-credential-foundation.md (D2 + D3 + D4 + D8).
//
// Issuer/audience use RFC 7519 / OIDC URL-form identifiers (not opaque strings).
// scope vocabulary is additive only — never rename or remove existing entries.
// To extend, add new constants here and pin them in the spec.

const (
	ScopeChatRelay     = "chat:relay"
	ScopeModelsRead    = "models:read"
	ScopeDaemonConnect = "daemon:connect"

	// AudienceAPI / AudienceChat identify the resource servers this token is
	// valid against. The same token validates against kittyapi self-checks
	// and kittychat-side verification (multi-aud).
	AudienceAPI  = "https://api.kittypaw.app"
	AudienceChat = "https://chat.kittypaw.app"

	// Issuer identifies the auth authority. Path-based (api.kittypaw.app/auth)
	// matches today's deployment where /auth/* is hosted under the api host.
	// Future host split (auth.kittypaw.app) requires dual-accept migration.
	Issuer = "https://api.kittypaw.app/auth"

	ClaimsVersion = 1
)

// DefaultAPIClientScopes is the scope set granted to OAuth-issued
// access tokens (web/CLI users).
var DefaultAPIClientScopes = []string{ScopeChatRelay, ScopeModelsRead}

// DefaultAPIClientAudiences is the audience set for OAuth-issued access tokens.
var DefaultAPIClientAudiences = []string{AudienceAPI, AudienceChat}
