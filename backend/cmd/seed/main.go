package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/tk/bangkok-commute-map/backend/internal/cache"
	"github.com/tk/bangkok-commute-map/backend/internal/config"
	"github.com/tk/bangkok-commute-map/backend/internal/db"
)

type StationInput struct {
	ID          string  `json:"id"`
	NameTH      string  `json:"name_th"`
	NameEN      string  `json:"name_en"`
	LineCode    string  `json:"line_code"`
	Operator    string  `json:"operator"`
	IsExtension bool    `json:"is_extension"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

type EdgeInput struct {
	From          string `json:"from"`
	To            string `json:"to"`
	TravelMinutes int    `json:"travel_minutes"`
	IsTransfer    bool   `json:"is_transfer"`
}

type FareRuleInput struct {
	Operator      string `json:"operator"`
	Hops          int    `json:"hops"`
	FareTHB       int    `json:"fare_thb"`
	EffectiveDate string `json:"effective_date"`
	Source        string `json:"source,omitempty"`
}

type ListingInput struct {
	ID                  string  `json:"id"`
	OSMID               string  `json:"osm_id,omitempty"`
	TitleTH             string  `json:"title_th"`
	TitleEN             string  `json:"title_en"`
	PropertyType        string  `json:"property_type"`
	District            string  `json:"district"`
	MonthlyRentTHB      int     `json:"monthly_rent_thb"`
	EstimatedRentMinTHB int     `json:"estimated_rent_min_thb"`
	EstimatedRentMaxTHB int     `json:"estimated_rent_max_thb"`
	PriceSource         string  `json:"price_source,omitempty"`
	RoomSizeSqm         float64 `json:"room_size_sqm"`
	Bedrooms            int     `json:"bedrooms"`
	Lat                 float64 `json:"lat"`
	Lng                 float64 `json:"lng"`
	NearestStationID    string  `json:"nearest_station_id,omitempty"`
	WalkToStationM      int     `json:"walk_to_station_m,omitempty"`
	Source              string  `json:"source,omitempty"`
}

type AccessInput struct {
	ListingID      string `json:"listing_id"`
	StationID      string `json:"station_id"`
	WalkDistanceM  int    `json:"walk_distance_m"`
	WalkMinutes    int    `json:"walk_minutes"`
	StraightLineM  int    `json:"straight_line_m"`
	DistanceSource string `json:"distance_source"`
}

type Dataset struct {
	Stations  []StationInput
	Edges     []EdgeInput
	FareRules []FareRuleInput
	Listings  []ListingInput
	Access    []AccessInput
}

func main() {
	dataDir := flag.String("data-dir", "./data", "directory containing stations.json, edges.json, fare_rules.json, listings.json, listing_station_access.json")
	runMigrate := flag.Bool("migrate", true, "run embedded SQL migrations before seeding")
	validateOnly := flag.Bool("validate-only", false, "validate JSON datasets and exit without connecting to DB")
	dumpSQL := flag.String("dump-sql", "", "write idempotent PostgreSQL seed SQL to this path and exit")
	flag.Parse()

	ds, err := loadDataset(*dataDir)
	if err != nil {
		log.Fatalf("load dataset: %v", err)
	}
	if err := validateDataset(ds); err != nil {
		log.Fatalf("validate dataset: %v", err)
	}

	log.Printf("dataset valid: %d stations, %d undirected edges (%d directed), %d fare rules, %d listings, %d OSRM walking links",
		len(ds.Stations), len(ds.Edges), len(ds.Edges)*2, len(ds.FareRules), len(ds.Listings), len(ds.Access))

	if *dumpSQL != "" {
		if err := writeSeedSQL(*dumpSQL, ds); err != nil {
			log.Fatalf("dump sql: %v", err)
		}
		log.Printf("wrote idempotent seed SQL to %s", *dumpSQL)
		return
	}

	if *validateOnly {
		log.Println("validation-only mode completed successfully")
		return
	}

	cfg := config.Load()
	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	if *runMigrate {
		if err := db.Migrate(ctx, pool); err != nil {
			log.Fatalf("run migrations: %v", err)
		}
		log.Println("migrations applied")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := upsertStations(ctx, tx, ds.Stations); err != nil {
		log.Fatalf("upsert stations: %v", err)
	}
	if err := upsertEdges(ctx, tx, ds.Edges); err != nil {
		log.Fatalf("upsert edges: %v", err)
	}
	if err := upsertFareRules(ctx, tx, ds.FareRules); err != nil {
		log.Fatalf("upsert fare rules: %v", err)
	}
	if err := upsertListings(ctx, tx, ds.Listings); err != nil {
		log.Fatalf("upsert listings: %v", err)
	}
	if err := upsertListingStationAccess(ctx, tx, ds.Access); err != nil {
		log.Fatalf("upsert listing_station_access: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit tx: %v", err)
	}

	log.Printf("seeded database successfully: %d stations, %d directed edges, %d fare rules, %d listings, %d OSRM links",
		len(ds.Stations), len(ds.Edges)*2, len(ds.FareRules), len(ds.Listings), len(ds.Access))

	cacheClient := cache.NewFromOptions(cache.Options{
		URL:       cfg.RedisURL,
		RestURL:   cfg.RedisRestURL,
		RestToken: cfg.RedisRestToken,
		Addr:      cfg.RedisAddr,
		Password:  cfg.RedisPassword,
		DB:        cfg.RedisDB,
	})
	defer func() { _ = cacheClient.Close() }()
	if err := cacheClient.Ping(ctx); err != nil {
		log.Printf("redis unavailable (%v); skipped cache invalidation", err)
	} else {
		deleted, err := cacheClient.DeleteByPrefix(ctx, "commute:")
		if err != nil {
			log.Printf("warning: failed to invalidate redis cache: %v", err)
		} else {
			log.Printf("invalidated %d cached commute entries in redis", deleted)
		}
	}
}

func loadDataset(dir string) (*Dataset, error) {
	ds := &Dataset{}
	if err := readJSONFile(filepath.Join(dir, "stations.json"), &ds.Stations); err != nil {
		return nil, fmt.Errorf("stations.json: %w", err)
	}
	if err := readJSONFile(filepath.Join(dir, "edges.json"), &ds.Edges); err != nil {
		return nil, fmt.Errorf("edges.json: %w", err)
	}
	if err := readJSONFile(filepath.Join(dir, "fare_rules.json"), &ds.FareRules); err != nil {
		return nil, fmt.Errorf("fare_rules.json: %w", err)
	}
	if err := readJSONFile(filepath.Join(dir, "listings.json"), &ds.Listings); err != nil {
		return nil, fmt.Errorf("listings.json: %w", err)
	}
	accessPath := filepath.Join(dir, "listing_station_access.json")
	if _, err := os.Stat(accessPath); err == nil {
		if err := readJSONFile(accessPath, &ds.Access); err != nil {
			return nil, fmt.Errorf("listing_station_access.json: %w", err)
		}
	}
	return ds, nil
}

func readJSONFile(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func validateDataset(ds *Dataset) error {
	if len(ds.Stations) == 0 {
		return fmt.Errorf("stations.json is empty")
	}

	validOps := map[string]bool{
		"BTS":      true,
		"BTS_GOLD": true,
		"MRT":      true,
		"MRT_YL":   true,
		"MRT_PK":   true,
		"ARL":      true,
		"SRT_RED":  true,
	}
	stationSet := make(map[string]StationInput, len(ds.Stations))

	for _, s := range ds.Stations {
		if s.ID == "" || s.NameTH == "" || s.NameEN == "" {
			return fmt.Errorf("station missing required fields: %+v", s)
		}
		if _, dup := stationSet[s.ID]; dup {
			return fmt.Errorf("duplicate station id: %s", s.ID)
		}
		if !validOps[s.Operator] {
			return fmt.Errorf("station %s has invalid operator %q", s.ID, s.Operator)
		}
		if s.Lat < 13.45 || s.Lat > 14.12 || s.Lng < 100.25 || s.Lng > 100.98 {
			return fmt.Errorf("station %s coordinates out of Greater Bangkok bounds: (%.4f, %.4f)", s.ID, s.Lat, s.Lng)
		}
		stationSet[s.ID] = s
	}

	adj := make(map[string][]string, len(ds.Stations))
	for _, e := range ds.Edges {
		if _, ok := stationSet[e.From]; !ok {
			return fmt.Errorf("edge references unknown from station: %s", e.From)
		}
		if _, ok := stationSet[e.To]; !ok {
			return fmt.Errorf("edge references unknown to station: %s", e.To)
		}
		if e.From == e.To {
			return fmt.Errorf("self-loop edge on station: %s", e.From)
		}
		if e.TravelMinutes <= 0 {
			return fmt.Errorf("edge %s -> %s has non-positive travel_minutes: %d", e.From, e.To, e.TravelMinutes)
		}
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}

	// Verify graph connectivity via BFS from first station.
	visited := make(map[string]bool, len(ds.Stations))
	queue := []string{ds.Stations[0].ID}
	visited[ds.Stations[0].ID] = true
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, nxt := range adj[curr] {
			if !visited[nxt] {
				visited[nxt] = true
				queue = append(queue, nxt)
			}
		}
	}
	if len(visited) != len(ds.Stations) {
		var missing []string
		for _, s := range ds.Stations {
			if !visited[s.ID] {
				missing = append(missing, s.ID)
			}
		}
		return fmt.Errorf("transit graph is disconnected; unreachable stations: %v", missing)
	}

	// Verify fare rules exist for all operators present in stations.json.
	opRules := map[string]int{}
	for _, f := range ds.FareRules {
		if !validOps[f.Operator] {
			return fmt.Errorf("fare rule has invalid operator: %s", f.Operator)
		}
		if f.Hops < 0 || f.FareTHB < 0 {
			return fmt.Errorf("fare rule has negative hops/fare: %+v", f)
		}
		opRules[f.Operator]++
	}
	usedOps := map[string]bool{}
	for _, s := range ds.Stations {
		usedOps[s.Operator] = true
	}
	for op := range usedOps {
		if opRules[op] == 0 {
			return fmt.Errorf("missing fare rules for operator: %s", op)
		}
	}

	if len(ds.Listings) == 0 {
		return fmt.Errorf("listings.json is empty")
	}
	validTypes := map[string]bool{"condo": true, "apartment": true}
	listingSet := make(map[string]bool, len(ds.Listings))
	for _, l := range ds.Listings {
		if l.ID == "" || l.TitleTH == "" || l.TitleEN == "" {
			return fmt.Errorf("listing missing required fields: %+v", l)
		}
		if listingSet[l.ID] {
			return fmt.Errorf("duplicate listing id: %s", l.ID)
		}
		if !validTypes[l.PropertyType] {
			return fmt.Errorf("listing %s has invalid property_type %q", l.ID, l.PropertyType)
		}
		if l.MonthlyRentTHB <= 0 || l.RoomSizeSqm <= 0 || l.Bedrooms < 0 {
			return fmt.Errorf("listing %s has invalid rent/size/bedrooms: %+v", l.ID, l)
		}
		if l.Lat < 13.45 || l.Lat > 14.12 || l.Lng < 100.25 || l.Lng > 100.98 {
			return fmt.Errorf("listing %s coordinates out of Greater Bangkok bounds: (%.4f, %.4f)", l.ID, l.Lat, l.Lng)
		}
		listingSet[l.ID] = true
	}

	for _, a := range ds.Access {
		if !listingSet[a.ListingID] {
			return fmt.Errorf("access row references unknown listing: %s", a.ListingID)
		}
		if _, ok := stationSet[a.StationID]; !ok {
			return fmt.Errorf("access row references unknown station: %s", a.StationID)
		}
		if a.WalkDistanceM < 0 || a.WalkMinutes < 0 {
			return fmt.Errorf("access row has negative distance/minutes: %+v", a)
		}
	}

	return nil
}

func upsertStations(ctx context.Context, tx pgx.Tx, stations []StationInput) error {
	const q = `
		INSERT INTO stations (id, name_th, name_en, line_code, operator, is_extension, location, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, ST_SetSRID(ST_MakePoint($7, $8), 4326)::geography, NOW())
		ON CONFLICT (id) DO UPDATE SET
			name_th      = EXCLUDED.name_th,
			name_en      = EXCLUDED.name_en,
			line_code    = EXCLUDED.line_code,
			operator     = EXCLUDED.operator,
			is_extension = EXCLUDED.is_extension,
			location     = EXCLUDED.location,
			updated_at   = NOW();
	`
	for _, s := range stations {
		if _, err := tx.Exec(ctx, q, s.ID, s.NameTH, s.NameEN, s.LineCode, s.Operator, s.IsExtension, s.Lng, s.Lat); err != nil {
			return fmt.Errorf("station %s: %w", s.ID, err)
		}
	}
	return nil
}

func upsertEdges(ctx context.Context, tx pgx.Tx, edges []EdgeInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM transit_edges`); err != nil {
		return fmt.Errorf("clear transit_edges: %w", err)
	}
	const q = `
		INSERT INTO transit_edges (from_station_id, to_station_id, travel_minutes, is_transfer)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (from_station_id, to_station_id) DO UPDATE SET
			travel_minutes = EXCLUDED.travel_minutes,
			is_transfer    = EXCLUDED.is_transfer;
	`
	for _, e := range edges {
		if _, err := tx.Exec(ctx, q, e.From, e.To, e.TravelMinutes, e.IsTransfer); err != nil {
			return fmt.Errorf("edge %s->%s: %w", e.From, e.To, err)
		}
		if _, err := tx.Exec(ctx, q, e.To, e.From, e.TravelMinutes, e.IsTransfer); err != nil {
			return fmt.Errorf("edge %s->%s: %w", e.To, e.From, err)
		}
	}
	return nil
}

func upsertFareRules(ctx context.Context, tx pgx.Tx, rules []FareRuleInput) error {
	const q = `
		INSERT INTO fare_rules (operator, hops, fare_thb, effective_date, source)
		VALUES ($1, $2, $3, $4::date, $5)
		ON CONFLICT (operator, hops, effective_date) DO UPDATE SET
			fare_thb = EXCLUDED.fare_thb,
			source   = EXCLUDED.source;
	`
	for _, r := range rules {
		if _, err := tx.Exec(ctx, q, r.Operator, r.Hops, r.FareTHB, r.EffectiveDate, r.Source); err != nil {
			return fmt.Errorf("fare rule %s/%d: %w", r.Operator, r.Hops, err)
		}
	}
	return nil
}

func upsertListings(ctx context.Context, tx pgx.Tx, listings []ListingInput) error {
	const q = `
		WITH pt AS (
			SELECT ST_SetSRID(ST_MakePoint($13::double precision, $14::double precision), 4326)::geography AS loc
		),
		nearest AS (
			SELECT s.id, ROUND(ST_Distance(pt.loc, s.location))::int AS walk_m
			FROM stations s, pt
			ORDER BY s.location <-> pt.loc
			LIMIT 1
		)
		INSERT INTO listings (
			id, osm_id, title_th, title_en, property_type, district,
			monthly_rent_thb, estimated_rent_min_thb, estimated_rent_max_thb, price_source, data_source,
			room_size_sqm, bedrooms,
			location, nearest_station_id, walk_to_station_m, updated_at
		)
		SELECT
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $15,
			pt.loc,
			COALESCE(NULLIF($16, ''), nearest.id),
			CASE WHEN $17 > 0 THEN $17 ELSE nearest.walk_m END,
			NOW()
		FROM pt, nearest
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
			room_size_sqm          = EXCLUDED.room_size_sqm,
			bedrooms               = EXCLUDED.bedrooms,
			location               = EXCLUDED.location,
			nearest_station_id     = EXCLUDED.nearest_station_id,
			walk_to_station_m      = EXCLUDED.walk_to_station_m,
			updated_at             = NOW();
	`
	ids := make([]string, 0, len(listings))
	for _, l := range listings {
		ids = append(ids, l.ID)
		src := l.Source
		if src == "" {
			src = "osm"
		}
		estMin := l.EstimatedRentMinTHB
		estMax := l.EstimatedRentMaxTHB
		if estMin <= 0 {
			estMin = l.MonthlyRentTHB
		}
		if estMax <= 0 {
			estMax = l.MonthlyRentTHB
		}
		if _, err := tx.Exec(ctx, q,
			l.ID, l.OSMID, l.TitleTH, l.TitleEN, l.PropertyType, l.District,
			l.MonthlyRentTHB, estMin, estMax, l.PriceSource, src,
			l.RoomSizeSqm, l.Lng, l.Lat, l.Bedrooms, l.NearestStationID, l.WalkToStationM,
		); err != nil {
			return fmt.Errorf("listing %s: %w", l.ID, err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM listings WHERE NOT (id = ANY($1))`, ids); err != nil {
		return fmt.Errorf("prune stale listings: %w", err)
	}
	return nil
}

func upsertListingStationAccess(ctx context.Context, tx pgx.Tx, rows []AccessInput) error {
	if len(rows) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM listing_station_access`); err != nil {
		return fmt.Errorf("clear listing_station_access: %w", err)
	}
	const q = `
		INSERT INTO listing_station_access (listing_id, station_id, walk_distance_m, walk_minutes, straight_line_m, distance_source, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (listing_id, station_id) DO UPDATE SET
			walk_distance_m = EXCLUDED.walk_distance_m,
			walk_minutes    = EXCLUDED.walk_minutes,
			straight_line_m = EXCLUDED.straight_line_m,
			distance_source = EXCLUDED.distance_source,
			updated_at      = NOW();
	`
	for _, r := range rows {
		src := r.DistanceSource
		if src == "" {
			src = "osrm_foot"
		}
		if _, err := tx.Exec(ctx, q, r.ListingID, r.StationID, r.WalkDistanceM, r.WalkMinutes, r.StraightLineM, src); err != nil {
			return fmt.Errorf("access %s->%s: %w", r.ListingID, r.StationID, err)
		}
	}
	return nil
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func writeSeedSQL(path string, ds *Dataset) error {
	var b strings.Builder
	b.WriteString("BEGIN;\n\n")

	b.WriteString("INSERT INTO public.stations (id, name_th, name_en, line_code, operator, is_extension, location, updated_at) VALUES\n")
	for i, s := range ds.Stations {
		sep := ","
		if i == len(ds.Stations)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "  (%s, %s, %s, %s, %s, %t, ST_SetSRID(ST_MakePoint(%.6f, %.6f), 4326)::geography, NOW())%s\n",
			sqlQuote(s.ID), sqlQuote(s.NameTH), sqlQuote(s.NameEN), sqlQuote(s.LineCode), sqlQuote(s.Operator), s.IsExtension, s.Lng, s.Lat, sep,
		)
	}
	b.WriteString("ON CONFLICT (id) DO UPDATE SET name_th=EXCLUDED.name_th, name_en=EXCLUDED.name_en, line_code=EXCLUDED.line_code, operator=EXCLUDED.operator, is_extension=EXCLUDED.is_extension, location=EXCLUDED.location, updated_at=NOW();\n\n")

	b.WriteString("INSERT INTO public.transit_edges (from_station_id, to_station_id, travel_minutes, is_transfer) VALUES\n")
	for i, e := range ds.Edges {
		sep := ","
		if i == len(ds.Edges)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "  (%s, %s, %d, %t), (%s, %s, %d, %t)%s\n",
			sqlQuote(e.From), sqlQuote(e.To), e.TravelMinutes, e.IsTransfer,
			sqlQuote(e.To), sqlQuote(e.From), e.TravelMinutes, e.IsTransfer, sep,
		)
	}
	b.WriteString("ON CONFLICT (from_station_id, to_station_id) DO UPDATE SET travel_minutes=EXCLUDED.travel_minutes, is_transfer=EXCLUDED.is_transfer;\n\n")

	b.WriteString("INSERT INTO public.fare_rules (operator, hops, fare_thb, effective_date, source) VALUES\n")
	for i, f := range ds.FareRules {
		sep := ","
		if i == len(ds.FareRules)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "  (%s, %d, %d, %s::date, %s)%s\n",
			sqlQuote(f.Operator), f.Hops, f.FareTHB, sqlQuote(f.EffectiveDate), sqlQuote(f.Source), sep,
		)
	}
	b.WriteString("ON CONFLICT (operator, hops, effective_date) DO UPDATE SET fare_thb=EXCLUDED.fare_thb, source=EXCLUDED.source;\n\n")

	b.WriteString("INSERT INTO public.listings (id, osm_id, title_th, title_en, property_type, district, monthly_rent_thb, estimated_rent_min_thb, estimated_rent_max_thb, price_source, data_source, room_size_sqm, bedrooms, location, nearest_station_id, walk_to_station_m, updated_at) VALUES\n")
	for i, l := range ds.Listings {
		sep := ","
		if i == len(ds.Listings)-1 {
			sep = ""
		}
		src := l.Source
		if src == "" {
			src = "osm"
		}
		fmt.Fprintf(&b, "  (%s, %s, %s, %s, %s, %s, %d, %d, %d, %s, %s, %.1f, %d, ST_SetSRID(ST_MakePoint(%.6f, %.6f), 4326)::geography, %s, %d, NOW())%s\n",
			sqlQuote(l.ID), sqlQuote(l.OSMID), sqlQuote(l.TitleTH), sqlQuote(l.TitleEN), sqlQuote(l.PropertyType), sqlQuote(l.District),
			l.MonthlyRentTHB, l.EstimatedRentMinTHB, l.EstimatedRentMaxTHB, sqlQuote(l.PriceSource), sqlQuote(src),
			l.RoomSizeSqm, l.Bedrooms, l.Lng, l.Lat, sqlQuote(l.NearestStationID), l.WalkToStationM, sep,
		)
	}
	b.WriteString("ON CONFLICT (id) DO UPDATE SET osm_id=EXCLUDED.osm_id, title_th=EXCLUDED.title_th, title_en=EXCLUDED.title_en, property_type=EXCLUDED.property_type, district=EXCLUDED.district, monthly_rent_thb=EXCLUDED.monthly_rent_thb, estimated_rent_min_thb=EXCLUDED.estimated_rent_min_thb, estimated_rent_max_thb=EXCLUDED.estimated_rent_max_thb, price_source=EXCLUDED.price_source, data_source=EXCLUDED.data_source, room_size_sqm=EXCLUDED.room_size_sqm, bedrooms=EXCLUDED.bedrooms, location=EXCLUDED.location, nearest_station_id=EXCLUDED.nearest_station_id, walk_to_station_m=EXCLUDED.walk_to_station_m, updated_at=NOW();\n\n")

	if len(ds.Access) > 0 {
		b.WriteString("INSERT INTO public.listing_station_access (listing_id, station_id, walk_distance_m, walk_minutes, straight_line_m, distance_source, updated_at) VALUES\n")
		for i, a := range ds.Access {
			sep := ","
			if i == len(ds.Access)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "  (%s, %s, %d, %d, %d, %s, NOW())%s\n",
				sqlQuote(a.ListingID), sqlQuote(a.StationID), a.WalkDistanceM, a.WalkMinutes, a.StraightLineM, sqlQuote(a.DistanceSource), sep,
			)
		}
		b.WriteString("ON CONFLICT (listing_id, station_id) DO UPDATE SET walk_distance_m=EXCLUDED.walk_distance_m, walk_minutes=EXCLUDED.walk_minutes, straight_line_m=EXCLUDED.straight_line_m, distance_source=EXCLUDED.distance_source, updated_at=NOW();\n\n")
	}

	b.WriteString("COMMIT;\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
