package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinto/gopaw/core"
)

func TestBuildSkillsSection(t *testing.T) {
	section := buildSkillsSection(nil)

	// Must start with the header
	if !strings.HasPrefix(section, "## Available skill globals") {
		t.Error("buildSkillsSection missing header")
	}

	// Must contain every skill from the registry
	for _, skill := range core.SkillRegistry {
		for _, m := range skill.Methods {
			if !strings.Contains(section, m.Signature) {
				t.Errorf("buildSkillsSection missing signature: %s", m.Signature)
			}
		}
	}

	// Must contain console.log
	if !strings.Contains(section, "console.log") {
		t.Error("buildSkillsSection missing console.log")
	}

	// Must be deterministic
	section2 := buildSkillsSection(nil)
	if section != section2 {
		t.Error("buildSkillsSection is not deterministic")
	}
}

func TestParseAtMention(t *testing.T) {
	tests := []struct {
		text      string
		wantID    string
		wantRest  string
		wantMatch bool
	}{
		{"@bot hello", "bot", "hello", true},
		{"@my-agent do something", "my-agent", "do something", true},
		{"@agent_1", "agent_1", "", true},
		{"hello @bot", "", "hello @bot", false},    // not at start
		{"@", "", "@", false},                       // bare @
		{"", "", "", false},                         // empty
		{"no mention", "", "no mention", false},     // no @
		{"@inv@lid rest", "", "@inv@lid rest", false}, // invalid char in ID
		{"@CamelCase text", "CamelCase", "text", true},
		{"  @spaced text", "spaced", "text", true},  // leading whitespace
	}
	for _, tt := range tests {
		id, rest, ok := ParseAtMention(tt.text)
		if id != tt.wantID || rest != tt.wantRest || ok != tt.wantMatch {
			t.Errorf("ParseAtMention(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.text, id, rest, ok, tt.wantID, tt.wantRest, tt.wantMatch)
		}
	}
}

func TestFormatEvent(t *testing.T) {
	payload := core.ChatPayload{Text: "hello world"}
	raw, _ := json.Marshal(payload)
	event := &core.Event{Type: core.EventWebChat, Payload: raw}

	got := FormatEvent(event)
	if got != "hello world" {
		t.Errorf("FormatEvent() = %q, want %q", got, "hello world")
	}
}

func TestFormatExecResult(t *testing.T) {
	tests := []struct {
		result *core.ExecutionResult
		want   string
	}{
		{&core.ExecutionResult{Success: true, Output: "42"}, "output: 42"},
		{&core.ExecutionResult{Success: false, Error: "boom"}, "error: boom"},
	}
	for _, tt := range tests {
		got := FormatExecResult(tt.result)
		if got != tt.want {
			t.Errorf("FormatExecResult(%+v) = %q, want %q", tt.result, got, tt.want)
		}
	}
}
