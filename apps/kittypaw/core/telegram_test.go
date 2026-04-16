package core

import "testing"

func TestValidateTelegramToken_Valid(t *testing.T) {
	valid := []string{
		"123456:ABCDefghijklmnopqrstuvwxyz012345",
		"7890123456:abcdefghijklmnopqrstuvwxyz_0123456789-abc",
	}
	for _, tok := range valid {
		if !ValidateTelegramToken(tok) {
			t.Errorf("ValidateTelegramToken(%q) = false, want true", tok)
		}
	}
}

func TestValidateTelegramToken_Invalid(t *testing.T) {
	invalid := []string{
		"",
		"not-a-token",
		"123:short",                            // too short after colon
		"abc:ABCDefghijklmnopqrstuvwxyz012345", // non-digit prefix
		":ABCDefghijklmnopqrstuvwxyz012345",    // missing digits
	}
	for _, tok := range invalid {
		if ValidateTelegramToken(tok) {
			t.Errorf("ValidateTelegramToken(%q) = true, want false", tok)
		}
	}
}
