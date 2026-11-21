package commute

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tk/bangkok-commute-map/backend/internal/transit"
)

// NearestStation represents a station paired with its geographic distance in meters.
type NearestStation struct {
	Station   transit.Station `json:"station"`
	DistanceM float64         `json:"distance_m"`
}

// ListingRecord represents a condo/apartment row fetched from PostGIS.
type ListingRecord struct {
	ID               string  `json:"id"`
	TitleTH          string  `json:"title_th"`
	TitleEN          string  `json:"title_en"`
	PropertyType     string  `json:"property_type"`
	District         string  `json:"district"`
	MonthlyRentTHB   int     `json:"monthly_rent_thb"`
	RoomSizeSqm      float64 `json:"room_size_sqm"`
	Bedrooms         int     `json:"bedrooms"`
	Lat              float64 `json:"lat"`
	Lng              float64 `json:"lng"`
	StationID        string  `json:"station_id"`
	WalkToStationM   float64 `json:"walk_to_station_m"`
}

// Repository encapsulates all PostgreSQL / PostGIS queries for commute search.
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

// FindListingsNearStations uses PostGIS ST_DWithin to find all listings within radiusMeters
// of any reachable station whose monthly rent is <= maxBudget (when maxBudget > 0).
func (r *Repository) FindListingsNearStations(ctx context.Context, stationIDs []string, radiusMeters int, maxBudget int) ([]ListingRecord, error) {
	if len(stationIDs) == 0 {
		return nil, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT l.id, l.title_th, l.title_en, l.property_type, l.district,
		       l.monthly_rent_thb, l.room_size_sqm::double precision, l.bedrooms,
		       ST_Y(l.location::geometry) AS lat,
		       ST_X(l.location::geometry) AS lng,
		       s.id AS station_id,
		       ST_Distance(l.location, s.location) AS walk_m
		FROM listings l
		JOIN stations s ON ST_DWithin(l.location, s.location, $1)
		WHERE s.id = ANY($2)
		  AND ($3 <= 0 OR l.monthly_rent_thb <= $3)
	`, float64(radiusMeters), stationIDs, maxBudget)
	if err != nil {
		return nil, fmt.Errorf("st_dwithin listings query: %w", err)
	}
	defer rows.Close()

	var out []ListingRecord
	for rows.Next() {
		var rec ListingRecord
		if err := rows.Scan(
			&rec.ID, &rec.TitleTH, &rec.TitleEN, &rec.PropertyType, &rec.District,
			&rec.MonthlyRentTHB, &rec.RoomSizeSqm, &rec.Bedrooms,
			&rec.Lat, &rec.Lng, &rec.StationID, &rec.WalkToStationM,
		); err != nil {
			return nil, fmt.Errorf("scan listing: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// FindListingsByIDs fetches specific listings along with their precomputed nearest station.
func (r *Repository) FindListingsByIDs(ctx context.Context, ids []string) ([]ListingRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT l.id, l.title_th, l.title_en, l.property_type, l.district,
		       l.monthly_rent_thb, l.room_size_sqm::double precision, l.bedrooms,
		       ST_Y(l.location::geometry) AS lat,
		       ST_X(l.location::geometry) AS lng,
		       COALESCE(l.nearest_station_id, '') AS station_id,
		       COALESCE(l.walk_to_station_m, 0)::double precision AS walk_m
		FROM listings l
		WHERE l.id = ANY($1)
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("query listings by ids: %w", err)
	}
	defer rows.Close()

	var out []ListingRecord
	for rows.Next() {
		var rec ListingRecord
		if err := rows.Scan(
			&rec.ID, &rec.TitleTH, &rec.TitleEN, &rec.PropertyType, &rec.District,
			&rec.MonthlyRentTHB, &rec.RoomSizeSqm, &rec.Bedrooms,
			&rec.Lat, &rec.Lng, &rec.StationID, &rec.WalkToStationM,
		); err != nil {
			return nil, fmt.Errorf("scan listing by id: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
