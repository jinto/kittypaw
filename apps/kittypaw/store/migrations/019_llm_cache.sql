-- Generic LLM response cache; Phase 1 consumer is File.summary
-- (kind='file.summary'). Identity = (kind, key_hash, input_hash, model,
-- prompt_hash); first16hex is a 64-bit sha256 prefix — enough inside the
-- compound UNIQUE. See CLAUDE.md "File.summary + llm_cache" for the full
-- rationale (prompt_hash auto-invalidation, cross-tenant isolation).
CREATE TABLE IF NOT EXISTS llm_cache (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         TEXT NOT NULL,
    key_hash     TEXT NOT NULL,
    input_hash   TEXT NOT NULL,
    model        TEXT NOT NULL,
    prompt_hash  TEXT NOT NULL,
    result       TEXT NOT NULL,
    metadata     TEXT NOT NULL DEFAULT '{}',
    usage_input  INTEGER NOT NULL DEFAULT 0,
    usage_output INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (kind, key_hash, input_hash, model, prompt_hash)
);

-- Covers DeleteLLMCacheByKeyHash (GC on file remove) AND is a usable
-- prefix for the UNIQUE index lookup path (kind, key_hash, ...).
CREATE INDEX IF NOT EXISTS idx_llm_cache_key
    ON llm_cache (kind, key_hash);
