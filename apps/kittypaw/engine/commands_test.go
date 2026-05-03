package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/sandbox"
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

func TestSlashRunExecutesInstalledSkill(t *testing.T) {
	baseDir := t.TempDir()
	cfg := core.DefaultConfig()
	sess := &Session{
		BaseDir: baseDir,
		Config:  &cfg,
		Sandbox: sandbox.New(cfg.Sandbox),
	}
	if err := core.SaveSkillTo(baseDir, &core.Skill{
		Name:        "hello",
		Description: "test skill",
		Enabled:     true,
	}, `return "hello from skill"`); err != nil {
		t.Fatalf("save skill: %v", err)
	}

	out, handled := tryHandleCommand(context.Background(), "/run hello", sess)
	if !handled {
		t.Fatal("/run command was not handled")
	}
	if out != "hello from skill" {
		t.Fatalf("/run output = %q, want skill output", out)
	}
	if strings.Contains(out, "실행 요청됨") {
		t.Fatalf("/run returned a queued/requested message instead of executing: %q", out)
	}
}
