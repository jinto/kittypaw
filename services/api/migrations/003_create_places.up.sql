-- Plan 6 PR-1: places + alias_overrides (Geo Resolve)
-- pg_trgm extension required — install once with superuser:
--   CREATE EXTENSION pg_trgm;
-- On RDS managed PostgreSQL, set parameter group: rds.extensions = 'pg_trgm'
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE places (
    id              BIGSERIAL PRIMARY KEY,
    name_ko         TEXT NOT NULL,
    aliases         TEXT[] NOT NULL DEFAULT '{}',
    lat             DOUBLE PRECISION NOT NULL,
    lon             DOUBLE PRECISION NOT NULL,
    type            TEXT NOT NULL,                          -- 'subway_station' | 'landmark'
    source          TEXT NOT NULL,                          -- 'wikidata' | 'kogl_seoul_metro'
    source_ref      TEXT,                                   -- Wikidata QID, station_id, etc.
    region          TEXT,
    source_priority INT  NOT NULL DEFAULT 0,                -- tiebreaker: seoul_metro=30, wikidata=20
    imported_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source, source_ref)
);

CREATE INDEX idx_places_name_ko_trgm ON places USING gin (name_ko gin_trgm_ops);
CREATE INDEX idx_places_aliases_gin  ON places USING gin (aliases);
CREATE INDEX idx_places_type         ON places (type);

CREATE TABLE alias_overrides (
    id          BIGSERIAL PRIMARY KEY,
    alias       TEXT NOT NULL UNIQUE,
    target_lat  DOUBLE PRECISION NOT NULL,
    target_lon  DOUBLE PRECISION NOT NULL,
    target_name TEXT NOT NULL,
    note        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
