package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/kittypaw-app/kittyapi/internal/auth"
)

// MinJWTKeyBits is the lower bound for RSA signing keys. Below this is
// not considered safe for production token signing in the present era.
const MinJWTKeyBits = 2048

type Config struct {
	Port               string
	DatabaseURL        string
	JWTPrivateKey      *rsa.PrivateKey // RS256 signing key (Plan 21 PR-B cutover; replaces HS256 secret)
	JWTKID             string          // RFC 7638 thumbprint of JWTPrivateKey's public half
	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
	BaseURL            string
	AllowedOrigins     []string
	AirKoreaAPIKey     string
	HolidayAPIKey      string
	WeatherAPIKey      string
	KakaoRelayURL      string
	ChatRelayURL       string
	APIBaseURL         string
	SkillsRegistryURL  string
}

func Load() (*Config, error) {
	c := &Config{
		Port:               env("PORT", "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		BaseURL:            env("BASE_URL", "http://localhost:8080"),
		AirKoreaAPIKey:     os.Getenv("AIRKOREA_API_KEY"),
		HolidayAPIKey:      os.Getenv("HOLIDAY_API_KEY"),
		WeatherAPIKey:      os.Getenv("WEATHER_API_KEY"),
		KakaoRelayURL:      os.Getenv("KAKAO_RELAY_URL"),
		ChatRelayURL:       os.Getenv("CHAT_RELAY_URL"),
		APIBaseURL:         os.Getenv("API_BASE_URL"),
		SkillsRegistryURL:  env("SKILLS_REGISTRY_URL", "https://github.com/kittypaw-app/skills"),
	}

	required := map[string]string{
		"DATABASE_URL": c.DatabaseURL,
	}
	for name, val := range required {
		if val == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}

	// Plan 21 PR-B: RS256 cutover complete — HS256 JWT_SECRET removed.
	// Token signing/verification flows entirely through JWT_PRIVATE_KEY_PEM_B64
	// (PR-A) + the JWKS endpoint. The RSA key is required at startup so an
	// undecodable env can't survive into request time.
	//
	// Encoding contract: standard base64 (RFC 4648 §4 — `+/` alphabet
	// with padding). NOT URL-safe (`-_`). Mismatch at deploy time
	// surfaces here as "illegal base64 data" — see deploy/env docs.
	keyB64 := os.Getenv("JWT_PRIVATE_KEY_PEM_B64")
	if keyB64 == "" {
		return nil, fmt.Errorf("JWT_PRIVATE_KEY_PEM_B64 is required")
	}
	pemBytes, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("decode JWT_PRIVATE_KEY_PEM_B64 (base64): %w", err)
	}
	priv, kid, err := auth.LoadPrivateKeyPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("load RSA private key: %w", err)
	}
	if priv.N.BitLen() < MinJWTKeyBits {
		return nil, fmt.Errorf("RSA key must be at least %d bits, got %d", MinJWTKeyBits, priv.N.BitLen())
	}
	c.JWTPrivateKey = priv
	c.JWTKID = kid

	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		c.AllowedOrigins = strings.Split(origins, ",")
	} else {
		c.AllowedOrigins = []string{c.BaseURL}
	}

	return c, nil
}

// LoadForTest returns a config suitable for testing (no required fields).
// The shared fixture key is cached via sync.Once below so handlers that
// publish/use JWKS work without each test wiring its own key. Cost of
// 2048-bit generation is ~50ms; reusing across the suite keeps tests
// snappy and lets thumbprint pin the kid in assertions.
//
// The cache lives in this production file rather than a *_test.go
// because LoadForTest itself is a public API consumed by tests in
// other packages — Go does not link _test.go symbols across packages.
func LoadForTest() *Config {
	priv, kid := loadForTestKey()
	return &Config{
		Port:           env("PORT", "8080"),
		BaseURL:        env("BASE_URL", "http://localhost:8080"),
		AllowedOrigins: []string{"http://localhost:8080"},
		JWTPrivateKey:  priv,
		JWTKID:         kid,
	}
}

var (
	loadForTestKeyOnce sync.Once
	loadForTestKeyPriv *rsa.PrivateKey
	loadForTestKeyKID  string
)

func loadForTestKey() (*rsa.PrivateKey, string) {
	loadForTestKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(fmt.Sprintf("LoadForTest: rsa.GenerateKey: %v", err))
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			panic(fmt.Sprintf("LoadForTest: marshal PKCS8: %v", err))
		}
		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		_, kid, err := auth.LoadPrivateKeyPEM(pemBytes)
		if err != nil {
			panic(fmt.Sprintf("LoadForTest: load PEM: %v", err))
		}
		loadForTestKeyPriv = key
		loadForTestKeyKID = kid
	})
	return loadForTestKeyPriv, loadForTestKeyKID
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
