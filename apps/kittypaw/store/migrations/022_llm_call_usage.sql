CREATE TABLE IF NOT EXISTS llm_call_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    call_kind TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_input_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd REAL NOT NULL DEFAULT 0,
    pricing_source TEXT NOT NULL DEFAULT '',
    pricing_matched INTEGER NOT NULL DEFAULT 0,
    usage_json TEXT
);

CREATE INDEX IF NOT EXISTS idx_llm_call_usage_started ON llm_call_usage(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_call_usage_model ON llm_call_usage(model, started_at DESC);
