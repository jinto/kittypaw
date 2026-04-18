-- Add tenant_id to pending_responses for multi-tenant retry routing.
-- Default '' preserves legacy rows; dispatchLoop treats '' as the default tenant
-- via DefaultTenantID so pre-migration queued responses still deliver.
ALTER TABLE pending_responses
    ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_pending_responses_tenant
    ON pending_responses (tenant_id);
