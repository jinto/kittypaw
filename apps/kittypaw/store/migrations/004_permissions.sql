CREATE TABLE IF NOT EXISTS permission_file_rules (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    path_pattern TEXT NOT NULL,
    is_exception INTEGER NOT NULL DEFAULT 0,
    can_read INTEGER NOT NULL DEFAULT 1,
    can_write INTEGER NOT NULL DEFAULT 0,
    can_delete INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS permission_network_rules (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    domain_pattern TEXT NOT NULL,
    allowed_methods TEXT NOT NULL DEFAULT 'GET',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS permission_global_paths (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    access_type TEXT NOT NULL CHECK (access_type IN ('read', 'write')),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
