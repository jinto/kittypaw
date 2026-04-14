package core

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SkillMdMeta holds metadata parsed from a SKILL.md frontmatter.
type SkillMdMeta struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Permissions []string     `yaml:"permissions"`
	Trigger     SkillTrigger `yaml:"trigger"`
}

// ParseSkillMd splits a SKILL.md file into its YAML frontmatter and markdown body.
// The frontmatter must be delimited by "---" lines. Returns an error if the
// frontmatter is missing or the name field is empty.
func ParseSkillMd(data []byte) (*SkillMdMeta, string, error) {
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, "", err
	}

	var meta SkillMdMeta
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return nil, "", fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}

	if meta.Name == "" {
		return nil, "", fmt.Errorf("SKILL.md frontmatter: name is required")
	}

	return &meta, body, nil
}

// splitFrontmatter extracts the YAML frontmatter block from data.
// Expects the format: ---\n<yaml>\n---\n<body>
func splitFrontmatter(data []byte) (frontmatter []byte, body string, err error) {
	const sep = "---"

	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte(sep)) {
		return nil, "", fmt.Errorf("SKILL.md must start with --- frontmatter delimiter")
	}

	// Skip the opening "---" and newline.
	rest := trimmed[len(sep):]
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	} else if len(rest) > 1 && rest[0] == '\r' && rest[1] == '\n' {
		rest = rest[2:]
	}

	// Find the closing "---".
	idx := bytes.Index(rest, []byte("\n"+sep))
	if idx < 0 {
		return nil, "", fmt.Errorf("SKILL.md frontmatter: missing closing --- delimiter")
	}

	fm := rest[:idx]
	after := rest[idx+1+len(sep):]

	// Skip the newline after closing "---".
	if len(after) > 0 && after[0] == '\n' {
		after = after[1:]
	} else if len(after) > 1 && after[0] == '\r' && after[1] == '\n' {
		after = after[2:]
	}

	return fm, string(after), nil
}
