package server

import (
	"path/filepath"
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/store"
)

// AC-DB: isOnboardingCompleted must treat a configured LLM key as "onboarded"
// even when the DB flag is missing. `kittypaw setup` (CLI) writes config.toml
// but deliberately does NOT set user_context.onboarding_completed — that flag
// belongs to the Web onboarding path. If this fallback regresses, CLI users
// will see Web setup screens re-open even after a successful `kittypaw setup`,
// and the G3 drop decision in the spec breaks. Either the fallback stays OR
// the CLI starts writing the flag; either fix must update this test.
func TestIsOnboardingCompleted_FallbackToLLMKey(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "kittypaw.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Sanity check: DB flag must NOT be set — this is the CLI-setup baseline.
	if _, ok, _ := st.GetUserContext("onboarding_completed"); ok {
		t.Fatal("fresh DB should not have onboarding_completed — test premise broken")
	}

	cfg := core.DefaultConfig()
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.APIKey = "sk-test"
	cfg.LLM.Model = "claude-test"

	srv := &Server{
		config: &cfg,
		store:  st,
	}
	if !srv.isOnboardingCompleted() {
		t.Fatal("isOnboardingCompleted()=false with LLM.APIKey set — CLI parity broken")
	}

	// Sibling contract: no API key AND no DB flag → genuinely not onboarded.
	cfg.LLM.APIKey = ""
	cfg.LLM.BaseURL = ""
	if srv.isOnboardingCompleted() {
		t.Fatal("isOnboardingCompleted()=true with no DB flag and no LLM key — fallback is too loose")
	}
}
