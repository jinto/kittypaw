CREATE TABLE agent_checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    conv_row_id INTEGER NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);
CREATE INDEX idx_checkpoints_agent ON agent_checkpoints(agent_id);
