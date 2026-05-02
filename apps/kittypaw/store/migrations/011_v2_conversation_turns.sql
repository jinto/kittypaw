CREATE TABLE IF NOT EXISTS conversation_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    system_prompt TEXT NOT NULL DEFAULT '',
    state_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS v2_conversation_turns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    code TEXT,
    result TEXT,
    channel TEXT NOT NULL DEFAULT '',
    channel_user_id TEXT NOT NULL DEFAULT '',
    chat_id TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    timestamp TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_v2_conversation_turns_timestamp ON v2_conversation_turns(timestamp);
CREATE INDEX IF NOT EXISTS idx_v2_conversation_turns_role_timestamp ON v2_conversation_turns(role, timestamp);
CREATE TABLE IF NOT EXISTS conversation_compactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    start_turn_id INTEGER NOT NULL,
    end_turn_id INTEGER NOT NULL,
    summary TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_conversation_compactions_end_turn ON conversation_compactions(end_turn_id);
CREATE TABLE IF NOT EXISTS conversation_checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    label TEXT NOT NULL DEFAULT '',
    turn_id INTEGER NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_conversation_checkpoints_turn ON conversation_checkpoints(turn_id);
