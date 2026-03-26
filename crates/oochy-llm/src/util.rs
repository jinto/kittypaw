/// Strip markdown code fences from LLM-generated code.
/// Handles ```javascript, ```js, and bare ``` fences.
pub fn strip_code_fences(code: &str) -> String {
    let trimmed = code.trim();

    // Check for opening code fence
    if let Some(rest) = trimmed.strip_prefix("```") {
        // Skip the language identifier line
        let after_lang = if let Some(pos) = rest.find('\n') {
            &rest[pos + 1..]
        } else {
            return String::new();
        };

        // Remove closing fence
        if let Some(content) = after_lang.strip_suffix("```") {
            return content.trim().to_string();
        }
        // No closing fence — return as-is
        return after_lang.trim().to_string();
    }

    trimmed.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_strip_js_fence() {
        let input = "```javascript\nconsole.log('hello');\n```";
        assert_eq!(strip_code_fences(input), "console.log('hello');");
    }

    #[test]
    fn test_strip_bare_fence() {
        let input = "```\nconst x = 1;\n```";
        assert_eq!(strip_code_fences(input), "const x = 1;");
    }

    #[test]
    fn test_no_fence() {
        let input = "console.log('hello');";
        assert_eq!(strip_code_fences(input), "console.log('hello');");
    }

    #[test]
    fn test_strip_with_whitespace() {
        let input = "  ```js\n  const x = 1;\n  ```  ";
        assert_eq!(strip_code_fences(input), "const x = 1;");
    }
}
