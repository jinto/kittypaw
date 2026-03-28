use std::path::Path;

use kittypaw_core::error::{KittypawError, Result};
use kittypaw_core::workspace::FileEntry;

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct SemanticResult {
    pub path: String,
    pub rank: usize,
    pub reason: String,
}

/// Simple semantic search using an LLM provider trait.
/// Reads first 500 chars of each file, sends to LLM for ranking.
pub async fn semantic_search<F, Fut>(
    query: &str,
    files: &[FileEntry],
    workspace_root: &Path,
    llm_call: F,
) -> Result<Vec<SemanticResult>>
where
    F: FnOnce(String) -> Fut,
    Fut: std::future::Future<Output = Result<String>>,
{
    let file_summaries: Vec<String> = files
        .iter()
        .filter(|f| !f.is_dir)
        .map(|f| {
            let abs = workspace_root.join(&f.path);
            let preview = std::fs::read_to_string(&abs)
                .unwrap_or_default()
                .chars()
                .take(500)
                .collect::<String>();
            format!("File: {}\nPreview:\n{}\n", f.path, preview)
        })
        .collect();

    if file_summaries.is_empty() {
        return Ok(vec![]);
    }

    let prompt = format!(
        "You are a file relevance ranker. Given the search query below, rank the provided files by relevance.\n\
         Return ONLY a JSON array of objects with keys \"path\" and \"reason\", ordered from most to least relevant.\n\
         Include only files that are relevant (omit irrelevant ones).\n\
         Example: [{{\"path\": \"src/main.rs\", \"reason\": \"contains main entry point\"}}]\n\n\
         Query: {query}\n\n\
         Files:\n{}",
        file_summaries.join("\n---\n")
    );

    let response = llm_call(prompt).await?;

    parse_semantic_response(&response)
}

fn parse_semantic_response(response: &str) -> Result<Vec<SemanticResult>> {
    // Find JSON array in response (LLM may wrap it in markdown)
    let json_str = extract_json_array(response);

    let parsed: serde_json::Value = serde_json::from_str(&json_str).map_err(|e| {
        KittypawError::Sandbox(format!("Failed to parse LLM response as JSON: {e}"))
    })?;

    let arr = parsed
        .as_array()
        .ok_or_else(|| KittypawError::Sandbox("Expected JSON array from LLM".to_string()))?;

    let results = arr
        .iter()
        .enumerate()
        .filter_map(|(i, v)| {
            let path = v["path"].as_str()?.to_string();
            let reason = v["reason"].as_str().unwrap_or("").to_string();
            Some(SemanticResult {
                path,
                rank: i + 1,
                reason,
            })
        })
        .collect();

    Ok(results)
}

fn extract_json_array(text: &str) -> String {
    // Try to find [...] block
    if let Some(start) = text.find('[') {
        if let Some(end) = text.rfind(']') {
            if end > start {
                return text[start..=end].to_string();
            }
        }
    }
    text.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_semantic_response() {
        let response = r#"[{"path": "src/main.rs", "reason": "entry point"}, {"path": "lib.rs", "reason": "library"}]"#;
        let results = parse_semantic_response(response).unwrap();
        assert_eq!(results.len(), 2);
        assert_eq!(results[0].path, "src/main.rs");
        assert_eq!(results[0].rank, 1);
        assert_eq!(results[1].path, "lib.rs");
        assert_eq!(results[1].rank, 2);
    }

    #[test]
    fn test_parse_with_markdown_wrapper() {
        let response = "Here are the results:\n```json\n[{\"path\": \"foo.rs\", \"reason\": \"relevant\"}]\n```";
        let results = parse_semantic_response(response).unwrap();
        assert_eq!(results.len(), 1);
        assert_eq!(results[0].path, "foo.rs");
    }

    #[test]
    fn test_parse_empty_array() {
        let results = parse_semantic_response("[]").unwrap();
        assert!(results.is_empty());
    }
}
