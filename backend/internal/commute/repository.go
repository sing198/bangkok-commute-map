package commute

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tk/bangkok-commute-map/backend/internal/transit"
)

// NearestStation represents a station paired with its geographic distance in meters.
type NearestStation struct {
	Station   transit.Station `json:"station"`
	DistanceM float64         `json:"distance_m"`
}

// ListingRecord represents a condo/apartment row paired with a station access distance
// and live aggregated crowdsourced rent statistics.
type ListingRecord struct {
	ID                    string  `json:"id"`
	OSMID                 string  `json:"osm_id"`
	TitleTH               string  `json:"title_th"`
	TitleEN               string  `json:"title_en"`
	PropertyType          string  `json:"property_type"`
	District              string  `json:"district"`
	MonthlyRentTHB        int     `json:"monthly_rent_thb"`
	EstimatedRentMinTHB   int     `json:"estimated_rent_min_thb"`
	EstimatedRentMaxTHB   int     `json:"estimated_rent_max_thb"`
	PriceSource           string  `json:"price_source"`
	DataSource            string  `json:"data_source"`
	RoomSizeSqm           float64 `json:"room_size_sqm"`
	Bedrooms              int     `json:"bedrooms"`
	Lat                   float64 `json:"lat"`
	Lng                   float64 `json:"lng"`
	StationID             string  `json:"station_id"`
	WalkToStationM        float64 `json:"walk_to_station_m"`
	DistanceSource        string  `json:"distance_source"`
	VerifiedRentMedianTHB int     `json:"verified_rent_median_thb"`
	VerifiedReportCount   int     `json:"verified_report_count"`
	LatestReportMonth     string  `json:"latest_report_month"`
	LatestSourceURL       string  `json:"latest_source_url"`
}

// PriceReportRecord represents a single anonymized tenant-submitted rent report.
type PriceReportRecord struct {
	ID             int64     `json:"id"`
	ListingID      string    `json:"listing_id"`
	MonthlyRentTHB int       `json:"monthly_rent_thb"`
	RoomSizeSqm    float64   `json:"room_size_sqm"`
	RoomType       string    `json:"room_type"`
	ReportedMonth  string    `json:"reported_month"`
	Note           string    `json:"note,omitempty"`
	SourceURL      string    `json:"source_url,omitempty"`
	IsOutlier      bool      `json:"is_outlier"`
	CreatedAt      time.Time `json:"created_at"`
}

// VerifiedPriceStats holds aggregate statistics for non-outlier user reports on a listing.
type VerifiedPriceStats struct {
	MedianRentTHB     int    `json:"median_rent_thb"`
	MinRentTHB        int    `json:"min_rent_thb"`
	MaxRentTHB        int    `json:"max_rent_thb"`
	ReportCount       int    `json:"report_count"`
	LatestReportMonth string `json:"latest_report_month"`
	LatestSourceURL   string `json:"latest_source_url,omitempty"`
}

// ListingRentRef is used to verify listing existence and check outliers on report submission.
type ListingRentRef struct {
	ID                  string
	MonthlyRentTHB      int
	EstimatedRentMinTHB int
	EstimatedRentMaxTHB int
}

// Repository encapsulates all PostgreSQL / PostGIS queries for commute search and price reports.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new PostGIS-backed commute repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// LoadTransitGraph loads all stations, directed edges, and latest fare rules from PostgreSQL.
func (r *Repository) LoadTransitGraph(ctx context.Context) (*transit.Graph, error) {
	stationRows, err := r.pool.Query(ctx, `
		SELECT id, name_th, name_en, line_code, operator, is_extension,
		       ST_Y(location::geometry) AS lat,
		       ST_X(location::geometry) AS lng
		FROM stations
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query stations: %w", err)
	}
	defer stationRows.Close()

	var stations []transit.Station
	for stationRows.Next() {
		var s transit.Station
		if err := stationRows.Scan(&s.ID, &s.NameTH, &s.NameEN, &s.LineCode, &s.Operator, &s.IsExtension, &s.Lat, &s.Lng); err != nil {
			return nil, fmt.Errorf("scan station: %w", err)
		}
		stations = append(stations, s)
	}
	if err := stationRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stations: %w", err)
	}

	edgeRows, err := r.pool.Query(ctx, `
		SELECT from_station_id, to_station_id, travel_minutes, is_transfer
		FROM transit_edges
	`)
	if err != nil {
		return nil, fmt.Errorf("query transit_edges: %w", err)
	}
	defer edgeRows.Close()

	var edges []transit.Edge
	for edgeRows.Next() {
		var e transit.Edge
		if err := edgeRows.Scan(&e.From, &e.To, &e.TravelMinutes, &e.IsTransfer); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		edges = append(edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}

	fareRows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (operator, hops) operator, hops, fare_thb
		FROM fare_rules
		ORDER BY operator, hops, effective_date DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query fare_rules: %w", err)
	}
	defer fareRows.Close()

	var rules []transit.FareRule
	for fareRows.Next() {
		var f transit.FareRule
		if err := fareRows.Scan(&f.Operator, &f.Hops, &f.FareTHB); err != nil {
			return nil, fmt.Errorf("scan fare rule: %w", err)
		}
		rules = append(rules, f)
	}
	if err := fareRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fare rules: %w", err)
	}

	return transit.NewGraph(stations, edges, rules), nil
}

// FindNearestStations uses PostGIS GIST KNN (<->) to locate the closest transit stations to (lat, lng).
func (r *Repository) FindNearestStations(ctx context.Context, lat, lng float64, limit int) ([]NearestStation, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, name_th, name_en, line_code, operator, is_extension,
		       ST_Y(location::geometry) AS lat,
		       ST_X(location::geometry) AS lng,
		       ST_Distance(location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) AS distance_m
		FROM stations
		ORDER BY location <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
		LIMIT $3
	`, lng, lat, limit)
	if err != nil {
		return nil, fmt.Errorf("knn nearest stations: %w", err)
	}
	defer rows.Close()

	var out []NearestStation
	for rows.Next() {
		var ns NearestStation
		if err := rows.Scan(
			&ns.Station.ID, &ns.Station.NameTH, &ns.Station.NameEN,
			&ns.Station.LineCode, &ns.Station.Operator, &ns.Station.IsExtension,
			&ns.Station.Lat, &ns.Station.Lng, &ns.DistanceM,
		); err != nil {
			return nil, fmt.Errorf("scan nearest station: %w", err)
		}
		out = append(out, ns)
	}
	return out, rows.Err()
}

// FindListingsNearStations retrieves listings reachable from any of stationIDs using
// precomputed real OSRM street walking distances (listing_station_access) plus live
// crowdsourced median prices from price_reports.
func (r *Repository) FindListingsNearStations(ctx context.Context, stationIDs []string, maxWalkMeters int, maxBudget int) ([]ListingRecord, error) {
	if len(stationIDs) == 0 {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx, `
		WITH verified AS (
			SELECT
				listing_id,
				ROUND(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY monthly_rent_thb))::int AS median_rent,
				COUNT(*)::int AS report_count,
				MAX(reported_month) AS latest_month,
				(ARRAY_AGG(source_url ORDER BY created_at DESC) FILTER (WHERE source_url <> ''))[1] AS latest_url
			FROM price_reports
			WHERE NOT is_outlier
			GROUP BY listing_id
		)
		SELECT
			l.id, l.osm_id, l.title_th, l.title_en, l.property_type, l.district,
			l.monthly_rent_thb, l.estimated_rent_min_thb, l.estimated_rent_max_thb,
			l.price_source, l.data_source,
			l.room_size_sqm::double precision, l.bedrooms,
			ST_Y(l.location::geometry) AS lat,
			ST_X(l.location::geometry) AS lng,
			a.station_id,
			a.walk_distance_m::double precision AS walk_m,
			a.distance_source,
			COALESCE(v.median_rent, 0) AS verified_median,
			COALESCE(v.report_count, 0) AS verified_count,
			COALESCE(v.latest_month, '') AS latest_month,
			COALESCE(v.latest_url, '') AS latest_url
		FROM listing_station_access a
		JOIN listings l ON l.id = a.listing_id
		LEFT JOIN verified v ON v.listing_id = l.id
		WHERE a.station_id = ANY($1)
		  AND a.walk_distance_m <= $2
		  AND ($3 <= 0 OR v.median_rent IS NULL OR v.median_rent <= $3)
	`, stationIDs, maxWalkMeters, maxBudget)
	if err != nil {
		return nil, fmt.Errorf("query listings via osrm access: %w", err)
	}
	defer rows.Close()

	out, err := scanListingRecords(rows)
	if err != nil {
		return nil, err
	}
	if len(out) > 0 {
		return out, nil
	}

	// Graceful fallback if listing_station_access is empty (e.g. fresh minimal test DB)
	fbRows, err := r.pool.Query(ctx, `
		SELECT
			l.id, l.osm_id, l.title_th, l.title_en, l.property_type, l.district,
			l.monthly_rent_thb, l.estimated_rent_min_thb, l.estimated_rent_max_thb,
			l.price_source, l.data_source,
			l.room_size_sqm::double precision, l.bedrooms,
			ST_Y(l.location::geometry) AS lat,
			ST_X(l.location::geometry) AS lng,
			s.id AS station_id,
			ST_Distance(l.location, s.location) * 1.40 AS walk_m,
			'estimated_detour' AS distance_source,
			0 AS verified_median,
			0 AS verified_count,
			'' AS latest_month,
			'' AS latest_url
		FROM listings l
		JOIN stations s ON ST_DWithin(l.location, s.location, $1)
		WHERE s.id = ANY($2)
	`, float64(maxWalkMeters), stationIDs)
	if err != nil {
		return nil, fmt.Errorf("fallback st_dwithin listings query: %w", err)
	}
	defer fbRows.Close()
	return scanListingRecords(fbRows)
}

// FindListingsByIDsNearStations fetches specific listings paired with all precomputed OSRM
// station links (plus nearest_station_id fallback) and live crowdsourced rent statistics.
func (r *Repository) FindListingsByIDsNearStations(ctx context.Context, ids []string, maxWalkMeters int) ([]ListingRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		WITH verified AS (
			SELECT
				listing_id,
				ROUND(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY monthly_rent_thb))::int AS median_rent,
				COUNT(*)::int AS report_count,
				MAX(reported_month) AS latest_month,
				(ARRAY_AGG(source_url ORDER BY created_at DESC) FILTER (WHERE source_url <> ''))[1] AS latest_url
			FROM price_reports
			WHERE NOT is_outlier
			  AND listing_id = ANY($1)
			GROUP BY listing_id
		)
		SELECT
			l.id, l.osm_id, l.title_th, l.title_en, l.property_type, l.district,
			l.monthly_rent_thb, l.estimated_rent_min_thb, l.estimated_rent_max_thb,
			l.price_source, l.data_source,
			l.room_size_sqm::double precision, l.bedrooms,
			ST_Y(l.location::geometry) AS lat,
			ST_X(l.location::geometry) AS lng,
			a.station_id,
			a.walk_distance_m::double precision AS walk_m,
			a.distance_source,
			COALESCE(v.median_rent, 0) AS verified_median,
			COALESCE(v.report_count, 0) AS verified_count,
			COALESCE(v.latest_month, '') AS latest_month,
			COALESCE(v.latest_url, '') AS latest_url
		FROM listings l
		JOIN listing_station_access a ON a.listing_id = l.id
		LEFT JOIN verified v ON v.listing_id = l.id
		WHERE l.id = ANY($1)
		  AND (a.walk_distance_m <= $2 OR a.station_id = l.nearest_station_id)
	`, ids, maxWalkMeters)
	if err != nil {
		return nil, fmt.Errorf("query listings by ids via osrm access: %w", err)
	}
	defer rows.Close()

	out, err := scanListingRecords(rows)
	if err != nil {
		return nil, err
	}
	if len(out) > 0 {
		return out, nil
	}

	// Fallback if listing_station_access not populated
	fbRows, err := r.pool.Query(ctx, `
		SELECT
			l.id, l.osm_id, l.title_th, l.title_en, l.property_type, l.district,
			l.monthly_rent_thb, l.estimated_rent_min_thb, l.estimated_rent_max_thb,
			l.price_source, l.data_source,
			l.room_size_sqm::double precision, l.bedrooms,
			ST_Y(l.location::geometry) AS lat,
			ST_X(l.location::geometry) AS lng,
			s.id AS station_id,
			ST_Distance(l.location, s.location) * 1.40 AS walk_m,
			'estimated_detour' AS distance_source,
			0 AS verified_median,
			0 AS verified_count,
			'' AS latest_month,
			'' AS latest_url
		FROM listings l
		JOIN stations s ON ST_DWithin(l.location, s.location, $2) OR s.id = l.nearest_station_id
		WHERE l.id = ANY($1)
	`, ids, float64(maxWalkMeters))
	if err != nil {
		return nil, fmt.Errorf("fallback query listings by ids: %w", err)
	}
	defer fbRows.Close()
	return scanListingRecords(fbRows)
}

func scanListingRecords(rows pgx.Rows) ([]ListingRecord, error) {
	var out []ListingRecord
	for rows.Next() {
		var rec ListingRecord
		if err := rows.Scan(
			&rec.ID, &rec.OSMID, &rec.TitleTH, &rec.TitleEN, &rec.PropertyType, &rec.District,
			&rec.MonthlyRentTHB, &rec.EstimatedRentMinTHB, &rec.EstimatedRentMaxTHB,
			&rec.PriceSource, &rec.DataSource,
			&rec.RoomSizeSqm, &rec.Bedrooms,
			&rec.Lat, &rec.Lng,
			&rec.StationID, &rec.WalkToStationM, &rec.DistanceSource,
			&rec.VerifiedRentMedianTHB, &rec.VerifiedReportCount, &rec.LatestReportMonth,
			&rec.LatestSourceURL,
		); err != nil {
			return nil, fmt.Errorf("scan listing record: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetListingRentRef fetches a listing's reference rent range for report validation.
func (r *Repository) GetListingRentRef(ctx context.Context, listingID string) (ListingRentRef, bool, error) {
	var ref ListingRentRef
	err := r.pool.QueryRow(ctx, `
		SELECT id, monthly_rent_thb, estimated_rent_min_thb, estimated_rent_max_thb
		FROM listings
		WHERE id = $1
	`, listingID).Scan(&ref.ID, &ref.MonthlyRentTHB, &ref.EstimatedRentMinTHB, &ref.EstimatedRentMaxTHB)
	if err == pgx.ErrNoRows {
		return ListingRentRef{}, false, nil
	}
	if err != nil {
		return ListingRentRef{}, false, fmt.Errorf("get listing rent ref: %w", err)
	}
	return ref, true, nil
}

// CountRecentReportsByIP returns how many price reports an IP hash submitted in the last hour.
func (r *Repository) CountRecentReportsByIP(ctx context.Context, ipHash string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM price_reports
		WHERE ip_hash = $1
		  AND created_at >= NOW() - INTERVAL '1 hour'
	`, ipHash).Scan(&n)
	return n, err
}

// HasRecentListingReportByIP checks if this IP hash already reported for this listing in the last 30 days.
func (r *Repository) HasRecentListingReportByIP(ctx context.Context, listingID, ipHash string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM price_reports
			WHERE listing_id = $1
			  AND ip_hash = $2
			  AND created_at >= NOW() - INTERVAL '30 days'
		)
	`, listingID, ipHash).Scan(&exists)
	return exists, err
}

// InsertPriceReport stores a new tenant rent report (with optional source_url) and returns its created record.
func (r *Repository) InsertPriceReport(
	ctx context.Context,
	listingID string,
	monthlyRentTHB int,
	roomSizeSqm float64,
	roomType string,
	reportedMonth string,
	note string,
	sourceURL string,
	ipHash string,
	isOutlier bool,
) (PriceReportRecord, error) {
	var rec PriceReportRecord
	err := r.pool.QueryRow(ctx, `
		INSERT INTO price_reports (
			listing_id, monthly_rent_thb, room_size_sqm, room_type,
			reported_month, note, source_url, ip_hash, is_outlier
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, listing_id, monthly_rent_thb, room_size_sqm::double precision,
		          room_type, reported_month, note, source_url, is_outlier, created_at
	`, listingID, monthlyRentTHB, roomSizeSqm, roomType, reportedMonth, note, sourceURL, ipHash, isOutlier).Scan(
		&rec.ID, &rec.ListingID, &rec.MonthlyRentTHB, &rec.RoomSizeSqm,
		&rec.RoomType, &rec.ReportedMonth, &rec.Note, &rec.SourceURL, &rec.IsOutlier, &rec.CreatedAt,
	)
	if err != nil {
		return PriceReportRecord{}, fmt.Errorf("insert price report: %w", err)
	}
	return rec, nil
}

// GetVerifiedPriceStats computes aggregate median/min/max over non-outlier reports for a listing.
func (r *Repository) GetVerifiedPriceStats(ctx context.Context, listingID string) (VerifiedPriceStats, error) {
	var st VerifiedPriceStats
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(ROUND(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY monthly_rent_thb))::int, 0),
			COALESCE(MIN(monthly_rent_thb), 0),
			COALESCE(MAX(monthly_rent_thb), 0),
			COUNT(*)::int,
			COALESCE(MAX(reported_month), ''),
			COALESCE((ARRAY_AGG(source_url ORDER BY created_at DESC) FILTER (WHERE source_url <> ''))[1], '')
		FROM price_reports
		WHERE listing_id = $1
		  AND NOT is_outlier
	`, listingID).Scan(
		&st.MedianRentTHB, &st.MinRentTHB, &st.MaxRentTHB,
		&st.ReportCount, &st.LatestReportMonth, &st.LatestSourceURL,
	)
	return st, err
}

// ListPriceReports returns the most recent non-outlier tenant rent reports for a listing.
func (r *Repository) ListPriceReports(ctx context.Context, listingID string, limit int) ([]PriceReportRecord, error) {
	if limit <= 0 || limit > 50 {
		limit = 15
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, listing_id, monthly_rent_thb, room_size_sqm::double precision,
		       room_type, reported_month, note, source_url, is_outlier, created_at
		FROM price_reports
		WHERE listing_id = $1
		  AND NOT is_outlier
		ORDER BY reported_month DESC, created_at DESC
		LIMIT $2
	`, listingID, limit)
	if err != nil {
		return nil, fmt.Errorf("list price reports: %w", err)
	}
	defer rows.Close()

	var out []PriceReportRecord
	for rows.Next() {
		var rec PriceReportRecord
		if err := rows.Scan(
			&rec.ID, &rec.ListingID, &rec.MonthlyRentTHB, &rec.RoomSizeSqm,
			&rec.RoomType, &rec.ReportedMonth, &rec.Note, &rec.SourceURL, &rec.IsOutlier, &rec.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan price report: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
