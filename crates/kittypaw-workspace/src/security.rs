use kittypaw_core::error::{KittypawError, Result};
use std::path::{Path, PathBuf};

/// Validates that `rel_path` resolves to a path inside `workspace_root`.
/// Rejects paths containing ".." components and symlinks pointing outside.
pub fn validate_path(workspace_root: &Path, rel_path: &str) -> Result<PathBuf> {
    // Reject ".." components early before any resolution
    if rel_path.contains("..") {
        return Err(KittypawError::Sandbox(format!(
            "Path traversal not allowed: {rel_path}"
        )));
    }

    // Strip leading slash to make the path relative
    let rel = rel_path.trim_start_matches('/');

    let joined = workspace_root.join(rel);

    // Canonicalize the workspace root (must exist)
    let canonical_root = workspace_root
        .canonicalize()
        .map_err(|e| KittypawError::Io(e))?;

    // The target file may not exist yet (e.g. write_file creating new file),
    // so canonicalize the nearest existing ancestor, then re-append the suffix.
    let canonical_path = canonicalize_partial(&joined)?;

    if !canonical_path.starts_with(&canonical_root) {
        return Err(KittypawError::Sandbox(format!(
            "Path escapes workspace root: {rel_path}"
        )));
    }

    // Additionally check: if the final path is a symlink, resolve it and re-check
    if joined.exists() && joined.is_symlink() {
        let resolved = joined.read_link().map_err(|e| KittypawError::Io(e))?;
        let resolved = if resolved.is_absolute() {
            resolved
        } else {
            joined.parent().unwrap_or(Path::new("/")).join(&resolved)
        };
        let resolved_canonical = resolved.canonicalize().map_err(|e| KittypawError::Io(e))?;
        if !resolved_canonical.starts_with(&canonical_root) {
            return Err(KittypawError::Sandbox(format!(
                "Symlink escapes workspace root: {rel_path}"
            )));
        }
    }

    Ok(canonical_path)
}

/// Canonicalize a path that may not fully exist by walking up to the first
/// existing ancestor, canonicalizing that, then re-appending the remainder.
fn canonicalize_partial(path: &Path) -> Result<PathBuf> {
    let mut existing = path.to_path_buf();
    let mut suffix = std::collections::VecDeque::new();

    loop {
        if existing.exists() {
            let canonical = existing.canonicalize().map_err(|e| KittypawError::Io(e))?;
            let mut result = canonical;
            for component in suffix.iter() {
                result = result.join(component);
            }
            return Ok(result);
        }
        match existing.file_name() {
            Some(name) => {
                suffix.push_front(name.to_owned());
                existing = match existing.parent() {
                    Some(p) => p.to_path_buf(),
                    None => {
                        return Err(KittypawError::Sandbox(
                            "Could not resolve path ancestor".to_string(),
                        ))
                    }
                };
            }
            None => {
                return Err(KittypawError::Sandbox(
                    "Could not resolve path ancestor".to_string(),
                ))
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn test_valid_path() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path();
        // Create a file to canonicalize
        fs::write(root.join("file.txt"), "").unwrap();
        let result = validate_path(root, "file.txt");
        assert!(result.is_ok());
    }

    #[test]
    fn test_dotdot_rejected() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path();
        let result = validate_path(root, "../etc/passwd");
        assert!(result.is_err());
    }

    #[test]
    fn test_new_file_path_valid() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path();
        // new_file.txt doesn't exist yet
        let result = validate_path(root, "new_file.txt");
        assert!(result.is_ok());
        let p = result.unwrap();
        assert!(p.starts_with(root.canonicalize().unwrap()));
    }
}
