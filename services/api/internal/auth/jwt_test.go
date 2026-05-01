package auth_test

import (
	"testing"
	"time"

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
		[]string{"kittyapi", "kittychat"},
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
	if got := []string(claims.Audience); len(got) != 2 || got[0] != "kittyapi" || got[1] != "kittychat" {
		t.Fatalf("Audience = %v, want [kittyapi kittychat]", got)
	}
	if len(claims.Scope) != 2 || claims.Scope[0] != "chat:relay" || claims.Scope[1] != "models:read" {
		t.Fatalf("Scope = %v, want [chat:relay models:read]", claims.Scope)
	}
	if claims.V != 1 {
		t.Fatalf("V = %d, want 1", claims.V)
	}
}

// BC: legacy tokens issued via Sign (no aud/scope/v) must still verify.
func TestVerify_LegacyTokenWithoutAudOrScope(t *testing.T) {
	token, err := auth.Sign("user-legacy", testSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := auth.Verify(token, testSecret)
	if err != nil {
		t.Fatalf("verify legacy: %v", err)
	}
	if claims.UserID != "user-legacy" {
		t.Fatalf("UserID = %q", claims.UserID)
	}
	if len(claims.Audience) != 0 {
		t.Fatalf("legacy Audience must be empty, got %v", claims.Audience)
	}
	if len(claims.Scope) != 0 {
		t.Fatalf("legacy Scope must be empty, got %v", claims.Scope)
	}
}
