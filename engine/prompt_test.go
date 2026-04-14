package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
	mcpreg "github.com/jinto/kittypaw/mcp"
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

func TestBuildMCPToolsSection(t *testing.T) {
	tools := map[string][]mcpreg.ToolInfo{
		"browser": {
			{Name: "run_session", Description: "Run a browser session"},
			{Name: "get_result", Description: "Get session result"},
		},
		"filesystem": {
			{Name: "read_file", Description: "Read a file"},
		},
	}
	section := BuildMCPToolsSection(tools)
	if !strings.HasPrefix(section, "## MCP Tools") {
		t.Error("missing ## MCP Tools header")
	}
	if !strings.Contains(section, "### browser") {
		t.Error("missing ### browser section")
	}
	if !strings.Contains(section, "### filesystem") {
		t.Error("missing ### filesystem section")
	}
	if !strings.Contains(section, "- run_session: Run a browser session") {
		t.Error("missing run_session tool line")
	}
	if !strings.Contains(section, "- read_file: Read a file") {
		t.Error("missing read_file tool line")
	}
}

func TestBuildMCPToolsSectionEmpty(t *testing.T) {
	if got := BuildMCPToolsSection(nil); got != "" {
		t.Errorf("expected empty string for nil, got %q", got)
	}
	if got := BuildMCPToolsSection(map[string][]mcpreg.ToolInfo{}); got != "" {
		t.Errorf("expected empty string for empty map, got %q", got)
	}
}

func TestBuildMCPToolsSectionSorted(t *testing.T) {
	tools := map[string][]mcpreg.ToolInfo{
		"zebra": {{Name: "z_tool", Description: "Z"}},
		"alpha": {{Name: "a_tool", Description: "A"}},
		"mid":   {{Name: "m_tool", Description: "M"}},
	}
	section := BuildMCPToolsSection(tools)
	alphaIdx := strings.Index(section, "### alpha")
	midIdx := strings.Index(section, "### mid")
	zebraIdx := strings.Index(section, "### zebra")
	if alphaIdx >= midIdx || midIdx >= zebraIdx {
		t.Errorf("servers not in alpha order: alpha=%d, mid=%d, zebra=%d", alphaIdx, midIdx, zebraIdx)
	}
}

func TestBuildMCPToolsSectionCap(t *testing.T) {
	// Create many tools that exceed 2000 chars
	tools := map[string][]mcpreg.ToolInfo{}
	for i := 0; i < 100; i++ {
		tools["server"] = append(tools["server"], mcpreg.ToolInfo{
			Name:        fmt.Sprintf("tool_%03d", i),
			Description: "A moderately long description for testing the budget cap",
		})
	}
	section := BuildMCPToolsSection(tools)
	if len(section) > 2100 { // allow small overhead for omitted message
		t.Errorf("section too long: %d chars", len(section))
	}
	if !strings.Contains(section, "more tools omitted") {
		t.Error("expected truncation message")
	}
}

// --- BuildPrompt with Profile ---

func TestBuildPrompt_WithSoul(t *testing.T) {
	state := &core.AgentState{AgentID: "test", SystemPrompt: SystemPrompt}
	profile := &core.Profile{ID: "mybot", Soul: "I am a cheerful assistant."}
	msgs := BuildPrompt(state, "hello", CompactionConfig{RecentWindow: 5}, &core.Config{}, "telegram", profile, "", "")

	sys := msgs[0].Content
	if !strings.Contains(sys, "## Your Identity (SOUL.md)") {
		t.Error("missing SOUL.md header in system prompt")
	}
	if !strings.Contains(sys, "I am a cheerful assistant.") {
		t.Error("soul content not injected")
	}
}

func TestBuildPrompt_WithNickAndUserMD(t *testing.T) {
	state := &core.AgentState{AgentID: "test", SystemPrompt: SystemPrompt}
	profile := &core.Profile{
		ID:     "bot",
		Nick:   "Paw",
		Soul:   "soul",
		UserMD: "User likes hiking.",
	}
	msgs := BuildPrompt(state, "hi", CompactionConfig{RecentWindow: 5}, &core.Config{}, "slack", profile, "", "")

	sys := msgs[0].Content
	if !strings.Contains(sys, "Your name/nickname is: Paw") {
		t.Error("nick not injected")
	}
	if !strings.Contains(sys, "## User Profile (USER.md)") {
		t.Error("missing USER.md header")
	}
	if !strings.Contains(sys, "User likes hiking.") {
		t.Error("user md content not injected")
	}
}

func TestBuildPrompt_NilProfile(t *testing.T) {
	state := &core.AgentState{AgentID: "test", SystemPrompt: SystemPrompt}
	msgs := BuildPrompt(state, "hey", CompactionConfig{RecentWindow: 5}, &core.Config{}, "web", nil, "", "")

	sys := msgs[0].Content
	if strings.Contains(sys, "## Your Identity") {
		t.Error("nil profile should not inject identity section")
	}
}
