package connect

import (
	"strings"
	"testing"
	"time"
)

func TestCodeStoreConsumeOnce(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	store := NewCodeStore(CodeStoreOptions{
		TTL:        time.Minute,
		MaxEntries: 10,
		Now:        func() time.Time { return now },
	})

	tokens := TokenSet{
		Provider:     "gmail",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "openid email profile https://www.googleapis.com/auth/gmail.readonly",
		Email:        "alice@example.com",
		IssuedAt:     now,
	}

	code, err := store.Create(tokens)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code == "" {
		t.Fatal("code is empty")
	}

	got, err := store.Consume(code)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if got != tokens {
		t.Fatalf("tokens = %#v, want %#v", got, tokens)
	}

	if _, err := store.Consume(code); err == nil {
		t.Fatal("second Consume succeeded, want one-time code")
	}
}

func TestCodeStoreRejectsExpiredCode(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	store := NewCodeStore(CodeStoreOptions{
		TTL:        time.Minute,
		MaxEntries: 10,
		Now:        func() time.Time { return now },
	})
	code, err := store.Create(TokenSet{Provider: "gmail", AccessToken: "access-1", IssuedAt: now})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := store.Consume(code); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Consume expired err = %v, want expired error", err)
	}
}

func TestCodeStoreBounded(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	store := NewCodeStore(CodeStoreOptions{
		TTL:        time.Minute,
		MaxEntries: 2,
		Now:        func() time.Time { return now },
	})
	if _, err := store.Create(TokenSet{Provider: "gmail", AccessToken: "access-1", IssuedAt: now}); err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	if _, err := store.Create(TokenSet{Provider: "gmail", AccessToken: "access-2", IssuedAt: now}); err != nil {
		t.Fatalf("Create 2: %v", err)
	}
	if _, err := store.Create(TokenSet{Provider: "gmail", AccessToken: "access-3", IssuedAt: now}); err == nil {
		t.Fatal("Create 3 succeeded, want bounded store error")
	}
}

func TestCodeStoreEvictsExpiredBeforeBoundCheck(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	store := NewCodeStore(CodeStoreOptions{
		TTL:        time.Minute,
		MaxEntries: 1,
		Now:        func() time.Time { return now },
	})
	if _, err := store.Create(TokenSet{Provider: "gmail", AccessToken: "old", IssuedAt: now}); err != nil {
		t.Fatalf("Create old: %v", err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := store.Create(TokenSet{Provider: "gmail", AccessToken: "new", IssuedAt: now}); err != nil {
		t.Fatalf("Create after expiry: %v", err)
	}
}
