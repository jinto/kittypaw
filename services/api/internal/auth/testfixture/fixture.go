package testfixture

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kittypaw-app/kittyapi/internal/auth"
	"github.com/kittypaw-app/kittyapi/internal/model"
)

const defaultTTL = 15 * time.Minute

func IssueTestJWT(t *testing.T, secret, userID string, ttl time.Duration) string {
	t.Helper()
	if ttl == 0 {
		ttl = defaultTTL
	}
	token, err := auth.Sign(userID, secret, ttl)
	if err != nil {
		t.Fatalf("testfixture.IssueTestJWT: %v", err)
	}
	return token
}

func SeedTestUser(t *testing.T, store model.UserStore) *model.User {
	t.Helper()
	providerID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	email := providerID + "@example.com"
	user, err := store.CreateOrUpdate(context.Background(), "google", providerID, email, "Test User", "")
	if err != nil {
		t.Fatalf("testfixture.SeedTestUser: %v", err)
	}
	return user
}
