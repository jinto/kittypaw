package sandbox

import "strings"

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
