package engine

import (
	"testing"
	"time"

	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/store"
)

func newAutoFixStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// ---------------------------------------------------------------------------
// Store-level fix_attempts methods
// ---------------------------------------------------------------------------

func TestClaimFixAttempt(t *testing.T) {
	st := newAutoFixStore(t)

	// Seed the skill_schedule row via SetLastRun (creates the row).
	if err := st.SetLastRun("sk-claim", time.Now()); err != nil {
		t.Fatal(err)
	}

	// First claim should succeed.
	ok, err := st.ClaimFixAttempt("sk-claim", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("first claim should succeed")
	}

	// Second claim should succeed.
	ok, err = st.ClaimFixAttempt("sk-claim", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("second claim should succeed")
	}

	// Third claim should fail (max 2).
	ok, err = st.ClaimFixAttempt("sk-claim", 2)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("third claim should fail (max=2)")
	}
}

func TestResetFixAttempts(t *testing.T) {
	st := newAutoFixStore(t)
	if err := st.SetLastRun("sk-reset", time.Now()); err != nil {
		t.Fatal(err)
	}
	st.ClaimFixAttempt("sk-reset", 2)
	st.ClaimFixAttempt("sk-reset", 2)

	if err := st.ResetFixAttempts("sk-reset"); err != nil {
		t.Fatal(err)
	}

	// Should be able to claim again after reset.
	ok, err := st.ClaimFixAttempt("sk-reset", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("claim after reset should succeed")
	}
}

func TestGetFixAttempts(t *testing.T) {
	st := newAutoFixStore(t)

	// Non-existent skill returns 0.
	n, err := st.GetFixAttempts("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("got %d, want 0 for nonexistent skill", n)
	}

	// After claiming.
	st.SetLastRun("sk-get", time.Now())
	st.ClaimFixAttempt("sk-get", 5)
	n, err = st.GetFixAttempts("sk-get")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// buildFixPrompt
// ---------------------------------------------------------------------------

func TestBuildFixPrompt(t *testing.T) {
	prompt := buildFixPrompt("nil pointer dereference", "const x = null;\nx.foo();")
	if len(prompt) == 0 {
		t.Fatal("prompt should not be empty")
	}
	// Should contain both error and code.
	if !containsAll(prompt, "nil pointer dereference", "const x = null") {
		t.Error("prompt missing error or code context")
	}
}

// ---------------------------------------------------------------------------
// ApplyAutoFix — autonomy gate
// ---------------------------------------------------------------------------

func TestApplyAutoFix_ReadonlyBlocked(t *testing.T) {
	st := newAutoFixStore(t)
	s := &Session{
		Store:  st,
		Config: &core.Config{AutonomyLevel: core.AutonomyReadonly},
	}
	result := &TeachResult{SyntaxOK: true, SkillName: "test"}
	err := ApplyAutoFix(s, "test", result, "err", "old")
	if err == nil {
		t.Fatal("expected error for readonly mode")
	}
}

func TestApplyAutoFix_SyntaxErrorRejected(t *testing.T) {
	st := newAutoFixStore(t)
	s := &Session{
		Store:  st,
		Config: &core.Config{AutonomyLevel: core.AutonomyFull},
	}
	result := &TeachResult{SyntaxOK: false, SyntaxError: "unexpected token", SkillName: "test"}
	err := ApplyAutoFix(s, "test", result, "err", "old")
	if err == nil {
		t.Fatal("expected error for syntax failure")
	}
}

func TestApplyAutoFix_SupervisedStoresOnly(t *testing.T) {
	st := newAutoFixStore(t)
	s := &Session{
		Store:  st,
		Config: &core.Config{AutonomyLevel: core.AutonomySupervised},
	}
	result := &TeachResult{
		SyntaxOK:  true,
		SkillName: "supervised-skill",
		Code:      "return 'fixed';",
	}

	err := ApplyAutoFix(s, "supervised-skill", result, "error msg", "old code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be stored in DB but not applied.
	fixes, err := st.ListFixes("supervised-skill")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixes) != 1 {
		t.Fatalf("expected 1 fix, got %d", len(fixes))
	}
	if fixes[0].Applied {
		t.Error("supervised fix should NOT be applied")
	}
}

// ---------------------------------------------------------------------------
// estimateFixTokens
// ---------------------------------------------------------------------------

func TestEstimateFixTokens(t *testing.T) {
	tokens := estimateFixTokens("hello world", "fixed code")
	if tokens == 0 {
		t.Fatal("estimate should be > 0")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
