package core

import (
	"strings"
	"testing"
)

func TestParseSkillMd(t *testing.T) {
	input := `---
name: my-skill
description: Does something useful
permissions:
  - Http
  - Storage
trigger:
  type: keyword
  keyword: do-thing
---

# Instructions

Do something useful for the user.
`
	meta, body, err := ParseSkillMd([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMd: %v", err)
	}
	if meta.Name != "my-skill" {
		t.Errorf("Name = %q, want %q", meta.Name, "my-skill")
	}
	if meta.Description != "Does something useful" {
		t.Errorf("Description = %q", meta.Description)
	}
	if len(meta.Permissions) != 2 {
		t.Fatalf("Permissions len = %d, want 2", len(meta.Permissions))
	}
	if meta.Permissions[0] != "Http" || meta.Permissions[1] != "Storage" {
		t.Errorf("Permissions = %v", meta.Permissions)
	}
	if meta.Trigger.Type != "keyword" {
		t.Errorf("Trigger.Type = %q", meta.Trigger.Type)
	}
	if meta.Trigger.Keyword != "do-thing" {
		t.Errorf("Trigger.Keyword = %q", meta.Trigger.Keyword)
	}
	if !strings.Contains(body, "# Instructions") {
		t.Errorf("body should contain markdown content, got %q", body)
	}
	if !strings.Contains(body, "Do something useful") {
		t.Errorf("body should contain instructions, got %q", body)
	}
}

func TestParseSkillMdNoFrontmatter(t *testing.T) {
	_, _, err := ParseSkillMd([]byte("# Just markdown\nNo frontmatter here"))
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParseSkillMdEmptyPermissions(t *testing.T) {
	input := `---
name: minimal-skill
description: minimal
---

Body here.
`
	meta, body, err := ParseSkillMd([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMd: %v", err)
	}
	if meta.Name != "minimal-skill" {
		t.Errorf("Name = %q", meta.Name)
	}
	if len(meta.Permissions) != 0 {
		t.Errorf("Permissions should be empty, got %v", meta.Permissions)
	}
	if !strings.Contains(body, "Body here") {
		t.Errorf("body = %q", body)
	}
}

func TestParseSkillMdScheduleTrigger(t *testing.T) {
	input := `---
name: cron-skill
description: runs on schedule
trigger:
  type: schedule
  cron: "*/5 * * * *"
---

Run every 5 minutes.
`
	meta, _, err := ParseSkillMd([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMd: %v", err)
	}
	if meta.Trigger.Type != "schedule" {
		t.Errorf("Trigger.Type = %q, want schedule", meta.Trigger.Type)
	}
	if meta.Trigger.Cron != "*/5 * * * *" {
		t.Errorf("Trigger.Cron = %q", meta.Trigger.Cron)
	}
}

func TestParseSkillMdEmptyBody(t *testing.T) {
	input := `---
name: empty-body
description: test
---
`
	meta, body, err := ParseSkillMd([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMd: %v", err)
	}
	if meta.Name != "empty-body" {
		t.Errorf("Name = %q", meta.Name)
	}
	if strings.TrimSpace(body) != "" {
		t.Errorf("body should be empty, got %q", body)
	}
}

func TestParseSkillMdMissingName(t *testing.T) {
	input := `---
description: no name
---

Body.
`
	_, _, err := ParseSkillMd([]byte(input))
	if err == nil {
		t.Error("expected error for missing name")
	}
}
