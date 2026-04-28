package server

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateTurnID_EmptyAllowed(t *testing.T) {
	if msg, ok := validateTurnID(""); !ok || msg != "" {
		t.Errorf("empty turn_id should pass (legacy fallback): got msg=%q ok=%v", msg, ok)
	}
}

func TestValidateTurnID_AcceptsUUID(t *testing.T) {
	id := uuid.NewString()
	if msg, ok := validateTurnID(id); !ok || msg != "" {
		t.Errorf("uuid %q should pass: got msg=%q ok=%v", id, msg, ok)
	}
}

func TestValidateTurnID_RejectsOverLength(t *testing.T) {
	long := strings.Repeat("a", maxTurnIDLen+1)
	msg, ok := validateTurnID(long)
	if ok {
		t.Fatal("over-length id should fail")
	}
	if !strings.Contains(msg, "length") {
		t.Errorf("error msg should mention length: %q", msg)
	}
}

func TestValidateTurnID_RejectsNonUUID(t *testing.T) {
	cases := []string{
		"1",
		"abc",
		"not-a-uuid-at-all",
		"00000000-0000-0000-0000-00000000000",  // 35 chars (one short)
		"00000000-0000-0000-0000-000000000g00", // bad hex
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			msg, ok := validateTurnID(id)
			if ok {
				t.Errorf("non-UUID %q should fail validation", id)
			}
			if !strings.Contains(msg, "UUID") {
				t.Errorf("error msg should mention UUID: %q", msg)
			}
		})
	}
}
