package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// SkillFormat distinguishes between skill packaging standards.
type SkillFormat string

const (
	SkillFormatNative SkillFormat = "native"  // .skill.toml + .js
	SkillFormatMd     SkillFormat = "skillmd" // SKILL.md from agentskills.io
)

// ModelTier classifies the LLM tier a skill requires.
type ModelTier string

const (
	ModelTierAutomation ModelTier = "automation"
	ModelTierAnalysis   ModelTier = "analysis"
)

// Skill represents a learnable, executable skill.
type Skill struct {
	Name        string           `toml:"name"        json:"name"`
	Version     uint32           `toml:"version"     json:"version"`
	Description string           `toml:"description" json:"description"`
	CreatedAt   string           `toml:"created_at"  json:"created_at"`
	UpdatedAt   string           `toml:"updated_at"  json:"updated_at"`
	Enabled     bool             `toml:"enabled"     json:"enabled"`
	Trigger     SkillTrigger     `toml:"trigger"     json:"trigger"`
	Permissions SkillPermissions `toml:"permissions" json:"permissions"`
	Format      SkillFormat      `toml:"format"      json:"format"`
	ModelTier   *ModelTier       `toml:"model_tier"  json:"model_tier,omitempty"`
}

// SkillTrigger defines how a skill is activated.
type SkillTrigger struct {
	Type    string `toml:"type"    json:"type"`
	Cron    string `toml:"cron"    json:"cron,omitempty"`
	Natural string `toml:"natural" json:"natural,omitempty"`
	Keyword string `toml:"keyword" json:"keyword,omitempty"`
	RunAt   string `toml:"run_at"  json:"run_at,omitempty"` // RFC 3339 UTC
}

// SkillPermissions declares what a skill is allowed to do.
type SkillPermissions struct {
	Primitives   []string `toml:"primitives"    json:"primitives"`
	AllowedHosts []string `toml:"allowed_hosts" json:"allowed_hosts"`
}

// SkillsDir returns the directory where skills are stored, creating it if needed.
func SkillsDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return "", err
	}
	return skillsDir, nil
}

// SaveSkill writes a skill definition and its JavaScript code to disk.
func SaveSkill(skill *Skill, jsCode string) error {
	if err := ValidateSkillName(skill.Name); err != nil {
		return err
	}

	dir, err := SkillsDir()
	if err != nil {
		return err
	}

	skillDir := filepath.Join(dir, skill.Name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}

	// Archive current version if it exists
	tomlPath := filepath.Join(skillDir, skill.Name+".skill.toml")
	if _, err := os.Stat(tomlPath); err == nil {
		archiveDir := filepath.Join(skillDir, "archive")
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			return err
		}
		stamp := time.Now().Format("20060102-150405")
		_ = copyFile(tomlPath, filepath.Join(archiveDir, fmt.Sprintf("%s.v%d.skill.toml", stamp, skill.Version-1)))
		jsPath := filepath.Join(skillDir, skill.Name+".js")
		_ = copyFile(jsPath, filepath.Join(archiveDir, fmt.Sprintf("%s.v%d.js", stamp, skill.Version-1)))
	}

	// Write TOML
	skill.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if skill.CreatedAt == "" {
		skill.CreatedAt = skill.UpdatedAt
	}

	f, err := os.Create(tomlPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(skill); err != nil {
		return err
	}

	// Write JS
	jsPath := filepath.Join(skillDir, skill.Name+".js")
	return os.WriteFile(jsPath, []byte(jsCode), 0o644)
}

// LoadSkill loads a single skill by name. Returns nil, nil if not found.
func LoadSkill(name string) (*Skill, string, error) {
	dir, err := SkillsDir()
	if err != nil {
		return nil, "", err
	}
	return loadSkillFrom(filepath.Join(dir, name), name)
}

// LoadAllSkills loads all skills from the skills directory.
func LoadAllSkills() ([]SkillWithCode, error) {
	dir, err := SkillsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []SkillWithCode
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skill, code, err := loadSkillFrom(filepath.Join(dir, entry.Name()), entry.Name())
		if err != nil || skill == nil {
			continue
		}
		skills = append(skills, SkillWithCode{Skill: *skill, Code: code})
	}
	return skills, nil
}

// SkillWithCode bundles a Skill with its JavaScript source.
type SkillWithCode struct {
	Skill Skill
	Code  string
}

// DisableSkill sets enabled=false for a skill on disk.
func DisableSkill(name string) error {
	skill, code, err := LoadSkill(name)
	if err != nil {
		return err
	}
	if skill == nil {
		return fmt.Errorf("skill %q not found", name)
	}
	skill.Enabled = false
	return SaveSkill(skill, code)
}

// DeleteSkill removes a skill directory entirely.
func DeleteSkill(name string) error {
	dir, err := SkillsDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(dir, name))
}

// RollbackSkill restores the most recent archived version of a skill.
func RollbackSkill(name string) error {
	dir, err := SkillsDir()
	if err != nil {
		return err
	}

	archiveDir := filepath.Join(dir, name, "archive")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return fmt.Errorf("no archive for skill %q", name)
	}

	// Find latest TOML archive
	var latestToml, latestJs string
	for i := len(entries) - 1; i >= 0; i-- {
		n := entries[i].Name()
		if strings.HasSuffix(n, ".skill.toml") && latestToml == "" {
			latestToml = filepath.Join(archiveDir, n)
		}
		if strings.HasSuffix(n, ".js") && latestJs == "" {
			latestJs = filepath.Join(archiveDir, n)
		}
		if latestToml != "" && latestJs != "" {
			break
		}
	}

	if latestToml == "" {
		return fmt.Errorf("no archived version found for skill %q", name)
	}

	skillDir := filepath.Join(dir, name)
	if err := copyFile(latestToml, filepath.Join(skillDir, name+".skill.toml")); err != nil {
		return err
	}
	if latestJs != "" {
		return copyFile(latestJs, filepath.Join(skillDir, name+".js"))
	}
	return nil
}

// MatchTrigger checks if an event text activates a skill's keyword trigger.
func MatchTrigger(skill *Skill, eventText string) bool {
	if skill.Trigger.Keyword == "" {
		return false
	}
	return strings.Contains(
		strings.ToLower(eventText),
		strings.ToLower(skill.Trigger.Keyword),
	)
}

func loadSkillFrom(skillDir, name string) (*Skill, string, error) {
	tomlPath := filepath.Join(skillDir, name+".skill.toml")
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}

	var skill Skill
	if err := toml.Unmarshal(data, &skill); err != nil {
		return nil, "", fmt.Errorf("parse skill %q: %w", name, err)
	}

	jsPath := filepath.Join(skillDir, name+".js")
	jsData, err := os.ReadFile(jsPath)
	if err != nil {
		jsData = nil // Skill without JS is valid (metadata only)
	}

	return &skill, string(jsData), nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
