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
	Lat        float64
	Lng        float64
	MaxMinutes int
	MaxBudget  int
	Workdays   int
	WalkMaxM   int
	Sort       string // "true_cost" (default) or "time"
	Limit      int
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

// ListingResult is a listing enriched with real OSRM door-to-door commute time, route,
// cited estimated rent range, crowdsourced verified rent, and True Monthly Cost.
type ListingResult struct {
	ID                    string                     `json:"id"`
	OSMID                 string                     `json:"osm_id,omitempty"`
	TitleTH               string                     `json:"title_th"`
	TitleEN               string                     `json:"title_en"`
	PropertyType          string                     `json:"property_type"`
	District              string                     `json:"district"`
	MonthlyRentTHB        int                        `json:"monthly_rent_thb"`
	EstimatedRentMinTHB   int                        `json:"estimated_rent_min_thb"`
	EstimatedRentMaxTHB   int                        `json:"estimated_rent_max_thb"`
	PriceStatus           string                     `json:"price_status"` // "verified" or "estimated"
	PriceSource           string                     `json:"price_source,omitempty"`
	DataSource            string                     `json:"data_source,omitempty"`
	VerifiedRentMedianTHB int                        `json:"verified_rent_median_thb,omitempty"`
	VerifiedReportCount   int                        `json:"verified_report_count"`
	LatestReportMonth     string                     `json:"latest_report_month,omitempty"`
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
	MaxMinutes int    `json:"max_minutes"`
	MaxBudget  int    `json:"max_budget"`
	Workdays   int    `json:"workdays"`
	WalkMaxM   int    `json:"walk_max_m"`
	Sort       string `json:"sort"`
	Limit      int    `json:"limit"`
}

// CompareRequest is the JSON payload for POST /api/commute/compare.
type CompareRequest struct {
	Office     OfficePoint `json:"office" binding:"required"`
	Workdays   int         `json:"workdays"`
	WalkMaxM   int         `json:"walk_max_m"`
	ListingIDs []string    `json:"listing_ids" binding:"required,min=2,max=3"`
}

// CompareSummary highlights the best value and fastest commute trade-offs.
type CompareSummary struct {
	LowestTrueCostID     string  `json:"lowest_true_cost_id"`
	FastestCommuteID     string  `json:"fastest_commute_id"`
	MaxMonthlySavingsTHB int     `json:"max_monthly_savings_thb"`
	MaxMonthlyHoursSaved float64 `json:"max_monthly_hours_saved"`
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
	if p.Sort != "time" {
		p.Sort = "true_cost"
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
	return fmt.Sprintf("commute:search:v3:%.3f:%.3f:%d:%d:%d:%d:%s:%d",
		qLat, qLng, p.MaxMinutes, p.MaxBudget, p.Workdays, p.WalkMaxM, p.Sort, p.Limit)
}

func buildListingResult(rec ListingRecord, stOpt ReachableStation, destSt transit.Station, workdays, walkMaxM int) ListingResult {
	distSrc := rec.DistanceSource
	if distSrc == "" {
		distSrc = "osrm_foot"
	}
	homeAccess := transit.CalculateAccessWithSource(rec.WalkToStationM, walkMaxM, distSrc)

	effectiveRent := rec.MonthlyRentTHB
	priceStatus := "estimated"
	if rec.VerifiedReportCount > 0 && rec.VerifiedRentMedianTHB > 0 {
		effectiveRent = rec.VerifiedRentMedianTHB
		priceStatus = "verified"
	}

	cost := transit.CalculateMonthlyCost(effectiveRent, homeAccess, stOpt.OfficeAccess, stOpt.Route, workdays)

	return ListingResult{
		ID:                    rec.ID,
		OSMID:                 rec.OSMID,
		TitleTH:               rec.TitleTH,
		TitleEN:               rec.TitleEN,
		PropertyType:          rec.PropertyType,
		District:              rec.District,
		MonthlyRentTHB:        effectiveRent,
		EstimatedRentMinTHB:   rec.EstimatedRentMinTHB,
		EstimatedRentMaxTHB:   rec.EstimatedRentMaxTHB,
		PriceStatus:           priceStatus,
		PriceSource:           rec.PriceSource,
		DataSource:            rec.DataSource,
		VerifiedRentMedianTHB: rec.VerifiedRentMedianTHB,
		VerifiedReportCount:   rec.VerifiedReportCount,
		LatestReportMonth:     rec.LatestReportMonth,
		RoomSizeSqm:           rec.RoomSizeSqm,
		Bedrooms:              rec.Bedrooms,
		Lat:                   rec.Lat,
		Lng:                   rec.Lng,
		OriginStation:         stOpt.Station,
		DestStation:           destSt,
		HomeAccess:            homeAccess,
		OfficeAccess:          stOpt.OfficeAccess,
		Route:                 stOpt.Route,
		Cost:                  cost,
		ZoneColor:             zoneColorForMinutes(cost.OneWayMinutes),
	}
}

// Search finds reachable stations and matching listings sorted by True Monthly Cost or commute time.
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

	// Query listings using precomputed real OSRM street walking distances (up to 2,500m).
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

	// Deduplicate listings across multiple nearby stations by picking the best station per listing.
	bestByListing := make(map[string]ListingResult, len(rawListings))
	for _, rec := range rawListings {
		stOpt, ok := bestStationOption[rec.StationID]
		if !ok {
			continue
		}
		destSt, _ := s.graph.Station(stOpt.Route.DestID)
		candidate := buildListingResult(rec, stOpt, destSt, p.Workdays, p.WalkMaxM)
		if candidate.Cost.OneWayMinutes > p.MaxMinutes {
			continue
		}
		if existing, exists := bestByListing[rec.ID]; !exists || isBetterOption(candidate, existing) {
			bestByListing[rec.ID] = candidate
		}
	}

	listings := make([]ListingResult, 0, len(bestByListing))
	for _, item := range bestByListing {
		listings = append(listings, item)
	}
	if p.Sort == "time" {
		sort.Slice(listings, func(i, j int) bool {
			if listings[i].Cost.OneWayMinutes != listings[j].Cost.OneWayMinutes {
				return listings[i].Cost.OneWayMinutes < listings[j].Cost.OneWayMinutes
			}
			return isBetterOption(listings[i], listings[j])
		})
	} else {
		sort.Slice(listings, func(i, j int) bool {
			return isBetterOption(listings[i], listings[j])
		})
	}

	totalCount := len(listings)
	if len(listings) > p.Limit {
		listings = listings[:p.Limit]
	}

	resp := SearchResponse{
		Office: OfficePoint{Lat: p.Lat, Lng: p.Lng},
		Params: SearchMeta{
			MaxMinutes: p.MaxMinutes,
			MaxBudget:  p.MaxBudget,
			Workdays:   p.Workdays,
			WalkMaxM:   p.WalkMaxM,
			Sort:       p.Sort,
			Limit:      p.Limit,
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
		candidate := buildListingResult(rec, stOpt, destSt, workdays, walkMaxM)
		if existing, exists := bestByListing[rec.ID]; !exists || isBetterOption(candidate, existing) {
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
		// Calibrated real-street office walk estimate using Bangkok's empirical 1.40x detour factor
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

// isBetterStationOption compares two destination-station choices from the same origin station.
// When a walkable office exit is within 3 minutes of a paid motorcycle-taxi (win) exit,
// it prefers walking to avoid adding an unnecessary THB 15–25/trip feeder surcharge.
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

func isBetterOption(a, b ListingResult) bool {
	if a.Cost.TrueMonthlyCostTHB != b.Cost.TrueMonthlyCostTHB {
		return a.Cost.TrueMonthlyCostTHB < b.Cost.TrueMonthlyCostTHB
	}
	if a.Cost.OneWayMinutes != b.Cost.OneWayMinutes {
		return a.Cost.OneWayMinutes < b.Cost.OneWayMinutes
	}
	return a.ID < b.ID
}

func buildCompareSummary(items []ListingResult) CompareSummary {
	if len(items) == 0 {
		return CompareSummary{}
	}
	lowestCost := items[0]
	highestCost := items[0]
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
		if it.Cost.OneWayMinutes < fastest.Cost.OneWayMinutes ||
			(it.Cost.OneWayMinutes == fastest.Cost.OneWayMinutes && it.Cost.TrueMonthlyCostTHB < fastest.Cost.TrueMonthlyCostTHB) {
			fastest = it
		}
		if it.Cost.OneWayMinutes > slowest.Cost.OneWayMinutes {
			slowest = it
		}
	}

	hoursSaved := math.Round((slowest.Cost.MonthlyCommuteHours-fastest.Cost.MonthlyCommuteHours)*10) / 10

	return CompareSummary{
		LowestTrueCostID:     lowestCost.ID,
		FastestCommuteID:     fastest.ID,
		MaxMonthlySavingsTHB: highestCost.Cost.TrueMonthlyCostTHB - lowestCost.Cost.TrueMonthlyCostTHB,
		MaxMonthlyHoursSaved: hoursSaved,
	}
}
