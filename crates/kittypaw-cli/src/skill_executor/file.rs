use std::path::{Path, PathBuf};

use kittypaw_core::error::{KittypawError, Result};
use kittypaw_core::types::SkillCall;

pub(super) fn execute_file(call: &SkillCall, data_dir: Option<&Path>) -> Result<serde_json::Value> {
    let data_dir = data_dir.ok_or_else(|| {
        KittypawError::Sandbox("File operations require a package data directory".into())
    })?;

    // Create data dir if it doesn't exist
    std::fs::create_dir_all(data_dir)?;

    match call.method.as_str() {
        "read" => {
            let rel_path = call.args.first().and_then(|v| v.as_str()).unwrap_or("");
            if rel_path.is_empty() {
                return Err(KittypawError::Sandbox("File.read: path is required".into()));
            }
            let full_path = validate_file_path(data_dir, rel_path)?;
            let content = std::fs::read_to_string(&full_path)?;
            Ok(serde_json::json!({ "content": content }))
        }
        "write" => {
            let rel_path = call.args.first().and_then(|v| v.as_str()).unwrap_or("");
            let content = call.args.get(1).and_then(|v| v.as_str()).unwrap_or("");
            if rel_path.is_empty() {
                return Err(KittypawError::Sandbox(
                    "File.write: path is required".into(),
                ));
            }
            let full_path = validate_file_path(data_dir, rel_path)?;
            // Max file size: 10MB
            if content.len() > 10 * 1024 * 1024 {
                return Err(KittypawError::Sandbox(
                    "File.write: content exceeds 10MB limit".into(),
                ));
            }
            // Create parent directories
            if let Some(parent) = full_path.parent() {
                std::fs::create_dir_all(parent)?;
            }
            std::fs::write(&full_path, content)?;
            Ok(serde_json::json!({ "ok": true }))
        }
        "edit" => {
            let rel_path = call.args.first().and_then(|v| v.as_str()).unwrap_or("");
            let old_content = call.args.get(1).and_then(|v| v.as_str()).unwrap_or("");
            let new_content = call.args.get(2).and_then(|v| v.as_str()).unwrap_or("");
            if rel_path.is_empty() {
                return Err(KittypawError::Sandbox("File.edit: path is required".into()));
            }
            if old_content.is_empty() {
                return Err(KittypawError::Sandbox(
                    "File.edit: old content is required".into(),
                ));
            }
            let full_path = validate_file_path(data_dir, rel_path)?;
            let content = std::fs::read_to_string(&full_path)?;
            if !content.contains(old_content) {
                return Err(KittypawError::Sandbox(
                    "File.edit: old content not found in file".into(),
                ));
            }
            let result = content.replacen(old_content, new_content, 1);
            if result.len() > 10 * 1024 * 1024 {
                return Err(KittypawError::Sandbox(
                    "File.edit: result exceeds 10MB limit".into(),
                ));
            }
            std::fs::write(&full_path, &result)?;
            Ok(serde_json::json!({ "ok": true }))
        }
        _ => Err(KittypawError::Sandbox(format!(
            "Unknown File method: {}",
            call.method
        ))),
    }
}

/// Validate that a relative path stays within the data directory.
/// Rejects ".." components and symlinks escaping the boundary.
pub(super) fn validate_file_path(data_dir: &Path, rel_path: &str) -> Result<PathBuf> {
    if rel_path.contains("..") {
        return Err(KittypawError::Sandbox(
            "File: path traversal not allowed".into(),
        ));
    }
    let rel = rel_path.trim_start_matches('/');
    let full = data_dir.join(rel);
    if full.exists() {
        // For existing files, canonicalize and check prefix
        let canonical = full.canonicalize()?;
        let canonical_root = data_dir.canonicalize()?;
        if !canonical.starts_with(&canonical_root) {
            return Err(KittypawError::Sandbox(
                "File: path escapes data directory".into(),
            ));
        }
        Ok(canonical)
    } else {
        // For non-existent files, canonicalize the parent and append filename
        let parent = full
            .parent()
            .ok_or_else(|| KittypawError::Sandbox("File: path has no parent directory".into()))?;
        let file_name = full
            .file_name()
            .ok_or_else(|| KittypawError::Sandbox("File: path has no filename".into()))?;
        // Parent must exist; if it doesn't, reject to prevent traversal via missing dirs
        let canonical_parent = parent
            .canonicalize()
            .map_err(|_| KittypawError::Sandbox("File: parent directory does not exist".into()))?;
        let canonical_root = data_dir.canonicalize()?;
        if !canonical_parent.starts_with(&canonical_root) {
            return Err(KittypawError::Sandbox(
                "File: path escapes data directory".into(),
            ));
        }
        Ok(canonical_parent.join(file_name))
    }
}
