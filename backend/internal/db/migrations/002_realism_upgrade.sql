-- 002_realism_upgrade.sql
-- Expand Bangkok transit network to all operating rail operators,
-- store precomputed OSRM real street walking distances,
-- and add honest estimated price ranges + crowdsourced tenant rent reports.

-- 1. Expand operator constraints to cover all operating Greater Bangkok rail lines
ALTER TABLE stations DROP CONSTRAINT IF EXISTS stations_operator_check;
ALTER TABLE stations
    ADD CONSTRAINT stations_operator_check
    CHECK (operator IN ('BTS', 'BTS_GOLD', 'MRT', 'MRT_YL', 'MRT_PK', 'ARL', 'SRT_RED'));

ALTER TABLE fare_rules DROP CONSTRAINT IF EXISTS fare_rules_operator_check;
ALTER TABLE fare_rules
    ADD CONSTRAINT fare_rules_operator_check
    CHECK (operator IN ('BTS', 'BTS_GOLD', 'MRT', 'MRT_YL', 'MRT_PK', 'ARL', 'SRT_RED'));

ALTER TABLE fare_rules
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '';

-- 2. Enrich listings with OSM provenance & market-range estimate columns
ALTER TABLE listings
    ADD COLUMN IF NOT EXISTS osm_id                 TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS estimated_rent_min_thb INT  NOT NULL DEFAULT 0 CHECK (estimated_rent_min_thb >= 0),
    ADD COLUMN IF NOT EXISTS estimated_rent_max_thb INT  NOT NULL DEFAULT 0 CHECK (estimated_rent_max_thb >= 0),
    ADD COLUMN IF NOT EXISTS price_source           TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS data_source            TEXT NOT NULL DEFAULT 'osm';

-- 3. Precomputed real street/soi walking distances (OSRM foot profile) per listing <-> station
CREATE TABLE IF NOT EXISTS listing_station_access (
    listing_id      TEXT NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    station_id      TEXT NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
    walk_distance_m INT  NOT NULL CHECK (walk_distance_m >= 0),
    walk_minutes    INT  NOT NULL CHECK (walk_minutes >= 0),
    straight_line_m INT  NOT NULL CHECK (straight_line_m >= 0),
    distance_source TEXT NOT NULL DEFAULT 'osrm_foot' CHECK (distance_source IN ('osrm_foot', 'estimated_detour')),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (listing_id, station_id)
);

CREATE INDEX IF NOT EXISTS idx_lsa_station_dist ON listing_station_access (station_id, walk_distance_m);
CREATE INDEX IF NOT EXISTS idx_lsa_listing ON listing_station_access (listing_id);

-- 4. Crowdsourced real tenant rent reports with outlier flagging and IP-hash rate limiting
CREATE TABLE IF NOT EXISTS price_reports (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    listing_id       TEXT NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
    monthly_rent_thb INT  NOT NULL CHECK (monthly_rent_thb BETWEEN 1500 AND 250000),
    room_size_sqm    NUMERIC(5, 1) NOT NULL CHECK (room_size_sqm BETWEEN 10 AND 300),
    room_type        TEXT NOT NULL CHECK (room_type IN ('studio', '1br', '2br', 'dorm')),
    reported_month   TEXT NOT NULL CHECK (reported_month ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    note             TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 200),
    ip_hash          TEXT NOT NULL,
    is_outlier       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_price_reports_listing_valid ON price_reports (listing_id, is_outlier, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_price_reports_rate_limit ON price_reports (ip_hash, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_price_reports_dedupe ON price_reports (listing_id, ip_hash, created_at DESC);

-- Enable RLS on new public tables (Go backend connects directly via Postgres credentials)
ALTER TABLE listing_station_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_reports ENABLE ROW LEVEL SECURITY;
