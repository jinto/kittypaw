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

// --- Block constants ---

func TestBlockConstants_NonEmpty(t *testing.T) {
	blocks := map[string]string{
		"IdentityBlock":      IdentityBlock,
		"ExecutionBlock":     ExecutionBlock,
		"QualityBlock":       QualityBlock,
		"SkillCreationBlock": SkillCreationBlock,
		"MemoryBlock":        MemoryBlock,
	}
	for name, block := range blocks {
		if len(strings.TrimSpace(block)) == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestBlockConstants_KeyPhrases(t *testing.T) {
	tests := []struct {
		block  string
		name   string
		phrase string
	}{
		{IdentityBlock, "IdentityBlock", "KittyPaw"},
		{IdentityBlock, "IdentityBlock", "How you work"},
		{ExecutionBlock, "ExecutionBlock", "## Rules"},
		{ExecutionBlock, "ExecutionBlock", "Web.search query quality"},
		{QualityBlock, "QualityBlock", "Execution quality"},
		{QualityBlock, "QualityBlock", "never fabricate"},
		{SkillCreationBlock, "SkillCreationBlock", "When to create a skill"},
		{SkillCreationBlock, "SkillCreationBlock", "schedule"},
		{SkillCreationBlock, "SkillCreationBlock", "once"},
		{MemoryBlock, "MemoryBlock", "Memory.user"},
	}
	for _, tt := range tests {
		if !strings.Contains(tt.block, tt.phrase) {
			t.Errorf("%s missing phrase %q", tt.name, tt.phrase)
		}
	}
}

// --- channelHint ---

func TestChannelHint_KnownChannels(t *testing.T) {
	tests := []struct {
		channel string
		want    string
	}{
		{"telegram", "Telegram"},
		{"web", "Web"},
		{"web_chat", "Web"},
		{"cli", "CLI"},
		{"desktop", "CLI"},
		{"slack", "Slack"},
		{"discord", "Discord"},
	}
	for _, tt := range tests {
		hint := channelHint(tt.channel)
		if hint == "" {
			t.Errorf("channelHint(%q) returned empty", tt.channel)
		}
		if !strings.Contains(hint, tt.want) {
			t.Errorf("channelHint(%q) missing %q", tt.channel, tt.want)
		}
	}
}

func TestChannelHint_UnknownChannel(t *testing.T) {
	if hint := channelHint("unknown_future_channel"); hint != "" {
		t.Errorf("unknown channel should return empty, got %q", hint)
	}
	if hint := channelHint(""); hint != "" {
		t.Errorf("empty channel should return empty, got %q", hint)
	}
}

func TestChannelHint_TelegramDispatch(t *testing.T) {
	hint := channelHint("telegram")
	if !strings.Contains(hint, "Telegram.sendMessage") {
		t.Error("telegram hint missing Telegram.sendMessage dispatch guidance")
	}
	if !strings.Contains(hint, "return null") {
		t.Error("telegram hint missing duplicate message avoidance")
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

func TestBuildPrompt_SoulBeforeIdentity(t *testing.T) {
	state := &core.AgentState{AgentID: "test"}
	profile := &core.Profile{ID: "mybot", Soul: "I am the soul."}
	msgs := BuildPrompt(state, "hi", CompactionConfig{RecentWindow: 5}, &core.Config{}, "web", profile, "", "")

	sys := msgs[0].Content
	soulIdx := strings.Index(sys, "## Your Identity (SOUL.md)")
	identityIdx := strings.Index(sys, "You are KittyPaw")
	if soulIdx < 0 || identityIdx < 0 {
		t.Fatal("missing soul or identity section")
	}
	if soulIdx >= identityIdx {
		t.Errorf("SOUL.md (pos %d) should appear before IdentityBlock (pos %d)", soulIdx, identityIdx)
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
	if strings.Contains(sys, "## Your Identity (SOUL.md)") {
		t.Error("nil profile should not inject SOUL.md section")
	}
}

func TestBuildPrompt_BlockPresence(t *testing.T) {
	state := &core.AgentState{AgentID: "test"}
	msgs := BuildPrompt(state, "test", CompactionConfig{RecentWindow: 5}, &core.Config{}, "telegram", nil, "", "")

	sys := msgs[0].Content
	required := []struct {
		name   string
		phrase string
	}{
		{"IdentityBlock", "You are KittyPaw"},
		{"ExecutionBlock", "## Rules"},
		{"QualityBlock", "## Execution quality"},
		{"SkillsBlock", "## Available skill globals"},
		{"SkillCreationBlock", "## When to create a skill"},
		{"MemoryBlock", "## Memory & Learning"},
	}
	for _, r := range required {
		if !strings.Contains(sys, r.phrase) {
			t.Errorf("assembled prompt missing %s (phrase: %q)", r.name, r.phrase)
		}
	}
}

func TestBuildPrompt_ChannelHintInjected(t *testing.T) {
	state := &core.AgentState{AgentID: "test"}
	msgs := BuildPrompt(state, "test", CompactionConfig{RecentWindow: 5}, &core.Config{}, "telegram", nil, "", "")
	sys := msgs[0].Content
	if !strings.Contains(sys, "## Output format (Telegram)") {
		t.Error("telegram channel hint not injected into prompt")
	}
}

func TestBuildPrompt_NoChannelHintForUnknown(t *testing.T) {
	state := &core.AgentState{AgentID: "test"}
	msgs := BuildPrompt(state, "test", CompactionConfig{RecentWindow: 5}, &core.Config{}, "unknown", nil, "", "")
	sys := msgs[0].Content
	if strings.Contains(sys, "## Output format") {
		t.Error("unknown channel should not inject output format section")
	}
}

func TestBuildPrompt_TokenBudget(t *testing.T) {
	// Static text blocks (excluding dynamic skills section from registry) should stay under 1200 tokens.
	// The skills section is dynamic and was part of the old prompt too — budget applies to authored text only.
	staticText := IdentityBlock + "\n\n" + ExecutionBlock + "\n\n" + QualityBlock + "\n\n" + SkillCreationBlock + "\n\n" + MemoryBlock
	tokens := EstimateTokens(staticText)
	const maxTokens = 1200
	if tokens > maxTokens {
		t.Errorf("static text blocks %d tokens exceeds budget %d", tokens, maxTokens)
	}
	t.Logf("static text blocks: %d tokens (budget: %d)", tokens, maxTokens)
}
