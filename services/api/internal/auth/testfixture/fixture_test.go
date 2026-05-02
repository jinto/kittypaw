package testfixture_test

import (
	"testing"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/auth/testfixture"
	"github.com/kittypaw-app/kittyapi/internal/config"
)

func TestIssueTestJWT_RoundTrip(t *testing.T) {
	cfg := config.LoadForTest()
	provider := auth.NewSingleKeyProvider(&cfg.JWTPrivateKey.PublicKey, cfg.JWTKID)

	token := testfixture.IssueTestJWT(t, cfg.JWTPrivateKey, cfg.JWTKID, "user-abc", 0)
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	claims, err := auth.Verify(token, provider, auth.AudienceAPI)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.UserID != "user-abc" {
		t.Fatalf("expected uid=user-abc, got %q", claims.UserID)
	}
}

func TestIssueTestJWT_DefaultTTL(t *testing.T) {
	cfg := config.LoadForTest()
	provider := auth.NewSingleKeyProvider(&cfg.JWTPrivateKey.PublicKey, cfg.JWTKID)

	token := testfixture.IssueTestJWT(t, cfg.JWTPrivateKey, cfg.JWTKID, "uid", 0)
	claims, err := auth.Verify(token, provider, auth.AudienceAPI)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	remaining := time.Until(claims.ExpiresAt.Time)
	if remaining < 14*time.Minute || remaining > 15*time.Minute+time.Second {
		t.Fatalf("default TTL out of 14-15min window: %v", remaining)
	}
}

func TestIssueTestJWT_CustomTTL(t *testing.T) {
	cfg := config.LoadForTest()
	provider := auth.NewSingleKeyProvider(&cfg.JWTPrivateKey.PublicKey, cfg.JWTKID)

	token := testfixture.IssueTestJWT(t, cfg.JWTPrivateKey, cfg.JWTKID, "uid", 5*time.Minute)
	claims, err := auth.Verify(token, provider, auth.AudienceAPI)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	remaining := time.Until(claims.ExpiresAt.Time)
	if remaining < 4*time.Minute || remaining > 5*time.Minute+time.Second {
		t.Fatalf("custom TTL 5min out of window: %v", remaining)
	}
}
