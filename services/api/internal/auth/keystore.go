package auth

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
)

// JWK is an RSA public key in JWK form (RFC 7517).
//
// Field order matters for human readability but NOT for thumbprint —
// Thumbprint() rebuilds a canonical JSON representation independently
// of struct tag order, per RFC 7638 §3.
type JWK struct {
	Kty string `json:"kty"`           // RSA
	Use string `json:"use,omitempty"` // sig
	Alg string `json:"alg,omitempty"` // RS256
	Kid string `json:"kid,omitempty"`
	N   string `json:"n"` // base64url modulus, leading-zero padded (RFC 7518 §6.3.1.1)
	E   string `json:"e"` // base64url public exponent
}

// JWKSet is a JWK Set (RFC 7517 §5).
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// LoadPrivateKeyPEM parses a PEM-encoded RSA private key and returns it
// alongside its kid (RFC 7638 thumbprint of the corresponding public
// key). Accepts both PKCS#8 ("PRIVATE KEY") and PKCS#1 ("RSA PRIVATE
// KEY") PEM blocks — `openssl genrsa` ships PKCS#1 by default.
func LoadPrivateKeyPEM(pemBytes []byte) (*rsa.PrivateKey, string, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, "", errors.New("invalid PEM data")
	}

	var (
		parsed any
		err    error
	)
	switch block.Type {
	case "PRIVATE KEY":
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		parsed, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, "", fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
	if err != nil {
		return nil, "", fmt.Errorf("parse private key: %w", err)
	}

	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, "", errors.New("PEM block is not an RSA key")
	}

	kid := Thumbprint(canonicalPubJWK(&rsaKey.PublicKey))
	return rsaKey, kid, nil
}

// BuildJWKSet returns a JWK Set publishing pub under kid. Use/Alg are
// pinned to sig/RS256 — this server only signs RS256.
func BuildJWKSet(pub *rsa.PublicKey, kid string) JWKSet {
	j := canonicalPubJWK(pub)
	j.Use = "sig"
	j.Alg = "RS256"
	j.Kid = kid
	return JWKSet{Keys: []JWK{j}}
}

// canonicalPubJWK returns just kty/n/e — the three fields that feed
// into the thumbprint per RFC 7638 §3.2.
func canonicalPubJWK(pub *rsa.PublicKey) JWK {
	return JWK{
		Kty: "RSA",
		N:   encodeBigIntPadded(pub.N),
		E:   encodeBigIntPadded(big.NewInt(int64(pub.E))),
	}
}

// encodeBigIntPadded base64url-encodes i so its byte representation has
// length (BitLen+7)/8. big.Int.Bytes() strips leading zeros, but JWK
// (RFC 7518 §6.3.1.1) requires preserving them — without padding,
// roughly 1 in 256 keys would produce a wire format that some verifier
// libraries reject.
func encodeBigIntPadded(i *big.Int) string {
	raw := i.Bytes()
	expected := (i.BitLen() + 7) / 8
	if expected == 0 {
		expected = 1
	}
	if len(raw) < expected {
		padded := make([]byte, expected)
		copy(padded[expected-len(raw):], raw)
		raw = padded
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Thumbprint returns the RFC 7638 §3.1 thumbprint of an RSA JWK.
//
// The canonical JSON form used as input is exactly:
//
//	{"e":"<E>","kty":"RSA","n":"<N>"}
//
// — alphabetical key order, no whitespace, only the three required
// members. Encoding/json with a struct field order is fragile (Go's
// encoder honors tag order, not alphabetical), so we build the string
// manually. The result is SHA-256 → base64url (no padding).
//
// **Caller contract**: j.E and j.N MUST be base64url-encoded (the
// alphabet is `[A-Za-z0-9_-]`, no `"` or backslash). canonicalPubJWK
// and BuildJWKSet satisfy this; do NOT pass arbitrary user-controlled
// strings without that guarantee — the manual concat would otherwise
// allow JSON injection.
func Thumbprint(j JWK) string {
	canonical := `{"e":"` + j.E + `","kty":"RSA","n":"` + j.N + `"}`
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// JWKSProvider abstracts kid → public key lookup for token verification
// AND publishes the active JWK Set for /.well-known/jwks.json. The same
// interface will accept a multi-key rotation backend without callers
// changing.
type JWKSProvider interface {
	Lookup(kid string) (*rsa.PublicKey, error)
	JWKSet() JWKSet
}

// NewSingleKeyProvider builds a provider backed by exactly one key. kid
// should be the RFC 7638 thumbprint computed at load time.
func NewSingleKeyProvider(pub *rsa.PublicKey, kid string) JWKSProvider {
	return &singleKeyProvider{
		pub: pub,
		kid: kid,
		set: BuildJWKSet(pub, kid),
	}
}

type singleKeyProvider struct {
	pub *rsa.PublicKey
	kid string
	set JWKSet
}

func (p *singleKeyProvider) Lookup(kid string) (*rsa.PublicKey, error) {
	if kid != p.kid {
		return nil, fmt.Errorf("unknown kid: %s", kid)
	}
	return p.pub, nil
}

func (p *singleKeyProvider) JWKSet() JWKSet {
	return p.set
}
