package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"testing"

	"github.com/kittypaw-app/kittyportal/internal/auth"
)

// rfc7638ExampleN is the modulus from RFC 7638 §3.1 example. Pinning the
// thumbprint of this exact key proves our Thumbprint() implementation
// matches the canonical-JSON spec (alphabetical keys, no whitespace).
const rfc7638ExampleN = "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw"

// rfc7638ExampleThumbprint is the value the RFC computes for that key.
// It is the entire reason this test exists — any drift in our canonical
// JSON ordering or hashing will surface here.
const rfc7638ExampleThumbprint = "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs"

func TestThumbprint_RFC7638_Example(t *testing.T) {
	jwk := auth.JWK{
		Kty: "RSA",
		N:   rfc7638ExampleN,
		E:   "AQAB",
	}
	got := auth.Thumbprint(jwk)
	if got != rfc7638ExampleThumbprint {
		t.Fatalf("thumbprint mismatch (RFC 7638 §3.1 example):\n got=%s\nwant=%s", got, rfc7638ExampleThumbprint)
	}
}

// TestBuildJWKSet_ModulusPadding pins RFC 7518 §6.3.1.1: the JWK n
// modulus byte length must equal (BitLen+7)/8. big.Int.Bytes() strips
// leading zeros — without explicit padding, every ~256th key would
// silently produce a verifier-incompatible JWK.
func TestBuildJWKSet_ModulusPadding(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	jwks := auth.BuildJWKSet(&key.PublicKey, "test-kid")
	if len(jwks.Keys) != 1 {
		t.Fatalf("expected 1 key in set, got %d", len(jwks.Keys))
	}
	jwk := jwks.Keys[0]

	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		t.Fatalf("decode N: %v", err)
	}
	want := (key.N.BitLen() + 7) / 8
	if len(nBytes) != want {
		t.Fatalf("modulus byte len = %d, want %d (BitLen=%d)", len(nBytes), want, key.N.BitLen())
	}

	if jwk.Kid != "test-kid" {
		t.Fatalf("kid = %q, want %q", jwk.Kid, "test-kid")
	}
	if jwk.Alg != "RS256" {
		t.Fatalf("alg = %q, want RS256", jwk.Alg)
	}
	if jwk.Use != "sig" {
		t.Fatalf("use = %q, want sig", jwk.Use)
	}
}

func TestLoadPrivateKeyPEM_Invalid(t *testing.T) {
	_, _, err := auth.LoadPrivateKeyPEM([]byte("not a valid pem"))
	if err == nil {
		t.Fatal("expected error for non-PEM input, got nil")
	}
}

// TestSingleKeyProvider_Lookup pins the JWKSProvider contract: known kid
// returns the public key, unknown kid returns an error. Multi-key
// rotation will land behind the same interface.
func TestSingleKeyProvider_Lookup(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	provider := auth.NewSingleKeyProvider(&key.PublicKey, "the-kid")

	pub, err := provider.Lookup("the-kid")
	if err != nil {
		t.Fatalf("Lookup(the-kid): %v", err)
	}
	if pub.N.Cmp(key.N) != 0 {
		t.Fatal("Lookup returned wrong public key")
	}

	if _, err := provider.Lookup("unknown"); err == nil {
		t.Fatal("Lookup(unknown) returned nil error, want non-nil")
	}
}
