package auth

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload shape issued by kittyapi.
//
// JSON wire format follows RFC 7519: subject is encoded as "sub" (not
// the legacy "uid"). Cross-service verifiers (kittychat) read the
// standard sub claim directly with no fallback hack.
//
// docs/specs/kittychat-credential-foundation.md (D2 schema, sub + iss).
type Claims struct {
	UserID string   `json:"sub"`
	Scope  []string `json:"scope,omitempty"`
	V      int      `json:"v,omitempty"`
	jwt.RegisteredClaims
}

// SignForAudiences issues an RS256 JWT with explicit audiences and scopes.
// header carries kid (RFC 7515 §4.1.4) so verifiers can resolve the
// signing key via JWKS lookup.
//
// Plan 21 PR-B: HS256 secret-based signing replaced with RS256 key +
// kid pair. ClaimsVersion = 2 (Plan 20 cutover, BC 부담 0 — 사용자 0명).
//
// docs/specs/kittychat-credential-foundation.md D5.
func SignForAudiences(userID string, audiences []string, scopes []string, key *rsa.PrivateKey, kid string, ttl time.Duration) (string, error) {
	if key == nil {
		return "", fmt.Errorf("private key is nil")
	}
	if kid == "" {
		return "", fmt.Errorf("kid is empty")
	}
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Scope:  scopes,
		V:      ClaimsVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Audience:  jwt.ClaimStrings(audiences),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	return token.SignedString(key)
}

// Verify parses and validates a JWT issued by SignForAudiences.
//
// Strict invariants (Plan 21 PR-B):
//   - alg=RS256 only — `WithValidMethods([RS256])` blocks alg=HS256
//     downgrade attacks even when the token is "valid" by golang-jwt's
//     default permissive verification
//   - iss exact match (docs/specs D8 path-form issuer)
//   - aud strict — caller passes the expected audience. user middleware
//     pins AudienceAPI; future device middleware will pin AudienceChat.
//     This is the cross-audience leak guard.
//   - leeway 60s — clock skew tolerance agreed with kittychat
//   - kid resolved via JWKSProvider.Lookup — unknown kid → reject
func Verify(tokenString string, jwks JWKSProvider, audience string) (*Claims, error) {
	if jwks == nil {
		return nil, fmt.Errorf("jwks provider is nil")
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		// Defense-in-depth: reject anything that isn't RS256 before
		// even attempting the lookup. WithValidMethods below also
		// catches this; we double-check here so an unsupported alg
		// can't reach the JWKS provider.
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("missing kid header")
		}
		return jwks.Lookup(kid)
	},
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(audience),
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithLeeway(60*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	if claims.UserID == "" {
		return nil, fmt.Errorf("missing user ID in token")
	}
	return claims, nil
}
