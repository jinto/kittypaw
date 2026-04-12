CREATE TABLE IF NOT EXISTS profile_meta (
    id TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    equipped_skills TEXT NOT NULL DEFAULT '[]',
    active INTEGER NOT NULL DEFAULT 1,
    created_by TEXT NOT NULL DEFAULT 'manual',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
