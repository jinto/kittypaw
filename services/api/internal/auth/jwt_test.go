package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kittypaw-app/kittyapi/internal/auth"
)

const testSecret = "test-secret-key-for-jwt"

func TestSignVerifyRoundtrip(t *testing.T) {
	token, err := auth.Sign("user-123", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	claims, err := auth.Verify(token, testSecret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if claims.UserID != "user-123" {
		t.Fatalf("expected UserID=user-123, got %q", claims.UserID)
	}
}

func TestVerifyExpired(t *testing.T) {
	token, err := auth.Sign("user-123", testSecret, -1*time.Second)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = auth.Verify(token, testSecret)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	token, err := auth.Sign("user-123", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = auth.Verify(token, "wrong-secret")
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestVerifyMalformed(t *testing.T) {
	_, err := auth.Verify("not-a-jwt-token", testSecret)
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

// Plan 17 — kittychat credential foundation
// (docs/specs/kittychat-credential-foundation.md)

func TestSignForAudiences_RoundTrip(t *testing.T) {
	token, err := auth.SignForAudiences(
		"user-abc",
		[]string{"https://api.kittypaw.app", "https://chat.kittypaw.app"},
		[]string{"chat:relay", "models:read"},
		testSecret,
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf("SignForAudiences: %v", err)
	}
	claims, err := auth.Verify(token, testSecret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.UserID != "user-abc" {
		t.Fatalf("UserID = %q", claims.UserID)
	}
	if got := []string(claims.Audience); len(got) != 2 || got[0] != "https://api.kittypaw.app" || got[1] != "https://chat.kittypaw.app" {
		t.Fatalf("Audience = %v, want [https://api.kittypaw.app https://chat.kittypaw.app] (Plan 13 URL form)", got)
	}
	if len(claims.Scope) != 2 || claims.Scope[0] != "chat:relay" || claims.Scope[1] != "models:read" {
		t.Fatalf("Scope = %v, want [chat:relay models:read]", claims.Scope)
	}
	if claims.V != 1 {
		t.Fatalf("V = %d, want 1", claims.V)
	}
	if claims.Issuer != "https://api.kittypaw.app/auth" {
		t.Fatalf("Issuer = %q, want https://api.kittypaw.app/auth (RFC 7519 iss, Plan 13 URL form)", claims.Issuer)
	}
}

// TestClaimsJSONUsesSubField verifies the JSON wire shape uses RFC 7519
// "sub" (not legacy "uid"). Cross-service (kittychat) MUST be able to
// read the standard sub claim without any uid-fallback hack.
func TestClaimsJSONUsesSubField(t *testing.T) {
	token, err := auth.SignForAudiences(
		"user-xyz",
		[]string{"https://api.kittypaw.app"},
		nil,
		testSecret,
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf("SignForAudiences: %v", err)
	}
	// Decode the middle (payload) segment without verification.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := raw["sub"].(string); !ok || got != "user-xyz" {
		t.Fatalf(`payload "sub" = %v, want "user-xyz"`, raw["sub"])
	}
	if _, ok := raw["uid"]; ok {
		t.Fatalf(`payload must not contain legacy "uid" key, got: %v`, raw)
	}
	if got, ok := raw["iss"].(string); !ok || got != "https://api.kittypaw.app/auth" {
		t.Fatalf(`payload "iss" = %v, want "https://api.kittypaw.app/auth"`, raw["iss"])
	}
}

// Tokens issued via the bare Sign helper (no audiences/scopes) must verify
// successfully — the only difference from SignForAudiences is empty aud/scope.
// They still use the standard "sub" claim. This is NOT a uid-fallback.
func TestVerify_TokenWithoutAudOrScope(t *testing.T) {
	token, err := auth.Sign("user-bare", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := auth.Verify(token, testSecret)
	if err != nil {
		t.Fatalf("verify bare: %v", err)
	}
	if claims.UserID != "user-bare" {
		t.Fatalf("UserID = %q", claims.UserID)
	}
	if len(claims.Audience) != 0 {
		t.Fatalf("bare token Audience must be empty, got %v", claims.Audience)
	}
	if len(claims.Scope) != 0 {
		t.Fatalf("bare token Scope must be empty, got %v", claims.Scope)
	}
}

// Pin the contract: tokens minted with the legacy "uid" JSON tag (no "sub")
// MUST be rejected. There is no uid-fallback. The verifier reads the
// standard sub claim only.
func TestVerify_RejectsLegacyUIDOnlyToken(t *testing.T) {
	legacy := struct {
		LegacyUID string `json:"uid"`
		jwt.RegisteredClaims
	}{
		LegacyUID: "user-old",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, legacy)
	signed, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign legacy: %v", err)
	}
	if _, err := auth.Verify(signed, testSecret); err == nil {
		t.Fatal("expected Verify to reject token with only uid (no sub)")
	}
}
