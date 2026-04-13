package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Profile holds the loaded persona data for a single profile.
type Profile struct {
	ID     string // profile directory name
	Nick   string // display name (from config, set by caller)
	Soul   string // SOUL.md content
	UserMD string // USER.md content (optional)
}

// PresetInfo describes a built-in persona preset.
type PresetInfo struct {
	ID          string
	Name        string
	Description string
	Soul        string
}

// Presets contains the built-in persona presets.
var Presets = map[string]PresetInfo{
	"default-assistant": {
		ID:          "default-assistant",
		Name:        "Default Assistant",
		Description: "간결하고 정확한 비서",
		Soul: `You are a concise and accurate assistant.

## Personality
- Direct and clear in communication
- Focus on facts and actionable information
- Respond in the same language the user uses
- Keep responses brief unless asked for detail

## Style
- Use plain language, avoid jargon unless the user uses it
- Structure complex answers with bullet points
- Acknowledge uncertainty honestly`,
	},
	"friendly-assistant": {
		ID:          "friendly-assistant",
		Name:        "Friendly Assistant",
		Description: "따뜻하고 캐주얼한 비서",
		Soul: `You are a warm and casual assistant.

## Personality
- Friendly and approachable tone
- Use conversational language
- Show enthusiasm when helping
- Respond in the same language the user uses

## Style
- Feel free to use light humor when appropriate
- Express empathy and encouragement
- Ask follow-up questions to better understand needs
- Celebrate wins with the user`,
	},
	"professional-assistant": {
		ID:          "professional-assistant",
		Name:        "Professional Assistant",
		Description: "격식 있고 분석적인 비서",
		Soul: `You are a formal and analytical assistant.

## Personality
- Professional and measured tone
- Prioritize accuracy and thoroughness
- Present multiple perspectives on complex topics
- Respond in the same language the user uses

## Style
- Use structured formats: numbered lists, tables, headers
- Cite sources and evidence when available
- Provide risk assessments and trade-off analyses
- Maintain objectivity in recommendations`,
	},
}

// LoadProfile reads a profile's SOUL.md and USER.md from disk.
// If SOUL.md is missing, falls back to the default-assistant preset with a warning log.
// Returns an error only for invalid profile IDs.
func LoadProfile(base, name string) (*Profile, error) {
	if err := ValidateProfileID(name); err != nil {
		return nil, fmt.Errorf("load profile: %w", err)
	}

	p := &Profile{ID: name}
	profDir := filepath.Join(base, "profiles", name)

	soulData, err := os.ReadFile(filepath.Join(profDir, "SOUL.md"))
	if err != nil {
		slog.Warn("SOUL.md not found, using default preset",
			"profile", name, "path", profDir)
		p.Soul = Presets["default-assistant"].Soul
	} else {
		p.Soul = string(soulData)
	}

	if userData, err := os.ReadFile(filepath.Join(profDir, "USER.md")); err == nil {
		p.UserMD = string(userData)
	}

	return p, nil
}

// EnsureDefaultProfile creates the default profile directory and SOUL.md
// if they don't already exist. Existing files are never overwritten.
func EnsureDefaultProfile(base string) error {
	profDir := filepath.Join(base, "profiles", "default")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		return fmt.Errorf("create default profile dir: %w", err)
	}

	soulPath := filepath.Join(profDir, "SOUL.md")
	if _, err := os.Stat(soulPath); err == nil {
		return nil // already exists, don't overwrite
	}

	preset := Presets["default-assistant"]
	if err := os.WriteFile(soulPath, []byte(preset.Soul), 0o644); err != nil {
		return fmt.Errorf("write default SOUL.md: %w", err)
	}
	return nil
}

// presetMeta is stored as .preset_meta JSON alongside SOUL.md.
type presetMeta struct {
	PresetID string `json:"preset_id"`
	Hash     string `json:"hash"`
}

// ApplyPreset writes a preset's SOUL.md and records the hash in .preset_meta.
func ApplyPreset(base, profileName, presetID string) error {
	preset, ok := Presets[presetID]
	if !ok {
		return fmt.Errorf("unknown preset ID: %q", presetID)
	}
	if err := ValidateProfileID(profileName); err != nil {
		return err
	}

	profDir := filepath.Join(base, "profiles", profileName)
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}

	// Write SOUL.md.
	soulPath := filepath.Join(profDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte(preset.Soul), 0o644); err != nil {
		return fmt.Errorf("write SOUL.md: %w", err)
	}

	// Write .preset_meta with hash of the SOUL.md content.
	h := sha256.Sum256([]byte(preset.Soul))
	meta := presetMeta{
		PresetID: presetID,
		Hash:     hex.EncodeToString(h[:]),
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal preset meta: %w", err)
	}
	if err := os.WriteFile(filepath.Join(profDir, ".preset_meta"), metaData, 0o644); err != nil {
		return fmt.Errorf("write .preset_meta: %w", err)
	}
	return nil
}

// DetectDirty reports whether SOUL.md has been modified since the last ApplyPreset.
// Returns false if there's no .preset_meta (no baseline to compare).
func DetectDirty(base, profileName string) (bool, error) {
	status := PresetStatus(base, profileName)
	return status.Kind == StatusCustom, nil
}

// PresetStatusKind describes the preset state of a profile.
type PresetStatusKind int

const (
	StatusPreset  PresetStatusKind = iota // matches a preset exactly
	StatusCustom                          // modified from a preset
	StatusUnknown                         // no preset metadata
)

// PresetStatusResult holds the result of PresetStatus.
type PresetStatusResult struct {
	Kind     PresetStatusKind
	PresetID string // set for StatusPreset and StatusCustom
}

// PresetStatus determines whether a profile's SOUL.md matches its original preset.
func PresetStatus(base, profileName string) PresetStatusResult {
	profDir := filepath.Join(base, "profiles", profileName)

	metaData, err := os.ReadFile(filepath.Join(profDir, ".preset_meta"))
	if err != nil {
		return PresetStatusResult{Kind: StatusUnknown}
	}
	var meta presetMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return PresetStatusResult{Kind: StatusUnknown}
	}

	soulData, err := os.ReadFile(filepath.Join(profDir, "SOUL.md"))
	if err != nil {
		return PresetStatusResult{Kind: StatusCustom, PresetID: meta.PresetID}
	}

	h := sha256.Sum256(soulData)
	currentHash := hex.EncodeToString(h[:])
	if currentHash == meta.Hash {
		return PresetStatusResult{Kind: StatusPreset, PresetID: meta.PresetID}
	}
	return PresetStatusResult{Kind: StatusCustom, PresetID: meta.PresetID}
}
