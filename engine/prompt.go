package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jinto/kittypaw/core"
	mcpreg "github.com/jinto/kittypaw/mcp"
)

// IdentityBlock defines who KittyPaw is and how it operates.
const IdentityBlock = `You are KittyPaw, an AI agent that helps users by writing JavaScript (ES2020) code.

## How you work
1. You receive an event (message, command, etc.)
2. Understand what the user actually wants — not just the literal words. Think about the most useful outcome.
3. You write JavaScript code to handle it
4. Your code is executed in a sandbox
5. The result is returned to the user`

// ExecutionBlock defines the JavaScript code generation rules.
const ExecutionBlock = `## Rules
- Write ONLY valid JavaScript (ES2020) code. No markdown fences, no explanations.
- ALWAYS use ` + "`return`" + ` to produce output. Without return, nothing is sent back.
  - Simple answer: ` + "`return \"4\"`" + `
  - Computed answer: ` + "`return new Date().toLocaleDateString('ko-KR')`" + `
- Use the available skill globals to interact with the outside world.
- Skill methods are synchronous — you can call them directly.
- Keep your code minimal and focused on the task.
- Handle errors with try/catch.
- Do NOT use: require(), import, fetch(), Node.js APIs, await.

## Web.search query quality
- NEVER pass a single generic word like "뉴스" or "news". Always add context: topic, date, or specifics.
  BAD:  Web.search("뉴스")  → returns news portal homepages, useless
  GOOD: Web.search("오늘 주요 뉴스 2026")  → returns actual articles
  GOOD: Web.search("한국 경제 뉴스 오늘")  → returns relevant results
- If the user's request is vague, infer a reasonable topic or ask. "뉴스 검색해줘" → search for today's top headlines.
- When the user communicates in a specific language (e.g. Korean), generate queries in that SAME language.`

// QualityBlock enforces tool execution, result quality, and code-level persistence.
const QualityBlock = `## Execution quality
For ANY request involving external information, you MUST generate code that calls available tools.
Never answer from memory — always fetch real data first.

WRONG: return "AI is advancing rapidly..."  ← fabricated, no tool call
RIGHT: const r = Web.search("AI news today"); return r.results.map(...)  ← real data

If results are insufficient, your code should try additional steps:
- Search results too brief → fetch detail from top result URLs
- First query returns nothing → try alternative keywords
- Combine multiple tool calls in one code block for thorough answers

If all tool calls fail, return "검색 결과를 가져오지 못했습니다" and stop — never fabricate data.`

// SkillCreationBlock guides when and how to create scheduled or one-shot skills.
const SkillCreationBlock = `## When to create a skill
If the user asks for something recurring ("매일", "every day", "주기적으로"), create a skill with a schedule trigger.
For one-time delayed requests ("2분 뒤", "한 번만", "이번 한 번", "내일 아침 한 번"), create a skill with a once trigger.
For immediate one-time requests, just execute the code directly without creating a skill.

Example — scheduled skill (recurring):
  await Skill.create("ai-news", "AI 뉴스 매시간 요약", ` + "`" + `
    const r = await Web.search("AI news");
    const summary = r.results.map(x => x.title).join("\\n");
    await Telegram.sendMessage(summary);
    return summary;
  ` + "`" + `, "schedule", "every 1h");

Example — once skill (one-shot delayed):
  await Skill.create("ai-news-once", "2분 뒤 AI 뉴스 한 번 요약", ` + "`" + `
    const r = await Web.search("AI 뉴스 오늘");
    const article = await Web.fetch(r.results[0].url);
    const summary = article.text.slice(0, 800);
    await Telegram.sendMessage(summary);
  ` + "`" + `, "once", "2m");

CRITICAL: Never use "schedule" trigger for one-time delayed tasks.
- "schedule" = recurring (runs repeatedly on cron)
- "once" = one-shot (runs exactly once after the delay, then deleted automatically)`

// MemoryBlock guides memory usage for user preferences.
const MemoryBlock = `## Memory & Learning
When you learn something about the user (preferences, interests, corrections):
- Use Memory.user(key, value) to save it to their profile`

// SystemPrompt is the assembled base prompt, stored in agent state for auditing.
// BuildPrompt assembles blocks directly — this var exists for backward compatibility.
var SystemPrompt = IdentityBlock + "\n\n" + ExecutionBlock + "\n\n" + QualityBlock + "\n\n" + SkillCreationBlock + "\n\n" + MemoryBlock

// channelHint returns channel-specific output format guidance.
// Returns empty string for unknown channels.
func channelHint(channelName string) string {
	switch channelName {
	case "telegram":
		return `## Output format (Telegram)
- Keep messages short and readable — Telegram renders limited markdown.
- Minimize markdown: avoid headers, complex formatting.
- ` + "`return value`" + ` → engine sends value as a Telegram message automatically.
- ` + "`Telegram.sendMessage(x)`" + ` → sends x directly, AND return value is also sent.
- To avoid duplicate messages: if you call Telegram.sendMessage(), return null.`
	case "web", "web_chat":
		return `## Output format (Web)
- Markdown is fully supported: headers, code blocks, lists, links.
- Use formatting to improve readability.`
	case "cli", "desktop":
		return `## Output format (CLI)
- Prefer plain text output.
- Use simple formatting: dashes for lists, indentation for structure.`
	case "slack":
		return `## Output format (Slack)
- Use Slack mrkdwn format: *bold*, _italic_, ~strike~, ` + "`code`" + `.
- Links: <url|text>. Avoid standard markdown.`
	case "discord":
		return `## Output format (Discord)
- Use Discord markdown: **bold**, *italic*, ~~strike~~, ` + "`code`" + `.
- Code blocks with language hints are supported.`
	default:
		return ""
	}
}

// FormatEvent extracts the user-facing text from an event.
func FormatEvent(event *core.Event) string {
	var payload core.ChatPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return string(event.Payload)
	}
	return payload.Text
}

// FormatExecResult summarizes an execution result for conversation history.
func FormatExecResult(result *core.ExecutionResult) string {
	if result.Success {
		return fmt.Sprintf("output: %s", result.Output)
	}
	return fmt.Sprintf("error: %s", result.Error)
}

// BuildPrompt constructs the LLM message chain from agent state and config.
// Assembly order: SOUL.md → Identity → Execution → Quality → Channel → Skills → SkillCreation → Memory → MCP → Nick/UserMD → MemoryContext → Observations
func BuildPrompt(
	state *core.AgentState,
	eventText string,
	compaction CompactionConfig,
	config *core.Config,
	channelName string,
	profile *core.Profile,
	memoryContext string,
	mcpToolsSection string,
	observations []core.Observation,
) []core.LlmMessage {
	var sb strings.Builder

	// 1. SOUL.md first — identity takes highest priority
	if profile != nil && profile.Soul != "" {
		sb.WriteString("## Your Identity (SOUL.md)\n")
		sb.WriteString(profile.Soul)
		sb.WriteString("\n\n")
	}

	// 2. Identity block
	sb.WriteString(IdentityBlock)
	sb.WriteString("\n\n")

	// 3. Execution rules
	sb.WriteString(ExecutionBlock)
	sb.WriteString("\n\n")

	// 4. Quality enforcement
	sb.WriteString(QualityBlock)
	sb.WriteString("\n\n")

	// 5. Channel-specific hints (dynamic)
	if hint := channelHint(channelName); hint != "" {
		sb.WriteString(hint)
		sb.WriteString("\n\n")
	}

	// 6. Available skills (dynamic)
	sb.WriteString(buildSkillsSection(config))
	sb.WriteString("\n\n")

	// 7. Skill creation guide
	sb.WriteString(SkillCreationBlock)
	sb.WriteString("\n\n")

	// 8. Memory guide
	sb.WriteString(MemoryBlock)

	// 9. MCP tools (dynamic)
	if mcpToolsSection != "" {
		sb.WriteString("\n\n")
		sb.WriteString(mcpToolsSection)
	}

	// 10. Profile nick + user markdown
	if profile != nil {
		if profile.Nick != "" {
			sb.WriteString("\n\nYour name/nickname is: ")
			sb.WriteString(profile.Nick)
		}
		if profile.UserMD != "" {
			sb.WriteString("\n\n## User Profile (USER.md)\n")
			sb.WriteString(profile.UserMD)
		}
	}

	// 11. Memory context
	if memoryContext != "" {
		sb.WriteString("\n\n## User Memory\n")
		sb.WriteString(memoryContext)
	}

	// 12. Observations (volatile — replaced each observe round, not accumulated)
	if len(observations) > 0 {
		sb.WriteString("\n\n## Current Observations\n")
		sb.WriteString("You previously called Agent.observe(). Analyze these results and write code to produce your response.\n")
		sb.WriteString("Do NOT call Agent.observe() again unless you need additional data.\n\n")
		for _, obs := range observations {
			if obs.Label != "" {
				sb.WriteString("### ")
				sb.WriteString(obs.Label)
				sb.WriteByte('\n')
			}
			sb.WriteString(obs.Data)
			sb.WriteString("\n\n")
		}
	}

	messages := []core.LlmMessage{
		{Role: core.RoleSystem, Content: sb.String()},
	}

	// Compact conversation history
	history := CompactTurns(state.Turns, compaction)
	messages = append(messages, history...)

	return messages
}

// buildSkillsSection generates the available skills documentation
// from the canonical core.SkillRegistry.
func buildSkillsSection(_ *core.Config) string {
	lines := []string{"## Available skill globals"}
	for _, skill := range core.SkillRegistry {
		var sigs []string
		for _, m := range skill.Methods {
			sigs = append(sigs, m.Signature)
		}
		lines = append(lines, "- "+strings.Join(sigs, ", "))
	}
	lines = append(lines, "- console.log(...args) — Log output (for debugging)")
	return strings.Join(lines, "\n")
}

// BuildMCPToolsSection generates a prompt section listing MCP tools from all
// connected servers. Servers are sorted alphabetically, tools within each server
// are sorted by name. The output is capped at 2000 bytes; excess tools are
// counted and reported as "[N more tools omitted]".
// Tool names and descriptions are sanitized to prevent prompt injection.
// Returns "" if allTools is nil or empty.
func BuildMCPToolsSection(allTools map[string][]mcpreg.ToolInfo) string {
	if len(allTools) == 0 {
		return ""
	}

	servers := make([]string, 0, len(allTools))
	for name := range allTools {
		servers = append(servers, name)
	}
	sort.Strings(servers)

	const budget = 2000
	header := "## MCP Tools\n\n"
	var b strings.Builder
	b.WriteString(header)
	remaining := budget - len(header)
	omitted := 0

outer:
	for si, srv := range servers {
		tools := make([]mcpreg.ToolInfo, len(allTools[srv]))
		copy(tools, allTools[srv])
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

		srvHeader := fmt.Sprintf("### %s\n", sanitizeMCPField(srv, 64))
		if remaining < len(srvHeader)+30 {
			for _, s := range servers[si:] {
				omitted += len(allTools[s])
			}
			break
		}
		b.WriteString(srvHeader)
		remaining -= len(srvHeader)

		for ti, tool := range tools {
			line := fmt.Sprintf("- %s: %s\n",
				sanitizeMCPField(tool.Name, 64),
				sanitizeMCPField(tool.Description, 200))
			if remaining < len(line) {
				omitted += len(tools) - ti
				for _, s := range servers[si+1:] {
					omitted += len(allTools[s])
				}
				break outer
			}
			b.WriteString(line)
			remaining -= len(line)
		}
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "[%d more tools omitted]\n", omitted)
	}
	return b.String()
}

// sanitizeMCPField strips newlines and markdown control characters from
// MCP server-supplied strings to prevent prompt injection via tool metadata.
func sanitizeMCPField(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.Map(func(r rune) rune {
		if r == '#' || r == '`' {
			return -1
		}
		return r
	}, s)
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// ParseAtMention extracts @profile_id from the start of user text.
// Returns (profileID, remainingText, matched).
func ParseAtMention(text string) (string, string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "@") {
		return "", text, false
	}
	rest := text[1:]
	if rest == "" {
		return "", text, false
	}

	// Find end of profile ID (first whitespace)
	idEnd := strings.IndexFunc(rest, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n'
	})
	if idEnd == -1 {
		idEnd = len(rest)
	}

	profileID := rest[:idEnd]
	if profileID == "" {
		return "", text, false
	}

	// Validate: alphanumeric + hyphen/underscore only
	for _, r := range profileID {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return "", text, false
		}
	}

	remaining := strings.TrimSpace(rest[idEnd:])
	return profileID, remaining, true
}
