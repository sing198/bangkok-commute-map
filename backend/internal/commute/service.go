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
	searchCacheTTL = 15 * time.Minute
)

// SearchParams defines query filters for /api/commute/search.
type SearchParams struct {
	Lat        float64
	Lng        float64
	MaxMinutes int
	MaxBudget  int
	Workdays   int
	WalkMaxM   int
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

// ListingResult is a listing enriched with door-to-door commute time, route, and True Monthly Cost.
type ListingResult struct {
	ID             string                     `json:"id"`
	TitleTH        string                     `json:"title_th"`
	TitleEN        string                     `json:"title_en"`
	PropertyType   string                     `json:"property_type"`
	District       string                     `json:"district"`
	MonthlyRentTHB int                        `json:"monthly_rent_thb"`
	RoomSizeSqm    float64                    `json:"room_size_sqm"`
	Bedrooms       int                        `json:"bedrooms"`
	Lat            float64                    `json:"lat"`
	Lng            float64                    `json:"lng"`
	OriginStation  transit.Station            `json:"origin_station"`
	DestStation    transit.Station            `json:"dest_station"`
	HomeAccess     transit.AccessLeg          `json:"home_access"`
	OfficeAccess   transit.AccessLeg          `json:"office_access"`
	Route          transit.RouteResult        `json:"route"`
	Cost           transit.CommuteCostSummary `json:"cost"`
	ZoneColor      string                     `json:"zone_color"`
}

// SearchResponse is returned by GET /api/commute/search.
type SearchResponse struct {
	Office            OfficePoint        `json:"office"`
	Params            SearchMeta         `json:"params"`
	NearestStations   []NearestStation   `json:"nearest_office_stations"`
	ReachableStations []ReachableStation `json:"reachable_stations"`
	Listings          []ListingResult    `json:"listings"`
}

type OfficePoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type SearchMeta struct {
	MaxMinutes int `json:"max_minutes"`
	MaxBudget  int `json:"max_budget"`
	Workdays   int `json:"workdays"`
	WalkMaxM   int `json:"walk_max_m"`
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
	LowestTrueCostID      string  `json:"lowest_true_cost_id"`
	FastestCommuteID      string  `json:"fastest_commute_id"`
	MaxMonthlySavingsTHB  int     `json:"max_monthly_savings_thb"`
	MaxMonthlyHoursSaved  float64 `json:"max_monthly_hours_saved"`
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

// Service orchestrates PostGIS spatial queries, in-memory Dijkstra routing, and Redis caching.
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
	return p
}

// QuantizedCacheKey rounds coordinates to ~100m precision (3 decimal places)
// so nearby office clicks share cache entries and maximize Redis hit rate.
func QuantizedCacheKey(p SearchParams) string {
	qLat := math.Round(p.Lat*1000) / 1000
	qLng := math.Round(p.Lng*1000) / 1000
	return fmt.Sprintf("commute:search:v1:%.3f:%.3f:%d:%d:%d:%d",
		qLat, qLng, p.MaxMinutes, p.MaxBudget, p.Workdays, p.WalkMaxM)
}

// Search finds reachable stations and matching listings sorted by True Monthly Cost.
func (s *Service) Search(ctx context.Context, raw SearchParams) (SearchResponse, bool, error) {
	p := NormalizeParams(raw)
	cacheKey := QuantizedCacheKey(p)

	if cached, ok := s.cache.Get(ctx, cacheKey); ok {
		var resp SearchResponse
		if err := json.Unmarshal([]byte(cached), &resp); err == nil {
			return resp, true, nil
		}
	}

	nearest, err := s.repo.FindNearestStations(ctx, p.Lat, p.Lng, 3)
	if err != nil {
		return SearchResponse{}, false, err
	}
	if len(nearest) == 0 {
		return SearchResponse{}, false, fmt.Errorf("no transit stations found in database")
	}

	// For every station in Bangkok, compute the fastest route to the office
	// via any of the 3 nearest office stations.
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

	// Query listings within walking/win radius of reachable stations via PostGIS ST_DWithin.
	searchRadiusM := p.WalkMaxM
	if searchRadiusM < 1000 {
		searchRadiusM = 1000
	}
	rawListings, err := s.repo.FindListingsNearStations(ctx, reachableIDs, searchRadiusM, p.MaxBudget)
	if err != nil {
		return SearchResponse{}, false, err
	}

	// Deduplicate listings across multiple nearby stations by picking the option
	// with the lowest True Monthly Cost (tie-break: lowest one-way commute time).
	bestByListing := make(map[string]ListingResult, len(rawListings))
	for _, rec := range rawListings {
		stOpt, ok := bestStationOption[rec.StationID]
		if !ok {
			continue
		}
		homeAccess := transit.CalculateAccess(rec.WalkToStationM, p.WalkMaxM)
		cost := transit.CalculateMonthlyCost(rec.MonthlyRentTHB, homeAccess, stOpt.OfficeAccess, stOpt.Route, p.Workdays)
		if cost.OneWayMinutes > p.MaxMinutes {
			continue
		}
		destSt, _ := s.graph.Station(stOpt.Route.DestID)
		candidate := ListingResult{
			ID:             rec.ID,
			TitleTH:        rec.TitleTH,
			TitleEN:        rec.TitleEN,
			PropertyType:   rec.PropertyType,
			District:       rec.District,
			MonthlyRentTHB: rec.MonthlyRentTHB,
			RoomSizeSqm:    rec.RoomSizeSqm,
			Bedrooms:       rec.Bedrooms,
			Lat:            rec.Lat,
			Lng:            rec.Lng,
			OriginStation:  stOpt.Station,
			DestStation:    destSt,
			HomeAccess:     homeAccess,
			OfficeAccess:   stOpt.OfficeAccess,
			Route:          stOpt.Route,
			Cost:           cost,
			ZoneColor:      zoneColorForMinutes(cost.OneWayMinutes),
		}

		if existing, exists := bestByListing[rec.ID]; !exists || isBetterOption(candidate, existing) {
			bestByListing[rec.ID] = candidate
		}
	}

	listings := make([]ListingResult, 0, len(bestByListing))
	for _, item := range bestByListing {
		listings = append(listings, item)
	}
	sort.Slice(listings, func(i, j int) bool {
		return isBetterOption(listings[i], listings[j])
	})

	resp := SearchResponse{
		Office:            OfficePoint{Lat: p.Lat, Lng: p.Lng},
		Params:            SearchMeta{MaxMinutes: p.MaxMinutes, MaxBudget: p.MaxBudget, Workdays: p.Workdays, WalkMaxM: p.WalkMaxM},
		NearestStations:   nearest,
		ReachableStations: reachableStations,
		Listings:          listings,
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

	nearest, err := s.repo.FindNearestStations(ctx, req.Office.Lat, req.Office.Lng, 3)
	if err != nil {
		return CompareResponse{}, err
	}
	if len(nearest) == 0 {
		return CompareResponse{}, fmt.Errorf("no transit stations found in database")
	}

	records, err := s.repo.FindListingsByIDs(ctx, req.ListingIDs)
	if err != nil {
		return CompareResponse{}, err
	}
	if len(records) == 0 {
		return CompareResponse{}, fmt.Errorf("no matching listings found")
	}

	// Precompute unrestricted best routes to the office from every station.
	bestStationOption := s.computeBestStationOptions(nearest, -1, walkMaxM)

	recordByID := make(map[string]ListingRecord, len(records))
	for _, rec := range records {
		recordByID[rec.ID] = rec
	}

	items := make([]ListingResult, 0, len(req.ListingIDs))
	for _, id := range req.ListingIDs {
		rec, ok := recordByID[id]
		if !ok {
			continue
		}
		stOpt, ok := bestStationOption[rec.StationID]
		if !ok {
			continue
		}
		homeAccess := transit.CalculateAccess(rec.WalkToStationM, walkMaxM)
		cost := transit.CalculateMonthlyCost(rec.MonthlyRentTHB, homeAccess, stOpt.OfficeAccess, stOpt.Route, workdays)
		destSt, _ := s.graph.Station(stOpt.Route.DestID)

		items = append(items, ListingResult{
			ID:             rec.ID,
			TitleTH:        rec.TitleTH,
			TitleEN:        rec.TitleEN,
			PropertyType:   rec.PropertyType,
			District:       rec.District,
			MonthlyRentTHB: rec.MonthlyRentTHB,
			RoomSizeSqm:    rec.RoomSizeSqm,
			Bedrooms:       rec.Bedrooms,
			Lat:            rec.Lat,
			Lng:            rec.Lng,
			OriginStation:  stOpt.Station,
			DestStation:    destSt,
			HomeAccess:     homeAccess,
			OfficeAccess:   stOpt.OfficeAccess,
			Route:          stOpt.Route,
			Cost:           cost,
			ZoneColor:      zoneColorForMinutes(cost.OneWayMinutes),
		})
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
		officeAccess := transit.CalculateAccess(ns.DistanceM, walkMaxM)
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
			// Station-to-office time (excludes home-to-station walk, which is per-listing).
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

			if prev, exists := best[stationID]; !exists ||
				candidate.CommuteMins < prev.CommuteMins ||
				(candidate.CommuteMins == prev.CommuteMins && candidate.OneWayFareTHB < prev.OneWayFareTHB) {
				best[stationID] = candidate
			}
		}
	}

	return best
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
		if it.Cost.TrueMonthlyCostTHB < lowestCost.Cost.TrueMonthlyCostTHB {
			lowestCost = it
		}
		if it.Cost.TrueMonthlyCostTHB > highestCost.Cost.TrueMonthlyCostTHB {
			highestCost = it
		}
		if it.Cost.OneWayMinutes < fastest.Cost.OneWayMinutes {
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
