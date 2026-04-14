package engine

import (
	"strings"
	"testing"

	"github.com/jinto/gopaw/core"
)

func TestFilterSkillsByPermissions(t *testing.T) {
	allowed := []string{"Http", "Storage"}
	filtered := FilterSkillsByPermissions(core.SkillRegistry, allowed)

	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2", len(filtered))
	}

	names := make(map[string]bool)
	for _, s := range filtered {
		names[s.Name] = true
	}
	if !names["Http"] {
		t.Error("Http should be in filtered list")
	}
	if !names["Storage"] {
		t.Error("Storage should be in filtered list")
	}
	if names["File"] {
		t.Error("File should NOT be in filtered list")
	}
}

func TestFilterSkillsByPermissionsEmpty(t *testing.T) {
	filtered := FilterSkillsByPermissions(core.SkillRegistry, nil)
	if len(filtered) != 0 {
		t.Errorf("nil permissions should return empty, got %d", len(filtered))
	}

	filtered = FilterSkillsByPermissions(core.SkillRegistry, []string{})
	if len(filtered) != 0 {
		t.Errorf("empty permissions should return empty, got %d", len(filtered))
	}
}

func TestFilterSkillsByPermissionsAll(t *testing.T) {
	var all []string
	for _, s := range core.SkillRegistry {
		all = append(all, s.Name)
	}
	filtered := FilterSkillsByPermissions(core.SkillRegistry, all)
	if len(filtered) != len(core.SkillRegistry) {
		t.Errorf("all permissions should return all skills, got %d/%d", len(filtered), len(core.SkillRegistry))
	}
}

func TestBuildPromptModeSystemPrompt(t *testing.T) {
	skill := &core.Skill{
		Name:        "test-skill",
		Format:      core.SkillFormatMd,
		Permissions: core.SkillPermissions{Primitives: []string{"Http", "Storage"}},
	}
	body := "You are a helpful assistant that fetches weather data."

	prompt := BuildPromptModeSystemPrompt(skill, body)

	if !strings.Contains(prompt, "weather data") {
		t.Error("prompt should contain SKILL.md body")
	}
	if !strings.Contains(prompt, "Http") {
		t.Error("prompt should mention allowed skills")
	}
	if !strings.Contains(prompt, "Storage") {
		t.Error("prompt should mention allowed skills")
	}
	if strings.Contains(prompt, "File") {
		t.Error("prompt should NOT mention disallowed skills")
	}
}

func TestIsPromptModeSkill(t *testing.T) {
	native := &core.Skill{Format: core.SkillFormatNative}
	md := &core.Skill{Format: core.SkillFormatMd}

	if IsPromptModeSkill(native) {
		t.Error("native skill should not be prompt mode")
	}
	if !IsPromptModeSkill(md) {
		t.Error("skillmd skill should be prompt mode")
	}
}
