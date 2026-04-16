package auth

import (
	"strings"
	"testing"
	"time"
)

func TestCLICodeStore_CreateConsume(t *testing.T) {
	s := NewCLICodeStore()
	defer s.Close()

	tokens := TokenResponse{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		ExpiresIn:    900,
	}

	code, err := s.Create(tokens)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Code should be in XXXX-YYYY format.
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("unexpected code format: %q", code)
	}

	got, err := s.Consume(code)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if got.AccessToken != "access-123" || got.RefreshToken != "refresh-456" {
		t.Errorf("token mismatch: got %+v", got)
	}
}

func TestCLICodeStore_ConsumeNormalization(t *testing.T) {
	s := NewCLICodeStore()
	defer s.Close()

	tokens := TokenResponse{AccessToken: "tok"}
	code, _ := s.Create(tokens)

	// Remove dash and lowercase — should still work.
	raw := strings.ReplaceAll(code, "-", "")
	lower := strings.ToLower(raw)

	got, err := s.Consume(lower)
	if err != nil {
		t.Fatalf("Consume with lowercase: %v", err)
	}
	if got.AccessToken != "tok" {
		t.Errorf("unexpected token: %v", got.AccessToken)
	}
}

func TestCLICodeStore_OneTimeUse(t *testing.T) {
	s := NewCLICodeStore()
	defer s.Close()

	tokens := TokenResponse{AccessToken: "tok"}
	code, _ := s.Create(tokens)

	if _, err := s.Consume(code); err != nil {
		t.Fatalf("first Consume: %v", err)
	}
	if _, err := s.Consume(code); err == nil {
		t.Error("second Consume should fail")
	}
}

func TestCLICodeStore_Expired(t *testing.T) {
	s := NewCLICodeStore()
	defer s.Close()

	tokens := TokenResponse{AccessToken: "tok"}
	code, _ := s.Create(tokens)

	// Manually expire the entry.
	raw := strings.ToUpper(strings.ReplaceAll(code, "-", ""))
	s.mu.Lock()
	entry := s.entries[raw]
	entry.createdAt = time.Now().Add(-6 * time.Minute)
	s.entries[raw] = entry
	s.mu.Unlock()

	if _, err := s.Consume(code); err == nil {
		t.Error("expected expired error")
	}
}

func TestCLICodeStore_UnknownCode(t *testing.T) {
	s := NewCLICodeStore()
	defer s.Close()

	if _, err := s.Consume("XXXX-YYYY"); err == nil {
		t.Error("expected error for unknown code")
	}
}

func TestGenerateCLICode_Format(t *testing.T) {
	for i := 0; i < 100; i++ {
		code, err := generateCLICode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != cliCodeLength {
			t.Errorf("code length %d, want %d", len(code), cliCodeLength)
		}
		for _, c := range code {
			if !strings.ContainsRune(cliCodeCharset, c) {
				t.Errorf("code %q contains invalid char %c", code, c)
			}
		}
	}
}
