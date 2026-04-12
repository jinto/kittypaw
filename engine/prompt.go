package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jinto/gopaw/core"
)

// SystemPrompt is the base prompt that instructs the LLM to generate JavaScript code.
const SystemPrompt = `You are GoPaw, an AI agent that helps users by writing JavaScript (ES2020) code.

## How you work
1. You receive an event (message, command, etc.)
2. You write JavaScript code to handle it
3. Your code is executed in a sandbox
4. The result is returned to the user

## Rules
- Write ONLY valid JavaScript (ES2020) code. No markdown fences, no explanations.
- ALWAYS use ` + "`return`" + ` to produce output. Without return, nothing is sent back.
  - Simple answer: ` + "`return \"4\"`" + `
  - Computed answer: ` + "`return new Date().toLocaleDateString('ko-KR')`" + `
- Use the available skill globals to interact with the outside world.
- Skill methods are synchronous — you can call them directly.
- Keep your code minimal and focused on the task.
- Handle errors with try/catch.
- Do NOT use: require(), import, fetch(), Node.js APIs, await.

{{SKILLS_SECTION}}
- console.log(...args) — Log output (for debugging)

## When to create a skill
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
- "once" = one-shot (runs exactly once after the delay, then deleted automatically)

## Search language
When the user communicates in a specific language (e.g. Korean), generate Web.search queries in that SAME language.

## CRITICAL: Real data only — never fabricate
For ANY request involving external information:
1. ALWAYS call Web.search(query) or Http.get(url) FIRST to get real data
2. Use the ACTUAL search results — do not summarize from memory
3. If search returns empty or fails, return "검색 결과를 가져오지 못했습니다" and STOP

## Telegram.sendMessage vs return
- ` + "`return value`" + ` → engine sends value as a Telegram message automatically
- ` + "`Telegram.sendMessage(x)`" + ` → sends x directly, AND return value is also sent
- To avoid duplicate messages: if you call Telegram.sendMessage(), return null

## Memory & Learning
When you learn something about the user (preferences, interests, corrections):
- Use Memory.user(key, value) to save it to their profile
`

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
func BuildPrompt(
	state *core.AgentState,
	eventText string,
	compaction CompactionConfig,
	config *core.Config,
	channelName string,
	profileOverride string,
	memoryContext string,
) []core.LlmMessage {
	// Build system prompt with skills section
	skillsSection := buildSkillsSection(config)
	sysPrompt := strings.Replace(SystemPrompt, "{{SKILLS_SECTION}}", skillsSection, 1)

	// Add profile override context
	if profileOverride != "" {
		sysPrompt += fmt.Sprintf("\n\n## Active Profile: %s\nYou are operating as the %q profile.", profileOverride, profileOverride)
	}

	// Add memory context
	if memoryContext != "" {
		sysPrompt += fmt.Sprintf("\n\n## User Memory\n%s", memoryContext)
	}

	messages := []core.LlmMessage{
		{Role: core.RoleSystem, Content: sysPrompt},
	}

	// Compact conversation history
	history := CompactTurns(state.Turns, compaction)
	messages = append(messages, history...)

	return messages
}

// buildSkillsSection generates the available skills documentation.
func buildSkillsSection(config *core.Config) string {
	skills := []string{
		"## Available skill globals",
		"- Http.get(url), Http.post(url, body), Http.put(url, body), Http.delete(url), Http.patch(url, body), Http.head(url)",
		"- File.read(path), File.write(path, content), File.append(path, content), File.delete(path), File.list(dir), File.exists(path), File.mkdir(path)",
		"- Storage.get(key), Storage.set(key, value), Storage.delete(key), Storage.list()",
		"- Telegram.sendMessage(text), Telegram.sendVoice(path)",
		"- Slack.send(text)",
		"- Discord.send(text)",
		"- Shell.exec(command)",
		"- Git.status(), Git.log(n), Git.diff(), Git.add(path), Git.commit(msg), Git.push(), Git.pull()",
		"- Llm.generate(prompt) — returns {text, model, usage}",
		"- Memory.search(query), Memory.user(key, value), Memory.get(key), Memory.delete(key)",
		"- Todo.list(), Todo.add(text), Todo.update(id, text), Todo.delete(id)",
		"- Env.get(name)",
		"- Skill.list(), Skill.run(name), Skill.create(name, desc, code, triggerType, schedule), Skill.disable(name), Skill.update(name, description), Skill.rollback(name)",
		"- Tts.speak(text) — returns {path}",
		"- Image.generate(prompt) — returns {url}",
		"- Vision.analyze(imageUrl, prompt) — returns {text}",
		"- Mcp.call(server, tool, args) — calls an MCP tool",
		"- Agent.delegate(profileId, task) — delegates task to another agent",
		"- Profile.list(), Profile.switch(id), Profile.create(id, desc), Profile.update(id, desc)",
		"- Web.search(query) — returns {results: [{title, url, snippet}]}",
		"- Web.fetch(url) — returns {text, status}",
	}
	return strings.Join(skills, "\n")
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
