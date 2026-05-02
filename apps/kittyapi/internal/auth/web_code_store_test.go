package auth_test

import (
	"strings"
	"testing"

	"github.com/kittypaw-app/kittyapi/internal/auth"
)

// TestWebCodeStore_CreateConsume pins the round-trip: Create returns
// a non-empty code, Consume returns the exact entry, and the code is
// removed (one-time use).
func TestWebCodeStore_CreateConsume(t *testing.T) {
	s := auth.NewWebCodeStore()
	t.Cleanup(s.Close)

	entry := auth.WebCodeEntry{
		UserID:        "user-1",
		RedirectURI:   "https://chat.kittypaw.app/auth/callback",
		CodeChallenge: "abc123",
	}
	code, err := s.Create(entry)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if code == "" {
		t.Fatal("expected non-empty code")
	}

	got, err := s.Consume(code)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if got.UserID != entry.UserID || got.RedirectURI != entry.RedirectURI || got.CodeChallenge != entry.CodeChallenge {
		t.Fatalf("Consume returned %+v, want %+v", got, entry)
	}
}

// TestWebCodeStore_OneTimeUse pins the contract that a second Consume
// on the same code MUST fail — protects against replay attacks where
// an attacker who steals the code from a server log can't reuse it.
func TestWebCodeStore_OneTimeUse(t *testing.T) {
	s := auth.NewWebCodeStore()
	t.Cleanup(s.Close)

	code, _ := s.Create(auth.WebCodeEntry{UserID: "u"})

	if _, err := s.Consume(code); err != nil {
		t.Fatalf("first Consume: %v", err)
	}
	_, err := s.Consume(code)
	if err == nil {
		t.Fatal("second Consume must fail (one-time use)")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown-code error, got %v", err)
	}
}

// TestWebCodeStore_UnknownCode pins the silent-failure contract for
// guessing attempts: a fabricated code returns "unknown" not panic.
func TestWebCodeStore_UnknownCode(t *testing.T) {
	s := auth.NewWebCodeStore()
	t.Cleanup(s.Close)

	_, err := s.Consume("not-a-real-code")
	if err == nil {
		t.Fatal("expected error for unknown code")
	}
}
