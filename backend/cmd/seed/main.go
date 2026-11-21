package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
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
}

type GeneratedListing struct {
	ID             string
	TitleTH        string
	TitleEN        string
	PropertyType   string
	District       string
	MonthlyRentTHB int
	RoomSizeSqm    float64
	Bedrooms       int
	Lat            float64
	Lng            float64
}

type Dataset struct {
	Stations  []StationInput
	Edges     []EdgeInput
	FareRules []FareRuleInput
}

func main() {
	dataDir := flag.String("data-dir", "./data", "directory containing stations.json, edges.json, fare_rules.json")
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

	listings := generateListings(ds.Stations)
	log.Printf("dataset valid: %d stations, %d undirected edges (%d directed), %d fare rules, %d generated listings",
		len(ds.Stations), len(ds.Edges), len(ds.Edges)*2, len(ds.FareRules), len(listings))

	if *dumpSQL != "" {
		if err := writeSeedSQL(*dumpSQL, ds, listings); err != nil {
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
	if err := upsertListings(ctx, tx, listings); err != nil {
		log.Fatalf("upsert listings: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit tx: %v", err)
	}

	log.Printf("seeded database successfully: %d stations, %d directed edges, %d fare rules, %d listings",
		len(ds.Stations), len(ds.Edges)*2, len(ds.FareRules), len(listings))

	cacheClient := cache.NewFromURL(cfg.RedisURL, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
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

	validOps := map[string]bool{"BTS": true, "MRT": true, "ARL": true}
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
		if s.Lat < 13.50 || s.Lat > 14.05 || s.Lng < 100.30 || s.Lng > 100.90 {
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

	// Verify fare rules exist for all operators.
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
	for op := range validOps {
		if opRules[op] == 0 {
			return fmt.Errorf("missing fare rules for operator: %s", op)
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
		INSERT INTO fare_rules (operator, hops, fare_thb, effective_date)
		VALUES ($1, $2, $3, $4::date)
		ON CONFLICT (operator, hops, effective_date) DO UPDATE SET
			fare_thb = EXCLUDED.fare_thb;
	`
	for _, r := range rules {
		if _, err := tx.Exec(ctx, q, r.Operator, r.Hops, r.FareTHB, r.EffectiveDate); err != nil {
			return fmt.Errorf("fare rule %s/%d: %w", r.Operator, r.Hops, err)
		}
	}
	return nil
}

func upsertListings(ctx context.Context, tx pgx.Tx, listings []GeneratedListing) error {
	const q = `
		WITH pt AS (
			SELECT ST_SetSRID(ST_MakePoint($9::double precision, $10::double precision), 4326)::geography AS loc
		),
		nearest AS (
			SELECT s.id, ROUND(ST_Distance(pt.loc, s.location))::int AS walk_m
			FROM stations s, pt
			ORDER BY s.location <-> pt.loc
			LIMIT 1
		)
		INSERT INTO listings (
			id, title_th, title_en, property_type, district,
			monthly_rent_thb, room_size_sqm, bedrooms,
			location, nearest_station_id, walk_to_station_m, updated_at
		)
		SELECT
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			pt.loc, nearest.id, nearest.walk_m, NOW()
		FROM pt, nearest
		ON CONFLICT (id) DO UPDATE SET
			title_th           = EXCLUDED.title_th,
			title_en           = EXCLUDED.title_en,
			property_type      = EXCLUDED.property_type,
			district           = EXCLUDED.district,
			monthly_rent_thb   = EXCLUDED.monthly_rent_thb,
			room_size_sqm      = EXCLUDED.room_size_sqm,
			bedrooms           = EXCLUDED.bedrooms,
			location           = EXCLUDED.location,
			nearest_station_id = EXCLUDED.nearest_station_id,
			walk_to_station_m  = EXCLUDED.walk_to_station_m,
			updated_at         = NOW();
	`
	for _, l := range listings {
		if _, err := tx.Exec(ctx, q,
			l.ID, l.TitleTH, l.TitleEN, l.PropertyType, l.District,
			l.MonthlyRentTHB, l.RoomSizeSqm, l.Bedrooms,
			l.Lng, l.Lat,
		); err != nil {
			return fmt.Errorf("listing %s: %w", l.ID, err)
		}
	}
	return nil
}

type rentTier struct {
	condoMin int
	condoMax int
	aptMin   int
	aptMax   int
	district string
}

func stationMarketTier(s StationInput) rentTier {
	switch s.ID {
	case "BTS_CEN", "BTS_E1", "BTS_E2", "BTS_E3", "BTS_E4", "BTS_E5", "BTS_E6", "BTS_S1", "BTS_S2", "BTS_S3", "MRT_BL22", "MRT_BL25", "MRT_BL26":
		return rentTier{condoMin: 18000, condoMax: 34000, aptMin: 11000, aptMax: 16500, district: "CBD / Sukhumvit-Silom"}
	case "BTS_N5", "BTS_N4", "BTS_N3", "BTS_N2", "BTS_N1", "BTS_E7", "BTS_S4", "BTS_S5", "MRT_BL20", "MRT_BL21", "MRT_BL27", "ARL_A8", "ARL_A7", "ARL_A6":
		return rentTier{condoMin: 13000, condoMax: 23000, aptMin: 8000, aptMax: 12500, district: "Inner City"}
	case "BTS_N8", "BTS_N7", "BTS_N9", "BTS_N10", "BTS_E8", "BTS_E9", "BTS_S6", "BTS_S7", "BTS_S8", "MRT_BL13", "MRT_BL14", "MRT_BL15", "MRT_BL16", "MRT_BL17", "MRT_BL18", "MRT_BL19", "MRT_BL23", "MRT_BL24", "MRT_BL28":
		return rentTier{condoMin: 9500, condoMax: 16500, aptMin: 6000, aptMax: 9500, district: "Mid-Town Transit"}
	default:
		return rentTier{condoMin: 6500, condoMax: 12000, aptMin: 4500, aptMax: 7500, district: "Outer Transit"}
	}
}

var condoPrefixesTH = []string{"เดอะ ไลน์", "ไอดีโอ", "ไลฟ์", "แอสปาย", "โนเบิล", "ศุภาลัย ไลท์", "ริธึ่ม", "เดอะ เบส"}
var condoPrefixesEN = []string{"The Line", "Ideo", "Life", "Aspire", "Noble", "Supalai Lite", "Rhythm", "The Base"}
var aptPrefixesTH = []string{"บ้านสบาย เรสซิเดนซ์", "เดอะ ลิฟวิ่ง เพลส", "ซิตี้ ลอฟท์ อพาร์ตเมนต์", "กรีน พาร์ค แมนชั่น", "เออร์เบิน ลิฟวิ่ง"}
var aptPrefixesEN = []string{"Baan Sabai Residence", "The Living Place", "City Loft Apartment", "Green Park Mansion", "Urban Living"}

// generateListings creates ~160-180 deterministic Bangkok condo & apartment listings
// distributed across all stations with realistic market rent and walk distances.
func generateListings(stations []StationInput) []GeneratedListing {
	rng := rand.New(rand.NewSource(42))
	var out []GeneratedListing
	seq := 1

	for _, s := range stations {
		// Skip Suvarnabhumi Airport terminal for residential listings.
		if s.ID == "ARL_A1" {
			continue
		}

		tier := stationMarketTier(s)

		// Generate 2 listings per station (1 condo + 1 apartment), plus a 3rd near major hubs.
		count := 2
		if tier.condoMin >= 13000 {
			count = 3
		}

		for i := 0; i < count; i++ {
			isCondo := i != 1
			var (
				propType string
				titleTH  string
				titleEN  string
				rent     int
				sqm      float64
				beds     int
			)

			if isCondo {
				propType = "condo"
				idx := rng.Intn(len(condoPrefixesEN))
				titleTH = fmt.Sprintf("%s %s", condoPrefixesTH[idx], s.NameTH)
				titleEN = fmt.Sprintf("%s %s", condoPrefixesEN[idx], s.NameEN)
				rent = roundTo500(tier.condoMin + rng.Intn(tier.condoMax-tier.condoMin+1))
				if rent >= 24000 {
					beds = 1 + rng.Intn(2)
					sqm = float64(34 + rng.Intn(22))
				} else {
					beds = 1
					sqm = float64(26 + rng.Intn(12))
				}
			} else {
				propType = "apartment"
				idx := rng.Intn(len(aptPrefixesEN))
				titleTH = fmt.Sprintf("%s %s", aptPrefixesTH[idx], s.NameTH)
				titleEN = fmt.Sprintf("%s %s", aptPrefixesEN[idx], s.NameEN)
				rent = roundTo500(tier.aptMin + rng.Intn(tier.aptMax-tier.aptMin+1))
				beds = 0
				sqm = float64(21 + rng.Intn(9))
			}

			// Place listings 120m to 950m from the station at random bearings.
			distMeters := 120.0 + rng.Float64()*830.0
			bearing := rng.Float64() * 2 * math.Pi
			lat, lng := offsetLatLng(s.Lat, s.Lng, distMeters, bearing)

			out = append(out, GeneratedListing{
				ID:             fmt.Sprintf("LST_%03d", seq),
				TitleTH:        titleTH,
				TitleEN:        titleEN,
				PropertyType:   propType,
				District:       tier.district,
				MonthlyRentTHB: rent,
				RoomSizeSqm:    sqm,
				Bedrooms:       beds,
				Lat:            lat,
				Lng:            lng,
			})
			seq++
		}
	}

	return out
}

func roundTo500(v int) int {
	return ((v + 250) / 500) * 500
}

func offsetLatLng(lat, lng, distMeters, bearingRad float64) (float64, float64) {
	const metersPerDegLat = 111320.0
	dLat := (distMeters * math.Cos(bearingRad)) / metersPerDegLat
	dLng := (distMeters * math.Sin(bearingRad)) / (metersPerDegLat * math.Cos(lat*math.Pi/180.0))
	return math.Round((lat+dLat)*1e6) / 1e6, math.Round((lng+dLng)*1e6) / 1e6
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func writeSeedSQL(path string, ds *Dataset, listings []GeneratedListing) error {
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

	b.WriteString("INSERT INTO public.fare_rules (operator, hops, fare_thb, effective_date) VALUES\n")
	for i, f := range ds.FareRules {
		sep := ","
		if i == len(ds.FareRules)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "  (%s, %d, %d, %s::date)%s\n",
			sqlQuote(f.Operator), f.Hops, f.FareTHB, sqlQuote(f.EffectiveDate), sep,
		)
	}
	b.WriteString("ON CONFLICT (operator, hops, effective_date) DO UPDATE SET fare_thb=EXCLUDED.fare_thb;\n\n")

	b.WriteString("WITH raw_listings(id, title_th, title_en, property_type, district, monthly_rent_thb, room_size_sqm, bedrooms, lng, lat) AS (\n  VALUES\n")
	for i, l := range listings {
		sep := ","
		if i == len(listings)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "    (%s, %s, %s, %s, %s, %d, %.1f::numeric, %d, %.6f::double precision, %.6f::double precision)%s\n",
			sqlQuote(l.ID), sqlQuote(l.TitleTH), sqlQuote(l.TitleEN), sqlQuote(l.PropertyType), sqlQuote(l.District),
			l.MonthlyRentTHB, l.RoomSizeSqm, l.Bedrooms, l.Lng, l.Lat, sep,
		)
	}
	b.WriteString(`),
with_geo AS (
  SELECT *, ST_SetSRID(ST_MakePoint(lng, lat), 4326)::geography AS loc
  FROM raw_listings
)
INSERT INTO public.listings (id, title_th, title_en, property_type, district, monthly_rent_thb, room_size_sqm, bedrooms, location, nearest_station_id, walk_to_station_m, updated_at)
SELECT g.id, g.title_th, g.title_en, g.property_type, g.district, g.monthly_rent_thb, g.room_size_sqm, g.bedrooms, g.loc, n.id, n.walk_m, NOW()
FROM with_geo g
CROSS JOIN LATERAL (
  SELECT s.id, ROUND(ST_Distance(g.loc, s.location))::int AS walk_m
  FROM public.stations s
  ORDER BY s.location <-> g.loc
  LIMIT 1
) n
ON CONFLICT (id) DO UPDATE SET
  title_th=EXCLUDED.title_th, title_en=EXCLUDED.title_en, property_type=EXCLUDED.property_type,
  district=EXCLUDED.district, monthly_rent_thb=EXCLUDED.monthly_rent_thb, room_size_sqm=EXCLUDED.room_size_sqm,
  bedrooms=EXCLUDED.bedrooms, location=EXCLUDED.location, nearest_station_id=EXCLUDED.nearest_station_id,
  walk_to_station_m=EXCLUDED.walk_to_station_m, updated_at=NOW();

COMMIT;
`)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
