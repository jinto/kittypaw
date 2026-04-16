package auth_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/jinto/kittypaw-api/internal/auth"
)

func TestGenerateVerifier(t *testing.T) {
	v, err := auth.GenerateVerifier()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(v) < 43 {
		t.Fatalf("verifier too short: %d chars", len(v))
	}

	// Must be URL-safe base64 (no padding, no + or /).
	if strings.ContainsAny(v, "+/=") {
		t.Fatalf("verifier contains non-URL-safe chars: %q", v)
	}
}

func TestGenerateVerifierUniqueness(t *testing.T) {
	a, _ := auth.GenerateVerifier()
	b, _ := auth.GenerateVerifier()
	if a == b {
		t.Fatal("two verifiers should not be equal")
	}
}

func TestChallengeS256(t *testing.T) {
	challenge := auth.ChallengeS256("test-verifier")

	// Must be valid base64url without padding.
	if strings.ContainsAny(challenge, "+/=") {
		t.Fatalf("challenge contains non-URL-safe chars: %q", challenge)
	}

	// Must decode to 32 bytes (SHA-256 output).
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(decoded))
	}
}

func TestChallengeS256Deterministic(t *testing.T) {
	a := auth.ChallengeS256("same-input")
	b := auth.ChallengeS256("same-input")
	if a != b {
		t.Fatal("same input should produce same challenge")
	}
}
