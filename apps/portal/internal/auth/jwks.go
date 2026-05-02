package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// HandleJWKS publishes the active JWK Set at /.well-known/jwks.json.
//
// Cache-Control max-age=600 (10 min) is paired with kittychat's cache
// TTL of the same value — both sides drifting out of alignment would
// break the rotation contract (old key must overlap for access TTL
// 15min + cache 10min + safety 5min = 30min minimum).
//
// The endpoint is anonymous — JWKS public keys are by definition
// public, and verifiers must be able to fetch them without holding
// a token.
func HandleJWKS(provider JWKSProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=600")
		// Log encode failures: by the time we hit this branch the
		// status + headers are already on the wire, so the client
		// receives a partial-but-typed response. Logging surfaces
		// the rare malformed-JWKSet bug without changing the wire
		// contract.
		if err := json.NewEncoder(w).Encode(provider.JWKSet()); err != nil {
			slog.Error("encode JWKS response", "err", err)
		}
	}
}
