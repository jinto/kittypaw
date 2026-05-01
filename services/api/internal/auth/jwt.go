package auth

import (
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

// SignForAudiences issues a JWT with explicit audiences and scopes.
// Pins claims version to 1 (Plan 17 spec D4 — additive only).
// docs/specs/kittychat-credential-foundation.md
func SignForAudiences(userID string, audiences []string, scopes []string, secret string, ttl time.Duration) (string, error) {
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
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// Verify parses and validates a JWT issued by SignForAudiences.
// Strict on iss (exact match) + aud (must contain AudienceAPI) — Plan 13 H1.
func Verify(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	},
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(AudienceAPI),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
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
