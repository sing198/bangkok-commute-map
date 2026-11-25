package transit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadRealTestGraph(t *testing.T) *Graph {
	t.Helper()
	dataDir := filepath.Join("..", "..", "data")

	var stations []Station
	rawStations, err := os.ReadFile(filepath.Join(dataDir, "stations.json"))
	if err != nil {
		t.Fatalf("read stations.json: %v", err)
	}
	if err := json.Unmarshal(rawStations, &stations); err != nil {
		t.Fatalf("unmarshal stations.json: %v", err)
	}

	var undirected []Edge
	rawEdges, err := os.ReadFile(filepath.Join(dataDir, "edges.json"))
	if err != nil {
		t.Fatalf("read edges.json: %v", err)
	}
	if err := json.Unmarshal(rawEdges, &undirected); err != nil {
		t.Fatalf("unmarshal edges.json: %v", err)
	}
	edges := make([]Edge, 0, len(undirected)*2)
	for _, e := range undirected {
		edges = append(edges, e, Edge{
			From:          e.To,
			To:            e.From,
			TravelMinutes: e.TravelMinutes,
			IsTransfer:    e.IsTransfer,
		})
	}

	var rules []FareRule
	rawRules, err := os.ReadFile(filepath.Join(dataDir, "fare_rules.json"))
	if err != nil {
		t.Fatalf("read fare_rules.json: %v", err)
	}
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		t.Fatalf("unmarshal fare_rules.json: %v", err)
	}

	return NewGraph(stations, edges, rules)
}

func TestShortestPath_DirectBTSCore(t *testing.T) {
	g := loadRealTestGraph(t)

	// BTS On Nut (E9) -> BTS Asok (E4): 5 hops, core only, 10 min, 35 THB
	res, err := g.ShortestPath("BTS_E9", "BTS_E4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalRailMinutes != 10 {
		t.Errorf("expected 10 min, got %d", res.TotalRailMinutes)
	}
	if res.TransferCount != 0 {
		t.Errorf("expected 0 transfers, got %d", res.TransferCount)
	}
	if res.FareTHB != 35 {
		t.Errorf("expected 35 THB for 5 core BTS hops, got %d", res.FareTHB)
	}
	if len(res.Legs) != 1 || res.Legs[0].HasExtension {
		t.Errorf("expected 1 core BTS leg, got %+v", res.Legs)
	}
}

func TestShortestPath_BTSExtensionAndCap(t *testing.T) {
	g := loadRealTestGraph(t)

	// BTS Bearing (E14, ext) -> BTS Asok (E4, core): 5 core hops (35) + 15 ext = 50 THB
	res, err := g.ShortestPath("BTS_E14", "BTS_E4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.FareTHB != 50 {
		t.Errorf("expected 50 THB (35 core + 15 ext), got %d (legs: %+v)", res.FareTHB, res.Legs)
	}

	// BTS Kheha (E23, ext) -> BTS Ari (N5, core): stays on BTS Sukhumvit and caps at 62 THB
	longRes, err := g.ShortestPath("BTS_E23", "BTS_N5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if longRes.FareTHB != BTSMaxFareTHB {
		t.Errorf("expected capped fare %d THB, got %d (legs: %+v)", BTSMaxFareTHB, longRes.FareTHB, longRes.Legs)
	}

	// BTS Extension-only ride: Bearing (E14) -> Bang Chak (E10): flat 15 THB
	extOnly, err := g.ShortestPath("BTS_E14", "BTS_E10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if extOnly.FareTHB != BTSExtensionFlatFareTHB {
		t.Errorf("expected flat %d THB for extension-only ride, got %d", BTSExtensionFlatFareTHB, extOnly.FareTHB)
	}
}

func TestShortestPath_BTSSiamSameOperatorTransfer(t *testing.T) {
	g := loadRealTestGraph(t)

	// BTS Ari (N5, Sukhumvit) -> BTS Sala Daeng (S2, Silom) via Siam (BTS_CEN):
	// Rail edges: N5-N4(2) + N4-N3(2) + N3-N2(2) + N2-N1(2) + N1-CEN(2) + CEN-S1(2) + S1-S2(2) = 14 min
	// Plus +3 min same-operator line switch at Siam = 17 min total.
	// Single BTS ticket for 7 core hops = 43 THB.
	res, err := g.ShortestPath("BTS_N5", "BTS_S2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TransferCount != 1 {
		t.Errorf("expected 1 transfer at Siam, got %d", res.TransferCount)
	}
	if res.TransferMinutes != SameOperatorLineTransferPenalty {
		t.Errorf("expected %d transfer minutes, got %d", SameOperatorLineTransferPenalty, res.TransferMinutes)
	}
	if res.TotalRailMinutes != 17 {
		t.Errorf("expected 17 total rail minutes, got %d", res.TotalRailMinutes)
	}
	if res.FareTHB != 43 {
		t.Errorf("expected 43 THB single BTS ticket for 7 hops, got %d", res.FareTHB)
	}
	if len(res.Legs) != 1 || res.Legs[0].Operator != "BTS" {
		t.Errorf("expected single BTS fare leg, got %+v", res.Legs)
	}
}

func TestShortestPath_CrossSystemBTSAndMRT(t *testing.T) {
	g := loadRealTestGraph(t)

	// MRT Phra Ram 9 (BL20) -> BTS Thong Lo (E6):
	// MRT BL20 -> BL21 (2) -> BL22 Sukhumvit (2) = 2 hops = 20 THB
	// Transfer BL22 -> BTS_E4 Asok = 6 min
	// BTS E4 -> E5 (2) -> E6 Thong Lo (2) = 2 hops = 25 THB
	// Total = 14 min, 1 transfer, 2 separate tickets = 45 THB
	res, err := g.ShortestPath("MRT_BL20", "BTS_E6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalRailMinutes != 14 {
		t.Errorf("expected 14 min, got %d (path: %v)", res.TotalRailMinutes, res.StationIDs)
	}
	if res.TransferCount != 1 {
		t.Errorf("expected 1 cross-system transfer, got %d", res.TransferCount)
	}
	if res.FareTHB != 45 {
		t.Errorf("expected 45 THB (20 MRT + 25 BTS), got %d (legs: %+v)", res.FareTHB, res.Legs)
	}
	if len(res.Legs) != 2 {
		t.Errorf("expected 2 operator fare legs, got %d", len(res.Legs))
	}
}

func TestShortestPath_CrossSystemARLAndMRT(t *testing.T) {
	g := loadRealTestGraph(t)

	// ARL Ramkhamhaeng (A5) -> MRT Phra Ram 9 (BL20):
	// ARL A5 -> A6 Makkasan (4 min, 1 hop = 15 THB)
	// Transfer A6 -> BL21 Phetchaburi (7 min)
	// MRT BL21 -> BL20 Phra Ram 9 (2 min, 1 hop = 17 THB)
	// Total = 13 min, 32 THB
	res, err := g.ShortestPath("ARL_A5", "MRT_BL20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalRailMinutes != 13 {
		t.Errorf("expected 13 min, got %d", res.TotalRailMinutes)
	}
	if res.FareTHB != 32 {
		t.Errorf("expected 32 THB (15 ARL + 17 MRT), got %d (legs: %+v)", res.FareTHB, res.Legs)
	}
}

func TestShortestPath_MRTSameOperatorBlueToPurple(t *testing.T) {
	g := loadRealTestGraph(t)

	// MRT Bang Son (PP15) -> MRT Chatuchak Park (BL13) via Tao Poon (BL10):
	// PP15 -> BL10 (Tao Poon, shared Blue/Purple hub) -> BL11 (Bang Sue) -> BL12 (Kamphaeng Phet) -> BL13
	// Single MRT operator ticket (4 hops = 25 THB), 1 same-operator transfer at Tao Poon.
	res, err := g.ShortestPath("MRT_PP15", "MRT_BL13")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Legs) != 1 || res.Legs[0].Operator != "MRT" {
		t.Errorf("expected single MRT fare leg across Tao Poon, got %+v", res.Legs)
	}
	if res.FareTHB != 25 {
		t.Errorf("expected 25 THB for 4 MRT hops, got %d (path: %v)", res.FareTHB, res.StationIDs)
	}
	if res.TransferCount != 1 {
		t.Errorf("expected 1 transfer at Tao Poon, got %d", res.TransferCount)
	}
}

func TestShortestPath_NewBangkokLines(t *testing.T) {
	g := loadRealTestGraph(t)

	// 1. MRT Yellow Line (Lat Phrao 71 YL04 -> Lat Phrao YL01 -> MRT BL15 -> Phra Ram 9 BL20)
	yl, err := g.ShortestPath("MRT_YL04", "MRT_BL20")
	if err != nil {
		t.Fatalf("yellow line route failed: %v", err)
	}
	if len(yl.Legs) != 2 || yl.Legs[0].Operator != "MRT_YL" || yl.Legs[1].Operator != "MRT" {
		t.Errorf("expected MRT_YL + MRT legs, got %+v", yl.Legs)
	}

	// 1b. MRT Yellow Line (Bang Kapi YL08 -> Hua Mak ARL shortcut -> Phra Ram 9 BL20)
	ylArl, err := g.ShortestPath("MRT_YL08", "MRT_BL20")
	if err != nil {
		t.Fatalf("yellow+arl route failed: %v", err)
	}
	if len(ylArl.Legs) < 2 || ylArl.Legs[0].Operator != "MRT_YL" {
		t.Errorf("expected MRT_YL start leg, got %+v", ylArl.Legs)
	}

	// 2. MRT Pink Line + IMPACT Link branch (MT02 -> PK10 -> PK16 Wat Phra Sri Mahathat -> BTS N17 -> BTS N5 Ari)
	pk, err := g.ShortestPath("MRT_MT02", "BTS_N5")
	if err != nil {
		t.Fatalf("pink line branch route failed: %v", err)
	}
	if pk.TransferCount < 2 || pk.FareTHB < 40 {
		t.Errorf("expected multi-transfer Pink+BTS route, got %+v", pk)
	}

	// 3. BTS Gold Line (G2 Charoen Nakhon -> G1 -> BTS S7 -> S2 Sala Daeng)
	gold, err := g.ShortestPath("BTS_G2", "BTS_S2")
	if err != nil {
		t.Fatalf("gold line route failed: %v", err)
	}
	if len(gold.Legs) != 2 || gold.Legs[0].Operator != "BTS_GOLD" || gold.Legs[0].FareTHB != 16 {
		t.Errorf("expected BTS_GOLD 16 THB + BTS legs, got %+v", gold.Legs)
	}

	// 4. SRT Dark Red Line (Don Mueang RN08 -> Krung Thep Aphiwat RN01 -> MRT BL11 Bang Sue -> MRT BL13 Chatuchak)
	red, err := g.ShortestPath("SRT_RN08", "MRT_BL13")
	if err != nil {
		t.Fatalf("red line route failed: %v", err)
	}
	if len(red.Legs) != 2 || red.Legs[0].Operator != "SRT_RED" {
		t.Errorf("expected SRT_RED + MRT legs, got %+v", red.Legs)
	}

	// 5. Calibrated Bangkok office detour factor (1.40x)
	off := CalculateOfficeAccess(500, 800)
	if off.DistanceM != 700 || off.Mode != "walk" || off.DistanceSource != "estimated_detour" {
		t.Errorf("unexpected office access with 1.40x detour: %+v", off)
	}
}

func TestReachableToDestMatchesShortestPath(t *testing.T) {
	g := loadRealTestGraph(t)

	reachable, err := g.ReachableToDest("BTS_E4", 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reachable) < 15 {
		t.Fatalf("expected at least 15 stations within 20 rail mins of Asok, got %d", len(reachable))
	}

	// Spot-check On Nut -> Asok matches ShortestPath exactly
	got, ok := reachable["BTS_E9"]
	if !ok {
		t.Fatalf("expected BTS_E9 to be reachable from BTS_E4 within 20 mins")
	}
	direct, _ := g.ShortestPath("BTS_E9", "BTS_E4")
	if got.TotalRailMinutes != direct.TotalRailMinutes || got.FareTHB != direct.FareTHB {
		t.Errorf("mismatch: reachable=%+v vs direct=%+v", got, direct)
	}
}

func TestCalculateAccess_WalkVsWin(t *testing.T) {
	walk := CalculateAccess(400, 800)
	if walk.Mode != "walk" || walk.Minutes != 5 || walk.OneWayCostTHB != 0 {
		t.Errorf("unexpected walk leg: %+v", walk)
	}

	winNear := CalculateAccess(950, 800)
	if winNear.Mode != "win" || winNear.Minutes != 4 || winNear.OneWayCostTHB != 15 {
		t.Errorf("unexpected near win leg: %+v", winNear)
	}

	winFar := CalculateAccess(1500, 800)
	if winFar.Mode != "win" || winFar.Minutes != 5 || winFar.OneWayCostTHB != 20 {
		t.Errorf("unexpected far win leg: %+v", winFar)
	}
}

func TestCalculateMonthlyCost(t *testing.T) {
	home := CalculateAccess(400, 800)   // 5 min walk, 0 THB
	office := CalculateAccess(240, 800) // 3 min walk, 0 THB
	route := RouteResult{
		OriginID:         "BTS_E9",
		DestID:           "BTS_E4",
		StationIDs:       []string{"BTS_E9", "BTS_E8", "BTS_E7", "BTS_E6", "BTS_E5", "BTS_E4"},
		TotalRailMinutes: 10,
		FareTHB:          35,
	}

	// One-way = 5 + 3 (wait) + 10 + 3 = 21 min
	// Monthly rail (22 workdays) = 35 * 2 * 22 = 1540 THB
	// True monthly cost = 12000 + 1540 = 13540 THB
	// Monthly hours = 21 * 2 * 22 / 60 = 15.4 hrs
	summary := CalculateMonthlyCost(12000, home, office, route, 22)
	if summary.OneWayMinutes != 21 {
		t.Errorf("expected 21 one-way minutes, got %d", summary.OneWayMinutes)
	}
	if summary.MonthlyRailTHB != 1540 {
		t.Errorf("expected 1540 monthly rail cost, got %d", summary.MonthlyRailTHB)
	}
	if summary.TrueMonthlyCostTHB != 13540 {
		t.Errorf("expected 13540 true monthly cost, got %d", summary.TrueMonthlyCostTHB)
	}
	if summary.MonthlyCommuteHours != 15.4 {
		t.Errorf("expected 15.4 monthly commute hours, got %.1f", summary.MonthlyCommuteHours)
	}
}
