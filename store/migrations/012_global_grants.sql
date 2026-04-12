CREATE TABLE IF NOT EXISTS global_grants (
    capability TEXT PRIMARY KEY,
    granted_at TEXT NOT NULL DEFAULT (datetime('now'))
);
