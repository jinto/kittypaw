package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProfile_MissingSoul(t *testing.T) {
	base := t.TempDir()
	// No SOUL.md exists — should fallback to default preset, no error.
	p, err := LoadProfile(base, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != "default" {
		t.Errorf("ID = %q, want %q", p.ID, "default")
	}
	if p.Soul == "" {
		t.Error("expected fallback preset Soul, got empty")
	}
	// Should match the default-assistant preset.
	preset := Presets["default-assistant"]
	if p.Soul != preset.Soul {
		t.Errorf("Soul = %q, want default-assistant preset", p.Soul)
	}
}

func TestLoadProfile_ExistingSoul(t *testing.T) {
	base := t.TempDir()
	profDir := filepath.Join(base, "profiles", "mybot")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	soul := "I am a custom bot with special powers."
	if err := os.WriteFile(filepath.Join(profDir, "SOUL.md"), []byte(soul), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadProfile(base, "mybot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Soul != soul {
		t.Errorf("Soul = %q, want %q", p.Soul, soul)
	}
	if p.UserMD != "" {
		t.Errorf("UserMD should be empty, got %q", p.UserMD)
	}
}

func TestLoadProfile_WithUserMD(t *testing.T) {
	base := t.TempDir()
	profDir := filepath.Join(base, "profiles", "bot")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "SOUL.md"), []byte("soul text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "USER.md"), []byte("user likes cats"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadProfile(base, "bot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Soul != "soul text" {
		t.Errorf("Soul = %q", p.Soul)
	}
	if p.UserMD != "user likes cats" {
		t.Errorf("UserMD = %q", p.UserMD)
	}
}

func TestLoadProfile_InvalidID(t *testing.T) {
	base := t.TempDir()
	_, err := LoadProfile(base, "../evil")
	if err == nil {
		t.Fatal("expected error for invalid profile ID")
	}
}

func TestEnsureDefaultProfile_CreatesDir(t *testing.T) {
	base := t.TempDir()
	if err := EnsureDefaultProfile(base); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soulPath := filepath.Join(base, "profiles", "default", "SOUL.md")
	data, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatalf("SOUL.md not created: %v", err)
	}
	if len(data) == 0 {
		t.Error("SOUL.md is empty")
	}
	// Should contain the default-assistant preset content.
	if string(data) != Presets["default-assistant"].Soul {
		t.Error("SOUL.md content does not match default-assistant preset")
	}
}

func TestEnsureDefaultProfile_Idempotent(t *testing.T) {
	base := t.TempDir()
	if err := EnsureDefaultProfile(base); err != nil {
		t.Fatal(err)
	}

	// Write custom content to SOUL.md.
	soulPath := filepath.Join(base, "profiles", "default", "SOUL.md")
	custom := "My custom persona"
	if err := os.WriteFile(soulPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second call should NOT overwrite.
	if err := EnsureDefaultProfile(base); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != custom {
		t.Errorf("SOUL.md was overwritten: got %q, want %q", string(data), custom)
	}
}

// --- T2: ApplyPreset / DetectDirty / PresetStatus ---

func TestApplyPreset(t *testing.T) {
	base := t.TempDir()
	if err := ApplyPreset(base, "mybot", "friendly-assistant"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SOUL.md should match the preset.
	soulPath := filepath.Join(base, "profiles", "mybot", "SOUL.md")
	data, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatalf("SOUL.md not created: %v", err)
	}
	if string(data) != Presets["friendly-assistant"].Soul {
		t.Error("SOUL.md content does not match friendly-assistant preset")
	}

	// .preset_meta should exist.
	metaPath := filepath.Join(base, "profiles", "mybot", ".preset_meta")
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf(".preset_meta not created: %v", err)
	}
}

func TestApplyPreset_InvalidPreset(t *testing.T) {
	base := t.TempDir()
	err := ApplyPreset(base, "mybot", "nonexistent-preset")
	if err == nil {
		t.Fatal("expected error for unknown preset ID")
	}
}

func TestDetectDirty_Clean(t *testing.T) {
	base := t.TempDir()
	if err := ApplyPreset(base, "bot", "default-assistant"); err != nil {
		t.Fatal(err)
	}
	dirty, err := DetectDirty(base, "bot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dirty {
		t.Error("expected clean (not dirty) after fresh apply")
	}
}

func TestDetectDirty_Modified(t *testing.T) {
	base := t.TempDir()
	if err := ApplyPreset(base, "bot", "default-assistant"); err != nil {
		t.Fatal(err)
	}

	// Modify SOUL.md.
	soulPath := filepath.Join(base, "profiles", "bot", "SOUL.md")
	if err := os.WriteFile(soulPath, []byte("modified soul"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirty, err := DetectDirty(base, "bot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dirty {
		t.Error("expected dirty after modification")
	}
}

func TestPresetStatus_Preset(t *testing.T) {
	base := t.TempDir()
	if err := ApplyPreset(base, "bot", "professional-assistant"); err != nil {
		t.Fatal(err)
	}
	status := PresetStatus(base, "bot")
	if status.Kind != StatusPreset {
		t.Errorf("Kind = %v, want StatusPreset", status.Kind)
	}
	if status.PresetID != "professional-assistant" {
		t.Errorf("PresetID = %q, want %q", status.PresetID, "professional-assistant")
	}
}

func TestPresetStatus_Custom(t *testing.T) {
	base := t.TempDir()
	if err := ApplyPreset(base, "bot", "default-assistant"); err != nil {
		t.Fatal(err)
	}
	// Modify SOUL.md.
	soulPath := filepath.Join(base, "profiles", "bot", "SOUL.md")
	if err := os.WriteFile(soulPath, []byte("custom persona"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := PresetStatus(base, "bot")
	if status.Kind != StatusCustom {
		t.Errorf("Kind = %v, want StatusCustom", status.Kind)
	}
	if status.PresetID != "default-assistant" {
		t.Errorf("PresetID = %q, want %q (original preset)", status.PresetID, "default-assistant")
	}
}

func TestPresetStatus_Unknown(t *testing.T) {
	base := t.TempDir()
	// Create a profile without .preset_meta.
	profDir := filepath.Join(base, "profiles", "manual")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "SOUL.md"), []byte("hand-written"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := PresetStatus(base, "manual")
	if status.Kind != StatusUnknown {
		t.Errorf("Kind = %v, want StatusUnknown", status.Kind)
	}
}

func TestPresets_NonEmpty(t *testing.T) {
	expected := []string{"default-assistant", "friendly-assistant", "professional-assistant"}
	for _, id := range expected {
		p, ok := Presets[id]
		if !ok {
			t.Errorf("preset %q not found", id)
			continue
		}
		if p.Soul == "" {
			t.Errorf("preset %q has empty Soul", id)
		}
		if p.Name == "" {
			t.Errorf("preset %q has empty Name", id)
		}
	}
}
