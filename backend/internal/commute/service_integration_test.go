package commute

import (
	"context"
	"os"
	"testing"

	"github.com/tk/bangkok-commute-map/backend/internal/cache"
	"github.com/tk/bangkok-commute-map/backend/internal/config"
	"github.com/tk/bangkok-commute-map/backend/internal/db"
)

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
	_, _ = cacheClient.DeleteByPrefix(ctx, "commute:search:v1:13.737:100.560")

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
