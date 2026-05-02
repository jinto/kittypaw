package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestSlashPersonaSwitchesAccountConversationProfile(t *testing.T) {
	st := openTestStore(t)
	if err := st.UpsertProfileMeta("finance", "재무담당 비서", "[]", "test"); err != nil {
		t.Fatalf("seed profile meta: %v", err)
	}
	cfg := core.DefaultConfig()
	sess := &Session{
		Store:     st,
		Config:    &cfg,
		AccountID: "alice",
	}

	out, handled := tryHandleCommand(context.Background(), "/persona finance", sess)
	if !handled {
		t.Fatal("/persona command was not handled")
	}
	if !strings.Contains(out, "finance") {
		t.Fatalf("response should mention selected profile, got %q", out)
	}
	if got, ok, err := st.GetUserContext("active_profile:alice"); err != nil || !ok || got != "finance" {
		t.Fatalf("active_profile:alice = %q ok=%v err=%v, want finance", got, ok, err)
	}
}
