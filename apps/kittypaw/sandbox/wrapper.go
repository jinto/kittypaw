package sandbox

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jinto/gopaw/core"
)

// Line-protocol tags written to stdout by the JS wrapper.
const (
	tagSkillReq = "__SKILL_REQ__:"
	tagResult   = "__RESULT__:"
	tagError    = "__ERROR__:"
)

// buildWrapper generates the complete JS source that wraps user code.
//
// The wrapper:
//  1. Defines a synchronous __callSkill helper that writes a tagged request
//     to stdout and reads the JSON response from stdin (blocking).
//  2. Defines each skill primitive as a global whose methods call __callSkill.
//  3. Injects `context` as a frozen global from the provided map.
//  4. Wraps the user code in try/catch, emitting result or error.
func buildWrapper(userCode string, jsContext map[string]any) (string, error) {
	var b strings.Builder

	// --- runtime I/O helpers ---
	// Detect Deno vs Node at runtime and provide synchronous stdin/stdout access.
	b.WriteString(jsIOHelpers)

	// --- skill stubs (generated from core.SkillRegistry) ---
	for _, skill := range core.SkillRegistry {
		b.WriteString(fmt.Sprintf("const %s = {\n", skill.Name))
		for i, method := range skill.Methods {
			b.WriteString(fmt.Sprintf(
				"  %s: function(...args) { return __callSkill(%q, %q, args); }",
				method.Name, skill.Name, method.Name,
			))
			if i < len(skill.Methods)-1 {
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

	// Auto-return: if the code has no return statement, prepend return
	// to the last non-empty line so bare expressions produce output.
	wrappedCode := autoReturn(userCode)

	// --- user code wrapped in try/catch ---
	b.WriteString("try {\n")
	b.WriteString("  const __result = (function(){\n")
	b.WriteString(wrappedCode)
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

// jsIOHelpers provides the synchronous I/O bridge for skill calls.
// Detects Deno vs Node at runtime and uses the appropriate API.
const jsIOHelpers = `const __isDeno = typeof Deno !== 'undefined';

function __readLine() {
  if (__isDeno) {
    const buf = new Uint8Array(1);
    let line = '';
    while (true) {
      const n = Deno.stdin.readSync(buf);
      if (n === null || n === 0) return line;
      if (buf[0] === 10) return line;
      line += String.fromCharCode(buf[0]);
    }
  } else {
    const __fs = require('fs');
    const buf = Buffer.alloc(1);
    let line = '';
    while (true) {
      try {
        const n = __fs.readSync(0, buf, 0, 1);
        if (n === 0) return line;
      } catch { return line; }
      if (buf[0] === 10) return line;
      line += String.fromCharCode(buf[0]);
    }
  }
}

function __writeOut(s) {
  if (__isDeno) {
    Deno.stdout.writeSync(new TextEncoder().encode(s));
  } else {
    const __fs = require('fs');
    __fs.writeSync(1, s);
  }
}

function __callSkill(skill, method, args) {
  const req = JSON.stringify({skill, method, args});
  __writeOut("` + tagSkillReq + `" + req + "\n");
  const resp = __readLine();
  try { return JSON.parse(resp); } catch { return null; }
}

`

// autoReturn adds "return" to the last expression when the code contains no
// return statement. This handles the common case where the LLM produces a
// bare expression like `"hello"` or `4` instead of `return "hello"`.
func autoReturn(code string) string {
	if strings.Contains(code, "return ") || strings.Contains(code, "return;") {
		return code
	}
	lines := strings.Split(strings.TrimRight(code, " \n\t"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || trimmed == "}" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		// Don't prepend return to statements that can't be expressions.
		for _, kw := range []string{"throw ", "if ", "if(", "for ", "for(", "while ", "while(", "switch ", "switch(", "try ", "try{", "const ", "let ", "var "} {
			if strings.HasPrefix(trimmed, kw) {
				return code
			}
		}
		lines[i] = strings.Replace(lines[i], trimmed, "return "+trimmed, 1)
		return strings.Join(lines, "\n")
	}
	return code
}
