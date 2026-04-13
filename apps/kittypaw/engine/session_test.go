package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinto/gopaw/core"
	"github.com/jinto/gopaw/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestResolveProfileName_MentionOverride(t *testing.T) {
	cfg := core.DefaultConfig()
	st := openTestStore(t)
	got := ResolveProfileName(&cfg, "telegram", "user-1", "english-teacher", st)
	if got != "english-teacher" {
		t.Errorf("got %q, want %q", got, "english-teacher")
	}
}

func TestResolveProfileName_SessionOverride(t *testing.T) {
	cfg := core.DefaultConfig()
	st := openTestStore(t)
	// Set active_profile for this agent.
	if err := st.SetUserContext("active_profile:user-1", "custom-bot", "agent"); err != nil {
		t.Fatal(err)
	}
	got := ResolveProfileName(&cfg, "telegram", "user-1", "", st)
	if got != "custom-bot" {
		t.Errorf("got %q, want %q", got, "custom-bot")
	}
}

func TestResolveProfileName_ChannelBinding(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.Profiles = []core.ProfileConfig{
		{ID: "tg-bot", Nick: "TG", Channels: []string{"telegram"}},
		{ID: "slack-bot", Nick: "SL", Channels: []string{"slack"}},
	}
	st := openTestStore(t)
	got := ResolveProfileName(&cfg, "telegram", "user-1", "", st)
	if got != "tg-bot" {
		t.Errorf("got %q, want %q", got, "tg-bot")
	}
}

func TestResolveProfileName_Default(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.DefaultProfile = "my-default"
	st := openTestStore(t)
	got := ResolveProfileName(&cfg, "web", "user-1", "", st)
	if got != "my-default" {
		t.Errorf("got %q, want %q", got, "my-default")
	}
}

func TestResolveProfileName_NilStore(t *testing.T) {
	cfg := core.DefaultConfig()
	// nil store should not panic, just skip session override.
	got := ResolveProfileName(&cfg, "web", "user-1", "", nil)
	if got != cfg.DefaultProfile {
		t.Errorf("got %q, want %q", got, cfg.DefaultProfile)
	}
}

// --- T5: Profile.switch integration ---

func TestProfileSwitch_SetsContext(t *testing.T) {
	st := openTestStore(t)

	// Create a profile directory so LoadProfile succeeds.
	base := t.TempDir()
	profDir := filepath.Join(base, "profiles", "new-persona")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "SOUL.md"), []byte("test soul"), 0o644); err != nil {
		t.Fatal(err)
	}

	// We can't easily call executeProfile directly without ConfigDir pointing to
	// our temp dir, so test the store round-trip that Profile.switch performs.
	agentID := "user-42"
	key := fmt.Sprintf("active_profile:%s", agentID)
	if err := st.SetUserContext(key, "new-persona", "agent"); err != nil {
		t.Fatal(err)
	}

	// ResolveProfileName should pick up the session override.
	cfg := core.DefaultConfig()
	got := ResolveProfileName(&cfg, "web", agentID, "", st)
	if got != "new-persona" {
		t.Errorf("got %q, want %q", got, "new-persona")
	}
}

func TestProfileSwitch_OverriddenByMention(t *testing.T) {
	st := openTestStore(t)
	agentID := "user-42"
	key := fmt.Sprintf("active_profile:%s", agentID)
	if err := st.SetUserContext(key, "session-profile", "agent"); err != nil {
		t.Fatal(err)
	}

	cfg := core.DefaultConfig()
	// @mention should win over session override.
	got := ResolveProfileName(&cfg, "web", agentID, "mention-profile", st)
	if got != "mention-profile" {
		t.Errorf("got %q, want %q", got, "mention-profile")
	}
}
