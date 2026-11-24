package commute

import (
	"context"
	"os"
	"testing"

	"github.com/tk/bangkok-commute-map/backend/internal/cache"
	"github.com/tk/bangkok-commute-map/backend/internal/config"
	"github.com/tk/bangkok-commute-map/backend/internal/db"
	"github.com/tk/bangkok-commute-map/backend/internal/transit"
)

func TestBuildCompareSummary_TieBreaking(t *testing.T) {
	// Exact tie scenario: all 3 listings cost 6,760 THB/mo, with 22m, 17m, 21m commute.
	tiedCost := []ListingResult{
		{
			ID: "SLOWEST",
			Cost: transit.CommuteCostSummary{
				TrueMonthlyCostTHB:  6760,
				OneWayMinutes:       22,
				MonthlyCommuteHours: 16.1,
			},
		},
		{
			ID: "FASTEST",
			Cost: transit.CommuteCostSummary{
				TrueMonthlyCostTHB:  6760,
				OneWayMinutes:       17,
				MonthlyCommuteHours: 12.5,
			},
		},
		{
			ID: "MIDDLE",
			Cost: transit.CommuteCostSummary{
				TrueMonthlyCostTHB:  6760,
				OneWayMinutes:       21,
				MonthlyCommuteHours: 15.4,
			},
		},
	}

	s := buildCompareSummary(tiedCost)
	if s.MaxMonthlySavingsTHB != 0 {
		t.Fatalf("expected 0 savings on tied cost, got %d", s.MaxMonthlySavingsTHB)
	}
	if s.LowestTrueCostID != "FASTEST" {
		t.Fatalf("expected LowestTrueCostID to tie-break to FASTEST, got %s", s.LowestTrueCostID)
	}
	if s.FastestCommuteID != "FASTEST" {
		t.Fatalf("expected FastestCommuteID=FASTEST, got %s", s.FastestCommuteID)
	}
	if s.MaxMonthlyHoursSaved != 3.6 {
		t.Fatalf("expected MaxMonthlyHoursSaved=3.6, got %.1f", s.MaxMonthlyHoursSaved)
	}

	// Equal-time tie scenario: fastest should tie-break to cheaper option.
	tiedTime := []ListingResult{
		{
			ID: "EXPENSIVE_16M",
			Cost: transit.CommuteCostSummary{
				TrueMonthlyCostTHB:  17968,
				OneWayMinutes:       16,
				MonthlyCommuteHours: 11.7,
			},
		},
		{
			ID: "CHEAPER_16M",
			Cost: transit.CommuteCostSummary{
				TrueMonthlyCostTHB:  14040,
				OneWayMinutes:       16,
				MonthlyCommuteHours: 11.7,
			},
		},
	}
	s2 := buildCompareSummary(tiedTime)
	if s2.FastestCommuteID != "CHEAPER_16M" {
		t.Fatalf("expected FastestCommuteID to tie-break to CHEAPER_16M, got %s", s2.FastestCommuteID)
	}
}

func TestIsBetterStationOption_PrefersWalkOverWinForSmallTimeGain(t *testing.T) {
	walkOption := ReachableStation{
		CommuteMins:   12,
		OneWayFareTHB: 22,
		OfficeAccess:  transit.AccessLeg{DistanceM: 85, Mode: "walk", Minutes: 1, OneWayCostTHB: 0},
		Route:         transit.RouteResult{DestID: "MRT_BL20"},
	}
	winOption := ReachableStation{
		CommuteMins:   11,
		OneWayFareTHB: 20,
		OfficeAccess:  transit.AccessLeg{DistanceM: 890, Mode: "win", Minutes: 3, OneWayCostTHB: 15},
		Route:         transit.RouteResult{DestID: "MRT_BL19"},
	}
	if !isBetterStationOption(walkOption, winOption) {
		t.Fatalf("expected walkable exit (12m, ฿22) to beat paid win exit (11m, ฿35)")
	}
	if isBetterStationOption(winOption, walkOption) {
		t.Fatalf("expected paid win exit saving only 1 min not to beat walkable exit")
	}

	// If win saves >= 4 minutes (e.g. avoids a cross-system transfer), win should still win.
	fastWinOption := winOption
	fastWinOption.CommuteMins = 8
	if !isBetterStationOption(fastWinOption, walkOption) {
		t.Fatalf("expected win exit saving 4+ min to win over much slower walk exit")
	}
}

func TestQuantizedCacheKey_NearbyPointsShareKey(t *testing.T) {
	p1 := NormalizeParams(SearchParams{Lat: 13.73671, Lng: 100.56031, MaxMinutes: 35, MaxBudget: 18000, Workdays: 22, WalkMaxM: 800})
	p2 := NormalizeParams(SearchParams{Lat: 13.73699, Lng: 100.56012, MaxMinutes: 35, MaxBudget: 18000, Workdays: 22, WalkMaxM: 800})

	k1 := QuantizedCacheKey(p1)
	k2 := QuantizedCacheKey(p2)
	if k1 != k2 {
		t.Errorf("expected nearby points within ~100m to share cache key, got %q vs %q", k1, k2)
	}
}

func TestZoneColorForMinutes(t *testing.T) {
	cases := map[int]string{
		10: "green",
		20: "green",
		21: "yellow",
		35: "yellow",
		36: "orange",
		45: "orange",
		46: "red",
	}
	for mins, want := range cases {
		if got := zoneColorForMinutes(mins); got != want {
			t.Errorf("zoneColorForMinutes(%d) = %q, want %q", mins, got, want)
		}
	}
}

func TestPostGISIntegration_SearchAndCompare(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		if os.Getenv("REQUIRE_DB_TESTS") == "1" {
			t.Fatalf("database required but unavailable: %v", err)
		}
		t.Skipf("skipping PostGIS integration test (DB unavailable: %v)", err)
	}
	defer pool.Close()

	repo := NewRepository(pool)
	graph, err := repo.LoadTransitGraph(ctx)
	if err != nil {
		t.Fatalf("LoadTransitGraph failed: %v", err)
	}
	if len(graph.AllStations()) < 70 {
		t.Skipf("database not seeded yet (found %d stations); run `make seed` first", len(graph.AllStations()))
	}

	// Verify KNN nearest stations around Asok intersection.
	nearest, err := repo.FindNearestStations(ctx, 13.7367, 100.5603, 3)
	if err != nil {
		t.Fatalf("FindNearestStations failed: %v", err)
	}
	if len(nearest) != 3 {
		t.Fatalf("expected 3 nearest stations, got %d", len(nearest))
	}
	if nearest[0].Station.ID != "BTS_E4" && nearest[0].Station.ID != "MRT_BL22" {
		t.Errorf("expected BTS_E4 or MRT_BL22 nearest to Asok, got %s (%.1fm)", nearest[0].Station.ID, nearest[0].DistanceM)
	}

	cacheClient := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	defer func() { _ = cacheClient.Close() }()
	_, _ = cacheClient.DeleteByPrefix(ctx, "commute:search:")

	svc := NewService(repo, graph, cacheClient)

	params := SearchParams{
		Lat:        13.7367,
		Lng:        100.5603,
		MaxMinutes: 35,
		MaxBudget:  25000,
		Workdays:   22,
		WalkMaxM:   800,
	}

	// First call: cache MISS.
	resp, hit, err := svc.Search(ctx, params)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if hit {
		t.Errorf("expected cache MISS on first search")
	}
	if len(resp.ReachableStations) == 0 {
		t.Fatalf("expected reachable stations around Asok")
	}
	if len(resp.Listings) < 2 {
		t.Fatalf("expected at least 2 matching listings, got %d", len(resp.Listings))
	}

	// Verify listings are sorted by TrueMonthlyCostTHB ascending.
	for i := 1; i < len(resp.Listings); i++ {
		if resp.Listings[i].Cost.TrueMonthlyCostTHB < resp.Listings[i-1].Cost.TrueMonthlyCostTHB {
			t.Errorf("listings not sorted by TrueMonthlyCostTHB at index %d: %d < %d",
				i, resp.Listings[i].Cost.TrueMonthlyCostTHB, resp.Listings[i-1].Cost.TrueMonthlyCostTHB)
		}
	}

	// Second call with slightly offset coordinates (~20m): should be a Redis cache HIT when Redis is up.
	if cacheClient.Ping(ctx) == nil {
		paramsNearby := params
		paramsNearby.Lat = 13.7368
		paramsNearby.Lng = 100.5604
		_, hit2, err := svc.Search(ctx, paramsNearby)
		if err != nil {
			t.Fatalf("second Search failed: %v", err)
		}
		if !hit2 {
			t.Errorf("expected cache HIT on quantized nearby coordinates")
		}
	}

	// Test Compare with first and last listing from search results.
	compareResp, err := svc.Compare(ctx, CompareRequest{
		Office:     OfficePoint{Lat: 13.7367, Lng: 100.5603},
		Workdays:   22,
		WalkMaxM:   800,
		ListingIDs: []string{resp.Listings[0].ID, resp.Listings[len(resp.Listings)-1].ID},
	})
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}
	if len(compareResp.Items) != 2 {
		t.Fatalf("expected 2 compared items, got %d", len(compareResp.Items))
	}
	if compareResp.Summary.LowestTrueCostID == "" || compareResp.Summary.FastestCommuteID == "" {
		t.Errorf("expected non-empty compare summary IDs, got %+v", compareResp.Summary)
	}
}
