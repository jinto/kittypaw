package sandbox

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Line-protocol tags written to stdout by the JS wrapper.
const (
	tagSkillCall = "__SKILL_CALL__:"
	tagResult    = "__RESULT__:"
	tagError     = "__ERROR__:"
)

// skillPrimitives maps each skill name to its method set.
// Every method becomes a global: e.g. Http.get(...), File.read(...).
var skillPrimitives = map[string][]string{
	"Http":      {"get", "post", "put", "delete", "patch", "head"},
	"File":      {"read", "write", "append", "delete", "list", "exists", "mkdir"},
	"Storage":   {"get", "set", "delete", "list"},
	"Telegram":  {"send"},
	"Slack":     {"send"},
	"Discord":   {"send"},
	"Shell":     {"exec"},
	"Git":       {"status", "log", "diff", "add", "commit", "push", "pull"},
	"Llm":       {"generate"},
	"Memory":    {"search", "set", "get", "delete"},
	"Todo":      {"list", "add", "update", "delete"},
	"Env":       {"get"},
	"Skill":     {"list", "run", "create", "disable"},
	"Tts":       {"speak"},
	"Image":     {"generate"},
	"Vision":    {"analyze"},
	"Mcp":       {"call"},
	"Agent":     {"delegate"},
	"Profile":   {"list", "switch", "create", "update"},
	"Web":       {"search", "fetch"},
}

// orderedSkillNames ensures deterministic output order (Go maps iterate randomly).
var orderedSkillNames = []string{
	"Http", "File", "Storage",
	"Telegram", "Slack", "Discord",
	"Shell", "Git", "Llm",
	"Memory", "Todo", "Env",
	"Skill", "Tts", "Image",
	"Vision", "Mcp", "Agent",
	"Profile", "Web",
}

// buildWrapper generates the complete JS source that wraps user code.
//
// The wrapper:
//  1. Defines each skill primitive as a global object whose methods
//     serialise the call to stdout and return null.
//  2. Injects `context` as a frozen global from the provided map.
//  3. Wraps the user code in try/catch, emitting result or error.
func buildWrapper(userCode string, jsContext map[string]any) (string, error) {
	var b strings.Builder

	// --- skill stubs ---
	for _, name := range orderedSkillNames {
		methods := skillPrimitives[name]
		b.WriteString(fmt.Sprintf("const %s = {\n", name))
		for i, method := range methods {
			// Each stub prints a tagged JSON line and returns null.
			b.WriteString(fmt.Sprintf(
				"  %s: function(...args) {\n"+
					"    const line = JSON.stringify({skill:%q,method:%q,args});\n"+
					"    console.log(%q + line);\n"+
					"    return null;\n"+
					"  }",
				method, name, method, tagSkillCall,
			))
			if i < len(methods)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString("};\n\n")
	}

	// --- inject context ---
	ctxJSON, err := json.Marshal(jsContext)
	if err != nil {
		return "", fmt.Errorf("marshal jsContext: %w", err)
	}
	b.WriteString(fmt.Sprintf("const context = Object.freeze(%s);\n\n", ctxJSON))

	// --- user code wrapped in try/catch ---
	b.WriteString("try {\n")
	b.WriteString("  const __result = (function(){\n")
	b.WriteString(userCode)
	b.WriteByte('\n')
	b.WriteString("  })();\n")
	b.WriteString(fmt.Sprintf(
		"  if (__result !== undefined) console.log(%q + JSON.stringify(__result));\n",
		tagResult,
	))
	b.WriteString("} catch(__err) {\n")
	b.WriteString(fmt.Sprintf(
		"  console.log(%q + (__err.stack || __err.message || String(__err)));\n",
		tagError,
	))
	b.WriteString("}\n")

	return b.String(), nil
}
