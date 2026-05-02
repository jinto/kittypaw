-- Rename pending_responses.tenant_id → account_id alongside the
-- whole-tree tenant→account rename. modernc.org/sqlite ships SQLite 3.45+
-- which supports RENAME COLUMN.
ALTER TABLE pending_responses RENAME COLUMN tenant_id TO account_id;

DROP INDEX IF EXISTS idx_pending_responses_tenant;
CREATE INDEX IF NOT EXISTS idx_pending_responses_account
    ON pending_responses (account_id);
