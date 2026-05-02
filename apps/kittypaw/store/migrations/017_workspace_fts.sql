-- Workspace file index for full-text search (FTS5).
-- workspace_files stores metadata; workspace_fts stores searchable content.

CREATE TABLE IF NOT EXISTS workspace_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL,
    abs_path TEXT NOT NULL,
    rel_path TEXT NOT NULL,
    filename TEXT NOT NULL,
    extension TEXT NOT NULL DEFAULT '',
    size INTEGER NOT NULL DEFAULT 0,
    modified_at TEXT NOT NULL,
    has_content INTEGER NOT NULL DEFAULT 0,
    indexed_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(workspace_id, abs_path)
);

CREATE INDEX IF NOT EXISTS idx_wf_workspace ON workspace_files(workspace_id);
CREATE INDEX IF NOT EXISTS idx_wf_ws_ext ON workspace_files(workspace_id, extension);

-- Standalone FTS5: stores filename + body internally for snippet() support.
-- rowid is manually synced with workspace_files.id during batch indexing.
CREATE VIRTUAL TABLE IF NOT EXISTS workspace_fts USING fts5(
    filename, body,
    tokenize='unicode61'
);
