CREATE TABLE devices (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name               TEXT,
    capabilities       JSONB NOT NULL DEFAULT '{}'::jsonb
                       CHECK (jsonb_typeof(capabilities) = 'object'),
    paired_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at       TIMESTAMPTZ,
    last_connected_at  TIMESTAMPTZ,
    revoked_at         TIMESTAMPTZ
);

CREATE INDEX idx_devices_user_id ON devices(user_id) WHERE revoked_at IS NULL;
