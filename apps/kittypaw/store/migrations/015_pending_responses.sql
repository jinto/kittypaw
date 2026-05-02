CREATE TABLE IF NOT EXISTS pending_responses (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type  TEXT    NOT NULL,
    chat_id     TEXT    NOT NULL,
    response    TEXT    NOT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    next_retry  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_pending_responses_next_retry
    ON pending_responses (next_retry);
