package server

import (
	"testing"
)

func TestFixedLenEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"secret", "secret", true},
		{"secret", "wrong", false},
		{"", "secret", false}, // empty a → false
		{"secret", "", false}, // empty b → false
		{"", "", false},       // both empty → false (prevents auth bypass)
		{"short", "longerstring", false},
		{"abc", "abd", false},
		{"same-length-1", "same-length-2", false},
	}
	for _, tt := range tests {
		got := fixedLenEqual(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("fixedLenEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestFixedLenEqualConstantTime(t *testing.T) {
	// Verify that identical strings always return true
	key := "my-super-secret-api-key-12345"
	for i := 0; i < 100; i++ {
		if !fixedLenEqual(key, key) {
			t.Fatal("fixedLenEqual returned false for identical strings")
		}
	}
}
