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
