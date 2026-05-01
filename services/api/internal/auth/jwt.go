package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID string   `json:"uid"`
	Scope  []string `json:"scope,omitempty"`
	V      int      `json:"v,omitempty"`
	jwt.RegisteredClaims
}

// Sign issues a JWT with no audience or scopes — kept for backward compatibility.
// New issuance paths should use SignForAudiences.
func Sign(userID, secret string, ttl time.Duration) (string, error) {
	return SignForAudiences(userID, nil, nil, secret, ttl)
}

// SignForAudiences issues a JWT with explicit audiences and scopes.
// Pins claims version to 1 (Plan 17 spec D4 — additive only).
// docs/specs/kittychat-credential-foundation.md
func SignForAudiences(userID string, audiences []string, scopes []string, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Scope:  scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings(audiences),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	if len(audiences) > 0 || len(scopes) > 0 {
		claims.V = 1
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func Verify(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
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
