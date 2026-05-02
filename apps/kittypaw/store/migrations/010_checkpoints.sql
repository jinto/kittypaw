CREATE TABLE IF NOT EXISTS conversation_checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    label TEXT NOT NULL DEFAULT '',
    turn_id INTEGER NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_conversation_checkpoints_turn ON conversation_checkpoints(turn_id);
