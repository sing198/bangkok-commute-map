package commute

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/tk/bangkok-commute-map/backend/internal/cache"
	"github.com/tk/bangkok-commute-map/backend/internal/transit"
)

const (
	searchCacheTTL      = 15 * time.Minute
	defaultSearchLimit  = 450
	maxSearchLimit      = 1000
	maxAccessQueryDistM = 2500
)

// SearchParams defines query filters for /api/commute/search.
type SearchParams struct {
	Lat              float64
	Lng              float64
	MaxMinutes       int
	MaxBudget        int    // Optional cap on verified monthly rent
	MaxCommuteBudget int    // Optional cap on real monthly commute cost (rail + feeder)
	VerifiedOnly     bool   // If true, return only listings with tenant-verified real rent
	Workdays         int
	WalkMaxM         int
	Sort             string // "time" (default), "commute_cost", or "true_cost"
	Limit            int
}

// ReachableStation is a station reachable within max_minutes with its zone color tier.
type ReachableStation struct {
	Station       transit.Station     `json:"station"`
	ZoneColor     string              `json:"zone_color"` // "green" (<=20), "yellow" (21-35), "orange" (36-45), "red" (>45)
	CommuteMins   int                 `json:"commute_minutes"`
	OneWayFareTHB int                 `json:"one_way_fare_thb"`
	Transfers     int                 `json:"transfers"`
	Route         transit.RouteResult `json:"route"`
	OfficeAccess  transit.AccessLeg   `json:"office_access"`
}

// ListingResult is a real Bangkok building enriched with factual OSRM street walking distance,
// real BTS/MRT/ARL/SRT rail time & fare, and verified user-reported rent (never guessed).
type ListingResult struct {
	ID                    string                     `json:"id"`
	OSMID                 string                     `json:"osm_id,omitempty"`
	TitleTH               string                     `json:"title_th"`
	TitleEN               string                     `json:"title_en"`
	PropertyType          string                     `json:"property_type"`
	District              string                     `json:"district"`
	MonthlyRentTHB        int                        `json:"monthly_rent_thb"` // >0 ONLY when price_status == "verified"
	PriceStatus           string                     `json:"price_status"`     // "verified" or "unverified"
	DataSource            string                     `json:"data_source,omitempty"`
	ListingURL            string                     `json:"listing_url,omitempty"`
	ListingURLLabel       string                     `json:"listing_url_label,omitempty"`
	VerifiedRentMedianTHB int                        `json:"verified_rent_median_thb,omitempty"`
	VerifiedReportCount   int                        `json:"verified_report_count"`
	LatestReportMonth     string                     `json:"latest_report_month,omitempty"`
	LatestSourceURL       string                     `json:"latest_source_url,omitempty"`
	RoomSizeSqm           float64                    `json:"room_size_sqm"`
	Bedrooms              int                        `json:"bedrooms"`
	Lat                   float64                    `json:"lat"`
	Lng                   float64                    `json:"lng"`
	OriginStation         transit.Station            `json:"origin_station"`
	DestStation           transit.Station            `json:"dest_station"`
	HomeAccess            transit.AccessLeg          `json:"home_access"`
	OfficeAccess          transit.AccessLeg          `json:"office_access"`
	Route                 transit.RouteResult        `json:"route"`
	Cost                  transit.CommuteCostSummary `json:"cost"`
	ZoneColor             string                     `json:"zone_color"`
}

// SearchResponse is returned by GET /api/commute/search.
type SearchResponse struct {
	Office                OfficePoint        `json:"office"`
	Params                SearchMeta         `json:"params"`
	DetourFactor          float64            `json:"detour_factor"`
	TotalListingsCount    int                `json:"total_listings_count"`
	ReturnedListingsCount int                `json:"returned_listings_count"`
	NearestStations       []NearestStation   `json:"nearest_office_stations"`
	ReachableStations     []ReachableStation `json:"reachable_stations"`
	Listings              []ListingResult    `json:"listings"`
}

type OfficePoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type SearchMeta struct {
	MaxMinutes       int    `json:"max_minutes"`
	MaxBudget        int    `json:"max_budget"`
	MaxCommuteBudget int    `json:"max_commute_budget"`
	VerifiedOnly     bool   `json:"verified_only"`
	Workdays         int    `json:"workdays"`
	WalkMaxM         int    `json:"walk_max_m"`
	Sort             string `json:"sort"`
	Limit            int    `json:"limit"`
}

// CompareRequest is the JSON payload for POST /api/commute/compare.
type CompareRequest struct {
	Office     OfficePoint `json:"office" binding:"required"`
	Workdays   int         `json:"workdays"`
	WalkMaxM   int         `json:"walk_max_m"`
	ListingIDs []string    `json:"listing_ids" binding:"required,min=2,max=3"`
}

// CompareSummary highlights the best commute cost and fastest commute trade-offs.
type CompareSummary struct {
	LowestTrueCostID            string  `json:"lowest_true_cost_id"`
	LowestCommuteCostID         string  `json:"lowest_commute_cost_id"`
	FastestCommuteID            string  `json:"fastest_commute_id"`
	MaxMonthlySavingsTHB        int     `json:"max_monthly_savings_thb"`
	MaxMonthlyCommuteSavingsTHB int     `json:"max_monthly_commute_savings_thb"`
	MaxMonthlyHoursSaved        float64 `json:"max_monthly_hours_saved"`
}

// CompareResponse is returned by POST /api/commute/compare.
type CompareResponse struct {
	Office          OfficePoint      `json:"office"`
	Workdays        int              `json:"workdays"`
	WalkMaxM        int              `json:"walk_max_m"`
	NearestStations []NearestStation `json:"nearest_office_stations"`
	Items           []ListingResult  `json:"items"`
	Summary         CompareSummary   `json:"summary"`
}

// Service orchestrates PostGIS spatial queries, OSRM precomputed walking access,
// in-memory Dijkstra routing, crowdsourced price reports, and Redis caching.
type Service struct {
	repo  *Repository
	graph *transit.Graph
	cache *cache.Client
}

// NewService creates a Commute Service.
func NewService(repo *Repository, graph *transit.Graph, cacheClient *cache.Client) *Service {
	return &Service{
		repo:  repo,
		graph: graph,
		cache: cacheClient,
	}
}

// NormalizeParams applies defaults and bounds to search parameters.
func NormalizeParams(p SearchParams) SearchParams {
	if p.MaxMinutes <= 0 {
		p.MaxMinutes = 45
	}
	if p.MaxMinutes > 120 {
		p.MaxMinutes = 120
	}
	if p.Workdays <= 0 {
		p.Workdays = 22
	}
	if p.Workdays > 31 {
		p.Workdays = 31
	}
	if p.WalkMaxM <= 0 {
		p.WalkMaxM = 800
	}
	if p.WalkMaxM > 2500 {
		p.WalkMaxM = 2500
	}
	switch p.Sort {
	case "time", "commute_cost", "true_cost":
	default:
		p.Sort = "time"
	}
	if p.Limit <= 0 {
		p.Limit = defaultSearchLimit
	}
	if p.Limit > maxSearchLimit {
		p.Limit = maxSearchLimit
	}
	return p
}

// QuantizedCacheKey rounds coordinates to ~100m precision (3 decimal places)
// so nearby office clicks share cache entries and maximize Redis hit rate.
func QuantizedCacheKey(p SearchParams) string {
	qLat := math.Round(p.Lat*1000) / 1000
	qLng := math.Round(p.Lng*1000) / 1000
	return fmt.Sprintf("commute:search:v5:%.3f:%.3f:%d:%d:%d:%t:%d:%d:%s:%d",
		qLat, qLng, p.MaxMinutes, p.MaxBudget, p.MaxCommuteBudget, p.VerifiedOnly, p.Workdays, p.WalkMaxM, p.Sort, p.Limit)
}

func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusM = 6371000.0
	p1 := lat1 * math.Pi / 180.0
	p2 := lat2 * math.Pi / 180.0
	dp := (lat2 - lat1) * math.Pi / 180.0
	dl := (lng2 - lng1) * math.Pi / 180.0
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(a))
}

func buildListingResult(
	rec ListingRecord,
	stOpt ReachableStation,
	destSt transit.Station,
	office OfficePoint,
	workdays, walkMaxM int,
) ListingResult {
	distSrc := rec.DistanceSource
	if distSrc == "" {
		distSrc = "osrm_foot"
	}

	homeAccess := transit.CalculateAccessWithSource(rec.WalkToStationM, walkMaxM, distSrc)
	officeAccess := stOpt.OfficeAccess
	route := stOpt.Route

	// Fix same-station zero-rail V-detour: when home and office share the exact same station
	// (no rail segment boarded), calculate a single direct door-to-door trip instead of two
	// separate feeder legs to/from an unused station.
	if len(route.StationIDs) <= 1 {
		directStreetM := haversineMeters(rec.Lat, rec.Lng, office.Lat, office.Lng) * transit.BangkokDetourFactor
		homeAccess = transit.CalculateAccessWithSource(directStreetM, walkMaxM, "estimated_detour")
		officeAccess = transit.AccessLeg{
			DistanceM:      0,
			Mode:           "walk",
			Minutes:        0,
			OneWayCostTHB:  0,
			DistanceSource: "estimated_detour",
		}
	}

	// Never guess room rent: only populate MonthlyRentTHB when tenant-verified reports exist.
	verifiedRent := 0
	priceStatus := "unverified"
	if rec.VerifiedReportCount > 0 && rec.VerifiedRentMedianTHB > 0 {
		verifiedRent = rec.VerifiedRentMedianTHB
		priceStatus = "verified"
	}

	cost := transit.CalculateMonthlyCost(verifiedRent, homeAccess, officeAccess, route, workdays)

	return ListingResult{
		ID:                    rec.ID,
		OSMID:                 rec.OSMID,
		TitleTH:               rec.TitleTH,
		TitleEN:               rec.TitleEN,
		PropertyType:          rec.PropertyType,
		District:              rec.District,
		MonthlyRentTHB:        verifiedRent,
		PriceStatus:           priceStatus,
		DataSource:            rec.DataSource,
		ListingURL:            rec.ListingURL,
		ListingURLLabel:       rec.ListingURLLabel,
		VerifiedRentMedianTHB: rec.VerifiedRentMedianTHB,
		VerifiedReportCount:   rec.VerifiedReportCount,
		LatestReportMonth:     rec.LatestReportMonth,
		LatestSourceURL:       rec.LatestSourceURL,
		RoomSizeSqm:           rec.RoomSizeSqm,
		Bedrooms:              rec.Bedrooms,
		Lat:                   rec.Lat,
		Lng:                   rec.Lng,
		OriginStation:         stOpt.Station,
		DestStation:           destSt,
		HomeAccess:            homeAccess,
		OfficeAccess:          officeAccess,
		Route:                 route,
		Cost:                  cost,
		ZoneColor:             zoneColorForMinutes(cost.OneWayMinutes),
	}
}

// Search finds reachable stations and matching real Bangkok buildings ranked by factual commute metrics.
func (s *Service) Search(ctx context.Context, raw SearchParams) (SearchResponse, bool, error) {
	p := NormalizeParams(raw)
	cacheKey := QuantizedCacheKey(p)

	if cached, ok := s.cache.Get(ctx, cacheKey); ok {
		var resp SearchResponse
		if err := json.Unmarshal([]byte(cached), &resp); err == nil {
			return resp, true, nil
		}
	}

	nearest, err := s.repo.FindNearestStations(ctx, p.Lat, p.Lng, 4)
	if err != nil {
		return SearchResponse{}, false, err
	}
	if len(nearest) == 0 {
		return SearchResponse{}, false, fmt.Errorf("no transit stations found in database")
	}

	bestStationOption := s.computeBestStationOptions(nearest, p.MaxMinutes, p.WalkMaxM)

	reachableStations := make([]ReachableStation, 0, len(bestStationOption))
	reachableIDs := make([]string, 0, len(bestStationOption))
	for id, opt := range bestStationOption {
		reachableStations = append(reachableStations, opt)
		reachableIDs = append(reachableIDs, id)
	}
	sort.Slice(reachableStations, func(i, j int) bool {
		if reachableStations[i].CommuteMins != reachableStations[j].CommuteMins {
			return reachableStations[i].CommuteMins < reachableStations[j].CommuteMins
		}
		return reachableStations[i].Station.ID < reachableStations[j].Station.ID
	})

	searchWalkM := p.WalkMaxM + 900
	if searchWalkM < 1800 {
		searchWalkM = 1800
	}
	if searchWalkM > maxAccessQueryDistM {
		searchWalkM = maxAccessQueryDistM
	}
	rawListings, err := s.repo.FindListingsNearStations(ctx, reachableIDs, searchWalkM, p.MaxBudget)
	if err != nil {
		return SearchResponse{}, false, err
	}

	officePt := OfficePoint{Lat: p.Lat, Lng: p.Lng}
	bestByListing := make(map[string]ListingResult, len(rawListings))
	for _, rec := range rawListings {
		stOpt, ok := bestStationOption[rec.StationID]
		if !ok {
			continue
		}
		destSt, _ := s.graph.Station(stOpt.Route.DestID)
		candidate := buildListingResult(rec, stOpt, destSt, officePt, p.Workdays, p.WalkMaxM)
		if candidate.Cost.OneWayMinutes > p.MaxMinutes {
			continue
		}
		if p.MaxCommuteBudget > 0 && candidate.Cost.MonthlyCommuteTHB > p.MaxCommuteBudget {
			continue
		}
		if p.VerifiedOnly && candidate.PriceStatus != "verified" {
			continue
		}
		if existing, exists := bestByListing[rec.ID]; !exists || isBetterCommuteOption(candidate, existing) {
			bestByListing[rec.ID] = candidate
		}
	}

	listings := make([]ListingResult, 0, len(bestByListing))
	for _, item := range bestByListing {
		listings = append(listings, item)
	}
	sortListings(listings, p.Sort)

	totalCount := len(listings)
	if len(listings) > p.Limit {
		listings = listings[:p.Limit]
	}

	resp := SearchResponse{
		Office: officePt,
		Params: SearchMeta{
			MaxMinutes:       p.MaxMinutes,
			MaxBudget:        p.MaxBudget,
			MaxCommuteBudget: p.MaxCommuteBudget,
			VerifiedOnly:     p.VerifiedOnly,
			Workdays:         p.Workdays,
			WalkMaxM:         p.WalkMaxM,
			Sort:             p.Sort,
			Limit:            p.Limit,
		},
		DetourFactor:          transit.BangkokDetourFactor,
		TotalListingsCount:    totalCount,
		ReturnedListingsCount: len(listings),
		NearestStations:       nearest,
		ReachableStations:     reachableStations,
		Listings:              listings,
	}

	if encoded, err := json.Marshal(resp); err == nil {
		s.cache.Set(ctx, cacheKey, string(encoded), searchCacheTTL)
	}

	return resp, false, nil
}

// Compare evaluates 2-3 specific listings side-by-side against a target office location.
func (s *Service) Compare(ctx context.Context, req CompareRequest) (CompareResponse, error) {
	workdays := req.Workdays
	if workdays <= 0 {
		workdays = 22
	}
	walkMaxM := req.WalkMaxM
	if walkMaxM <= 0 {
		walkMaxM = 800
	}

	nearest, err := s.repo.FindNearestStations(ctx, req.Office.Lat, req.Office.Lng, 4)
	if err != nil {
		return CompareResponse{}, err
	}
	if len(nearest) == 0 {
		return CompareResponse{}, fmt.Errorf("no transit stations found in database")
	}

	searchWalkM := walkMaxM + 900
	if searchWalkM < 1800 {
		searchWalkM = 1800
	}
	if searchWalkM > maxAccessQueryDistM {
		searchWalkM = maxAccessQueryDistM
	}
	records, err := s.repo.FindListingsByIDsNearStations(ctx, req.ListingIDs, searchWalkM)
	if err != nil {
		return CompareResponse{}, err
	}
	if len(records) == 0 {
		return CompareResponse{}, fmt.Errorf("no matching listings found")
	}

	bestStationOption := s.computeBestStationOptions(nearest, -1, walkMaxM)

	bestByListing := make(map[string]ListingResult, len(req.ListingIDs))
	for _, rec := range records {
		stOpt, ok := bestStationOption[rec.StationID]
		if !ok {
			continue
		}
		destSt, _ := s.graph.Station(stOpt.Route.DestID)
		candidate := buildListingResult(rec, stOpt, destSt, req.Office, workdays, walkMaxM)
		if existing, exists := bestByListing[rec.ID]; !exists || isBetterCommuteOption(candidate, existing) {
			bestByListing[rec.ID] = candidate
		}
	}

	items := make([]ListingResult, 0, len(req.ListingIDs))
	for _, id := range req.ListingIDs {
		if item, ok := bestByListing[id]; ok {
			items = append(items, item)
		}
	}

	summary := buildCompareSummary(items)

	return CompareResponse{
		Office:          req.Office,
		Workdays:        workdays,
		WalkMaxM:        walkMaxM,
		NearestStations: nearest,
		Items:           items,
		Summary:         summary,
	}, nil
}

func (s *Service) computeBestStationOptions(nearest []NearestStation, maxMinutes int, walkMaxM int) map[string]ReachableStation {
	best := make(map[string]ReachableStation)

	for _, ns := range nearest {
		officeAccess := transit.CalculateOfficeAccess(ns.DistanceM, walkMaxM)
		maxRail := -1
		if maxMinutes >= 0 {
			maxRail = maxMinutes - officeAccess.Minutes
			if maxRail < 0 {
				continue
			}
		}

		routes, err := s.graph.ReachableToDest(ns.Station.ID, maxRail)
		if err != nil {
			continue
		}

		for stationID, route := range routes {
			waitMins := 0
			if len(route.StationIDs) > 1 {
				waitMins = transit.DefaultWaitMinutes
			}
			stationCommuteMins := waitMins + route.TotalRailMinutes + officeAccess.Minutes
			if maxMinutes >= 0 && stationCommuteMins > maxMinutes {
				continue
			}

			st, ok := s.graph.Station(stationID)
			if !ok {
				continue
			}

			candidate := ReachableStation{
				Station:       st,
				ZoneColor:     zoneColorForMinutes(stationCommuteMins),
				CommuteMins:   stationCommuteMins,
				OneWayFareTHB: route.FareTHB,
				Transfers:     route.TransferCount,
				Route:         route,
				OfficeAccess:  officeAccess,
			}

			if prev, exists := best[stationID]; !exists || isBetterStationOption(candidate, prev) {
				best[stationID] = candidate
			}
		}
	}

	return best
}

func isBetterStationOption(cand, prev ReachableStation) bool {
	candWalk := cand.OfficeAccess.Mode == "walk"
	prevWalk := prev.OfficeAccess.Mode == "walk"
	if candWalk != prevWalk {
		if candWalk && cand.CommuteMins <= prev.CommuteMins+3 {
			return true
		}
		if prevWalk && prev.CommuteMins <= cand.CommuteMins+3 {
			return false
		}
	}

	if cand.CommuteMins != prev.CommuteMins {
		return cand.CommuteMins < prev.CommuteMins
	}
	candCost := cand.OneWayFareTHB + cand.OfficeAccess.OneWayCostTHB
	prevCost := prev.OneWayFareTHB + prev.OfficeAccess.OneWayCostTHB
	if candCost != prevCost {
		return candCost < prevCost
	}
	return cand.Route.DestID < prev.Route.DestID
}

func zoneColorForMinutes(mins int) string {
	switch {
	case mins <= 20:
		return "green"
	case mins <= 35:
		return "yellow"
	case mins <= 45:
		return "orange"
	default:
		return "red"
	}
}

// isBetterCommuteOption picks the best transit route for a single building across multiple nearby stations.
func isBetterCommuteOption(a, b ListingResult) bool {
	if a.Cost.MonthlyCommuteTHB != b.Cost.MonthlyCommuteTHB {
		// Prefer faster route if cost difference is tiny, else prefer lower monthly commute cost
		if a.Cost.OneWayMinutes+3 < b.Cost.OneWayMinutes {
			return true
		}
		if b.Cost.OneWayMinutes+3 < a.Cost.OneWayMinutes {
			return false
		}
		return a.Cost.MonthlyCommuteTHB < b.Cost.MonthlyCommuteTHB
	}
	if a.Cost.OneWayMinutes != b.Cost.OneWayMinutes {
		return a.Cost.OneWayMinutes < b.Cost.OneWayMinutes
	}
	return a.ID < b.ID
}

func sortListings(listings []ListingResult, sortBy string) {
	sort.Slice(listings, func(i, j int) bool {
		a, b := listings[i], listings[j]
		switch sortBy {
		case "commute_cost":
			if a.Cost.MonthlyCommuteTHB != b.Cost.MonthlyCommuteTHB {
				return a.Cost.MonthlyCommuteTHB < b.Cost.MonthlyCommuteTHB
			}
			if a.Cost.OneWayMinutes != b.Cost.OneWayMinutes {
				return a.Cost.OneWayMinutes < b.Cost.OneWayMinutes
			}
			return a.ID < b.ID
		case "true_cost":
			// Verified real-rent listings appear first sorted by true monthly cost;
			// unverified listings follow sorted by factual monthly commute cost.
			aVer := a.PriceStatus == "verified"
			bVer := b.PriceStatus == "verified"
			if aVer != bVer {
				return aVer
			}
			if a.Cost.TrueMonthlyCostTHB != b.Cost.TrueMonthlyCostTHB {
				return a.Cost.TrueMonthlyCostTHB < b.Cost.TrueMonthlyCostTHB
			}
			if a.Cost.OneWayMinutes != b.Cost.OneWayMinutes {
				return a.Cost.OneWayMinutes < b.Cost.OneWayMinutes
			}
			return a.ID < b.ID
		default: // "time"
			if a.Cost.OneWayMinutes != b.Cost.OneWayMinutes {
				return a.Cost.OneWayMinutes < b.Cost.OneWayMinutes
			}
			if a.Cost.MonthlyCommuteTHB != b.Cost.MonthlyCommuteTHB {
				return a.Cost.MonthlyCommuteTHB < b.Cost.MonthlyCommuteTHB
			}
			return a.ID < b.ID
		}
	})
}

func buildCompareSummary(items []ListingResult) CompareSummary {
	if len(items) == 0 {
		return CompareSummary{}
	}
	lowestCost := items[0]
	highestCost := items[0]
	lowestCommute := items[0]
	highestCommute := items[0]
	fastest := items[0]
	slowest := items[0]

	for _, it := range items[1:] {
		if it.Cost.TrueMonthlyCostTHB < lowestCost.Cost.TrueMonthlyCostTHB ||
			(it.Cost.TrueMonthlyCostTHB == lowestCost.Cost.TrueMonthlyCostTHB && it.Cost.OneWayMinutes < lowestCost.Cost.OneWayMinutes) {
			lowestCost = it
		}
		if it.Cost.TrueMonthlyCostTHB > highestCost.Cost.TrueMonthlyCostTHB {
			highestCost = it
		}
		if it.Cost.MonthlyCommuteTHB < lowestCommute.Cost.MonthlyCommuteTHB ||
			(it.Cost.MonthlyCommuteTHB == lowestCommute.Cost.MonthlyCommuteTHB && it.Cost.OneWayMinutes < lowestCommute.Cost.OneWayMinutes) {
			lowestCommute = it
		}
		if it.Cost.MonthlyCommuteTHB > highestCommute.Cost.MonthlyCommuteTHB {
			highestCommute = it
		}
		if it.Cost.OneWayMinutes < fastest.Cost.OneWayMinutes ||
			(it.Cost.OneWayMinutes == fastest.Cost.OneWayMinutes && it.Cost.MonthlyCommuteTHB < fastest.Cost.MonthlyCommuteTHB) {
			fastest = it
		}
		if it.Cost.OneWayMinutes > slowest.Cost.OneWayMinutes {
			slowest = it
		}
	}

	hoursSaved := math.Round((slowest.Cost.MonthlyCommuteHours-fastest.Cost.MonthlyCommuteHours)*10) / 10

	return CompareSummary{
		LowestTrueCostID:            lowestCost.ID,
		LowestCommuteCostID:         lowestCommute.ID,
		FastestCommuteID:            fastest.ID,
		MaxMonthlySavingsTHB:        highestCost.Cost.TrueMonthlyCostTHB - lowestCost.Cost.TrueMonthlyCostTHB,
		MaxMonthlyCommuteSavingsTHB: highestCommute.Cost.MonthlyCommuteTHB - lowestCommute.Cost.MonthlyCommuteTHB,
		MaxMonthlyHoursSaved:        hoursSaved,
	}
}
