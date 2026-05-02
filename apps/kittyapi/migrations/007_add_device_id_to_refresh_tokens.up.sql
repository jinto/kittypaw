-- IMPORTANT: device_id must remain NULLABLE with NO DEFAULT. Adding a
-- non-NULL default (e.g. DEFAULT gen_random_uuid()) on this column would
-- trigger a full-table rewrite under AccessExclusiveLock — currently
-- safe only because the implicit NULL default is metadata-only (Postgres ≥11).
-- If you need to populate device_id, do it via UPDATE in a separate
-- migration, never via column DEFAULT.
ALTER TABLE refresh_tokens
  ADD COLUMN device_id UUID REFERENCES devices(id) ON DELETE CASCADE;

CREATE INDEX idx_refresh_tokens_device_id
  ON refresh_tokens(device_id) WHERE device_id IS NOT NULL;
