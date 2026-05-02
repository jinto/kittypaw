-- Plan 6 PR-2: addresses (행안부 도로명주소)
-- Depends on pg_trgm extension (created in 003_create_places.up.sql).
-- Holds ~10M rows after first juso seed; sized for gin_trgm fuzzy search
-- on the normalized road address + building name.

CREATE TABLE addresses (
    id                      BIGSERIAL PRIMARY KEY,
    road_address            TEXT NOT NULL,                  -- "서울특별시 강남구 테헤란로 152"
    road_address_normalized TEXT NOT NULL,                  -- NFC + 시도 약어 통일 + trim
    jibun_address           TEXT,                           -- "역삼동 825-22"
    building_name           TEXT,
    lat                     DOUBLE PRECISION NOT NULL,
    lon                     DOUBLE PRECISION NOT NULL,
    pnu                     TEXT NOT NULL,                  -- 행안부 PNU (필지고유번호) — UPSERT 키
    region_sido             TEXT NOT NULL,                  -- "서울특별시"
    region_sigungu          TEXT NOT NULL,                  -- "강남구"
    imported_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pnu)
);

-- Index naming follows 003_create_places convention: idx_<table>_<column>[_<type>].
-- Full column names (no abbreviation) for grep-ability and future consistency.

-- gin_trgm_ops on the normalized form: matches "서울 강남구..." and
-- "서울특별시 강남구..." consistently after the seeder normalizes input.
CREATE INDEX idx_addresses_road_address_normalized_trgm
    ON addresses USING gin (road_address_normalized gin_trgm_ops);

-- Partial index — most addresses have no building_name.
CREATE INDEX idx_addresses_building_name_trgm
    ON addresses USING gin (building_name gin_trgm_ops)
    WHERE building_name IS NOT NULL;

-- Composite index for region-prefixed search (e.g. "서울특별시 강남구").
CREATE INDEX idx_addresses_region
    ON addresses (region_sido, region_sigungu);
