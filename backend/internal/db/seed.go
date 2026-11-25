package db

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
)

const seedAdvisoryLockKey int64 = 884202602

type edgeInput struct {
	From          string `json:"from"`
	To            string `json:"to"`
	TravelMinutes int    `json:"travel_minutes"`
	IsTransfer    bool   `json:"is_transfer"`
}

// EnsureSeeded checks whether the database already has the full Bangkok rail network,
// precomputed OSRM walking distances, and verified building/station portal URLs; if not,
// it applies the embedded dataset idempotently inside a single advisory-locked transaction.
func EnsureSeeded(ctx context.Context, pool *pgxpool.Pool, dataFS fs.FS) (bool, error) {
	var stationCount, accessCount, listingURLCount, stationURLCount int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*)::int FROM stations),
			(SELECT COUNT(*)::int FROM listing_station_access),
			(SELECT COUNT(*)::int FROM listings WHERE listing_url <> ''),
			(SELECT COUNT(*)::int FROM stations WHERE portal_condo_url <> '' OR portal_apt_url <> '')
	`).Scan(&stationCount, &accessCount, &listingURLCount, &stationURLCount); err != nil {
		return false, fmt.Errorf("check seed status: %w", err)
	}
	if stationCount >= 190 && accessCount >= 4000 && listingURLCount >= 100 && stationURLCount >= 50 {
		return false, nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin seed tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize concurrent serverless cold starts safely
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, seedAdvisoryLockKey); err != nil {
		return false, fmt.Errorf("acquire seed advisory lock: %w", err)
	}

	// Re-check inside lock
	if err := tx.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*)::int FROM stations),
			(SELECT COUNT(*)::int FROM listing_station_access),
			(SELECT COUNT(*)::int FROM listings WHERE listing_url <> ''),
			(SELECT COUNT(*)::int FROM stations WHERE portal_condo_url <> '' OR portal_apt_url <> '')
	`).Scan(&stationCount, &accessCount, &listingURLCount, &stationURLCount); err != nil {
		return false, fmt.Errorf("recheck seed status: %w", err)
	}
	if stationCount >= 190 && accessCount >= 4000 && listingURLCount >= 100 && stationURLCount >= 50 {
		return false, nil
	}

	stationsJSON, err := fs.ReadFile(dataFS, "stations.json")
	if err != nil {
		return false, fmt.Errorf("read stations.json: %w", err)
	}
	edgesRaw, err := fs.ReadFile(dataFS, "edges.json")
	if err != nil {
		return false, fmt.Errorf("read edges.json: %w", err)
	}
	faresJSON, err := fs.ReadFile(dataFS, "fare_rules.json")
	if err != nil {
		return false, fmt.Errorf("read fare_rules.json: %w", err)
	}
	listingsJSON, err := fs.ReadFile(dataFS, "listings.json")
	if err != nil {
		return false, fmt.Errorf("read listings.json: %w", err)
	}
	accessJSON, err := fs.ReadFile(dataFS, "listing_station_access.json")
	if err != nil {
		return false, fmt.Errorf("read listing_station_access.json: %w", err)
	}

	// Expand undirected edges into directed pairs
	var undirected []edgeInput
	if err := json.Unmarshal(edgesRaw, &undirected); err != nil {
		return false, fmt.Errorf("parse edges.json: %w", err)
	}
	directed := make([]edgeInput, 0, len(undirected)*2)
	for _, e := range undirected {
		directed = append(directed, e, edgeInput{
			From:          e.To,
			To:            e.From,
			TravelMinutes: e.TravelMinutes,
			IsTransfer:    e.IsTransfer,
		})
	}
	directedEdgesJSON, err := json.Marshal(directed)
	if err != nil {
		return false, fmt.Errorf("marshal directed edges: %w", err)
	}

	// 1. Upsert all stations (including verified station-level portal URLs)
	if _, err := tx.Exec(ctx, `
		INSERT INTO stations (id, name_th, name_en, line_code, operator, is_extension, location, portal_condo_url, portal_apt_url, updated_at)
		SELECT x.id, x.name_th, x.name_en, x.line_code, x.operator, x.is_extension,
		       ST_SetSRID(ST_MakePoint(x.lng, x.lat), 4326)::geography,
		       COALESCE(x.portal_condo_url, ''), COALESCE(x.portal_apt_url, ''), NOW()
		FROM jsonb_to_recordset($1::jsonb) AS x(
			id text, name_th text, name_en text, line_code text, operator text,
			is_extension boolean, lat double precision, lng double precision,
			portal_condo_url text, portal_apt_url text
		)
		ON CONFLICT (id) DO UPDATE SET
			name_th          = EXCLUDED.name_th,
			name_en          = EXCLUDED.name_en,
			line_code        = EXCLUDED.line_code,
			operator         = EXCLUDED.operator,
			is_extension     = EXCLUDED.is_extension,
			location         = EXCLUDED.location,
			portal_condo_url = EXCLUDED.portal_condo_url,
			portal_apt_url   = EXCLUDED.portal_apt_url,
			updated_at       = NOW()
	`, string(stationsJSON)); err != nil {
		return false, fmt.Errorf("upsert stations: %w", err)
	}

	// 2. Replace transit edges cleanly so stale pre-upgrade edges are removed
	if _, err := tx.Exec(ctx, `DELETE FROM transit_edges`); err != nil {
		return false, fmt.Errorf("clear transit_edges: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transit_edges (from_station_id, to_station_id, travel_minutes, is_transfer)
		SELECT x."from", x."to", x.travel_minutes, x.is_transfer
		FROM jsonb_to_recordset($1::jsonb) AS x(
			"from" text, "to" text, travel_minutes int, is_transfer boolean
		)
		ON CONFLICT (from_station_id, to_station_id) DO UPDATE SET
			travel_minutes = EXCLUDED.travel_minutes,
			is_transfer    = EXCLUDED.is_transfer
	`, string(directedEdgesJSON)); err != nil {
		return false, fmt.Errorf("upsert transit_edges: %w", err)
	}

	// 3. Upsert fare rules
	if _, err := tx.Exec(ctx, `
		INSERT INTO fare_rules (operator, hops, fare_thb, effective_date, source)
		SELECT x.operator, x.hops, x.fare_thb, x.effective_date::date, COALESCE(x.source, '')
		FROM jsonb_to_recordset($1::jsonb) AS x(
			operator text, hops int, fare_thb int, effective_date text, source text
		)
		ON CONFLICT (operator, hops, effective_date) DO UPDATE SET
			fare_thb = EXCLUDED.fare_thb,
			source   = EXCLUDED.source
	`, string(faresJSON)); err != nil {
		return false, fmt.Errorf("upsert fare_rules: %w", err)
	}

	// 4. Upsert all real OSM + curated listings (including verified direct building rental URLs)
	if _, err := tx.Exec(ctx, `
		INSERT INTO listings (
			id, osm_id, title_th, title_en, property_type, district,
			monthly_rent_thb, estimated_rent_min_thb, estimated_rent_max_thb,
			price_source, data_source, listing_url, listing_url_label,
			room_size_sqm, bedrooms,
			location, nearest_station_id, walk_to_station_m, updated_at
		)
		SELECT
			x.id, COALESCE(x.osm_id, ''), x.title_th, x.title_en, x.property_type, x.district,
			x.monthly_rent_thb,
			CASE WHEN COALESCE(x.estimated_rent_min_thb, 0) > 0 THEN x.estimated_rent_min_thb ELSE x.monthly_rent_thb END,
			CASE WHEN COALESCE(x.estimated_rent_max_thb, 0) > 0 THEN x.estimated_rent_max_thb ELSE x.monthly_rent_thb END,
			COALESCE(x.price_source, ''), COALESCE(NULLIF(x.source, ''), 'osm'),
			COALESCE(x.listing_url, ''), COALESCE(x.listing_url_label, ''),
			x.room_size_sqm, x.bedrooms,
			ST_SetSRID(ST_MakePoint(x.lng, x.lat), 4326)::geography,
			x.nearest_station_id, x.walk_to_station_m, NOW()
		FROM jsonb_to_recordset($1::jsonb) AS x(
			id text, osm_id text, title_th text, title_en text, property_type text, district text,
			monthly_rent_thb int, estimated_rent_min_thb int, estimated_rent_max_thb int,
			price_source text, source text, listing_url text, listing_url_label text,
			room_size_sqm numeric, bedrooms int,
			lat double precision, lng double precision,
			nearest_station_id text, walk_to_station_m int
		)
		ON CONFLICT (id) DO UPDATE SET
			osm_id                 = EXCLUDED.osm_id,
			title_th               = EXCLUDED.title_th,
			title_en               = EXCLUDED.title_en,
			property_type          = EXCLUDED.property_type,
			district               = EXCLUDED.district,
			monthly_rent_thb       = EXCLUDED.monthly_rent_thb,
			estimated_rent_min_thb = EXCLUDED.estimated_rent_min_thb,
			estimated_rent_max_thb = EXCLUDED.estimated_rent_max_thb,
			price_source           = EXCLUDED.price_source,
			data_source            = EXCLUDED.data_source,
			listing_url            = EXCLUDED.listing_url,
			listing_url_label      = EXCLUDED.listing_url_label,
			room_size_sqm          = EXCLUDED.room_size_sqm,
			bedrooms               = EXCLUDED.bedrooms,
			location               = EXCLUDED.location,
			nearest_station_id     = EXCLUDED.nearest_station_id,
			walk_to_station_m      = EXCLUDED.walk_to_station_m,
			updated_at             = NOW()
	`, string(listingsJSON)); err != nil {
		return false, fmt.Errorf("upsert listings: %w", err)
	}

	// Remove legacy 3-digit listing IDs (LST_001..LST_183) if present without touching user price reports on new IDs
	if _, err := tx.Exec(ctx, `DELETE FROM listings WHERE id ~ '^LST_[0-9]{3}$'`); err != nil {
		return false, fmt.Errorf("prune legacy listings: %w", err)
	}

	// 5. Upsert all precomputed OSRM real walking distance links
	if _, err := tx.Exec(ctx, `DELETE FROM listing_station_access`); err != nil {
		return false, fmt.Errorf("clear listing_station_access: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO listing_station_access (
			listing_id, station_id, walk_distance_m, walk_minutes, straight_line_m, distance_source, updated_at
		)
		SELECT
			x.listing_id, x.station_id, x.walk_distance_m, x.walk_minutes, x.straight_line_m,
			COALESCE(NULLIF(x.distance_source, ''), 'osrm_foot'), NOW()
		FROM jsonb_to_recordset($1::jsonb) AS x(
			listing_id text, station_id text, walk_distance_m int, walk_minutes int,
			straight_line_m int, distance_source text
		)
		ON CONFLICT (listing_id, station_id) DO UPDATE SET
			walk_distance_m = EXCLUDED.walk_distance_m,
			walk_minutes    = EXCLUDED.walk_minutes,
			straight_line_m = EXCLUDED.straight_line_m,
			distance_source = EXCLUDED.distance_source,
			updated_at      = NOW()
	`, string(accessJSON)); err != nil {
		return false, fmt.Errorf("upsert listing_station_access: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit seed tx: %w", err)
	}
	return true, nil
}
