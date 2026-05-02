package testfixture

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/kittypaw-app/kittyapi/internal/auth"
)

// DeviceClaims is the input shape for IssueDeviceJWT — the wire-level
// claims a device JWT carries (Plan 20 spec). Mirroring this struct in
// kittychat's verifier-side test helpers keeps the contract explicit on
// both ends.
type DeviceClaims struct {
	UserID   string
	DeviceID string
	Audience []string
	Scope    []string
	Version  int
	IssuedAt time.Time
	Expires  time.Time
}

type deviceClaimsPayload struct {
	UserID string   `json:"user_id"`
	Scope  []string `json:"scope"`
	V      int      `json:"v"`
	jwt.RegisteredClaims
}

// IssueDeviceJWT signs a device JWT in the wire format kittychat's
// verifier expects. The header carries `alg=RS256` and `kid=<kid>`; the
// payload carries sub=device:<device_id>, user_id, aud, scope, v, iat,
// exp, iss=portal auth endpoint.
//
// This helper exists primarily for fixture generation in tests on both
// sides of the contract — production token issuance lives elsewhere.
// Copying the shape into kittychat's own helper, or vendoring this
// function, are both fine.
func IssueDeviceJWT(privateKey *rsa.PrivateKey, kid string, claims DeviceClaims) (string, error) {
	if privateKey == nil {
		return "", fmt.Errorf("private key is nil")
	}
	if kid == "" {
		return "", fmt.Errorf("kid is empty")
	}

	payload := deviceClaimsPayload{
		UserID: claims.UserID,
		Scope:  claims.Scope,
		V:      claims.Version,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.Issuer,
			Subject:   "device:" + claims.DeviceID,
			Audience:  jwt.ClaimStrings(claims.Audience),
			IssuedAt:  jwt.NewNumericDate(claims.IssuedAt),
			ExpiresAt: jwt.NewNumericDate(claims.Expires),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, payload)
	token.Header["kid"] = kid
	return token.SignedString(privateKey)
}
