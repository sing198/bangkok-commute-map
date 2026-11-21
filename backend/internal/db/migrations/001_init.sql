-- Enable PostGIS extension for geographic spatial indexing and distance calculations
CREATE EXTENSION IF NOT EXISTS postgis;

-- 1. Transit stations (BTS, MRT, ARL)
CREATE TABLE IF NOT EXISTS stations (
    id           TEXT PRIMARY KEY,
    name_th      TEXT NOT NULL,
    name_en      TEXT NOT NULL,
    line_code    TEXT NOT NULL,
    operator     TEXT NOT NULL CHECK (operator IN ('BTS', 'MRT', 'ARL')),
    is_extension BOOLEAN NOT NULL DEFAULT FALSE,
    location     GEOGRAPHY(Point, 4326) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_stations_location ON stations USING GIST (location);
CREATE INDEX IF NOT EXISTS idx_stations_operator ON stations (operator);
CREATE INDEX IF NOT EXISTS idx_stations_line_code ON stations (line_code);

-- 2. Directed edges between stations (rail segments and interchange transfers)
CREATE TABLE IF NOT EXISTS transit_edges (
    from_station_id TEXT NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
    to_station_id   TEXT NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
    travel_minutes  INT  NOT NULL CHECK (travel_minutes > 0),
    is_transfer     BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (from_station_id, to_station_id)
);

CREATE INDEX IF NOT EXISTS idx_transit_edges_from ON transit_edges (from_station_id);
CREATE INDEX IF NOT EXISTS idx_transit_edges_to ON transit_edges (to_station_id);

-- 3. Operator fare tables (updatable via seed pipeline without code changes)
CREATE TABLE IF NOT EXISTS fare_rules (
    operator       TEXT NOT NULL CHECK (operator IN ('BTS', 'MRT', 'ARL')),
    hops           INT  NOT NULL CHECK (hops >= 0),
    fare_thb       INT  NOT NULL CHECK (fare_thb >= 0),
    effective_date DATE NOT NULL,
    PRIMARY KEY (operator, hops, effective_date)
);

-- 4. Curated Bangkok condo & apartment listings
CREATE TABLE IF NOT EXISTS listings (
    id                 TEXT PRIMARY KEY,
    title_th           TEXT NOT NULL,
    title_en           TEXT NOT NULL,
    property_type      TEXT NOT NULL CHECK (property_type IN ('condo', 'apartment')),
    district           TEXT NOT NULL,
    monthly_rent_thb   INT  NOT NULL CHECK (monthly_rent_thb > 0),
    room_size_sqm      NUMERIC(5, 1) NOT NULL CHECK (room_size_sqm > 0),
    bedrooms           INT  NOT NULL CHECK (bedrooms >= 0),
    location           GEOGRAPHY(Point, 4326) NOT NULL,
    nearest_station_id TEXT REFERENCES stations(id) ON DELETE SET NULL,
    walk_to_station_m  INT  CHECK (walk_to_station_m >= 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_listings_location ON listings USING GIST (location);
CREATE INDEX IF NOT EXISTS idx_listings_nearest_station ON listings (nearest_station_id);
CREATE INDEX IF NOT EXISTS idx_listings_rent ON listings (monthly_rent_thb);
