package transit

import (
	"container/heap"
	"fmt"
	"math"
)

const (
	// DefaultWaitMinutes is the average train waiting time when taking rail transit.
	DefaultWaitMinutes = 3
	// SameOperatorLineTransferPenalty applies when switching lines at a shared station (e.g. BTS Siam).
	SameOperatorLineTransferPenalty = 3
	// BTSExtensionSurchargeTHB is added when crossing between BTS core and extension stations.
	BTSExtensionSurchargeTHB = 15
	// BTSExtensionFlatFareTHB is the flat fare for rides entirely within BTS extension stations.
	BTSExtensionFlatFareTHB = 15
	// BTSMaxFareTHB is the maximum single-trip BTS Green Line fare cap.
	BTSMaxFareTHB = 62

	// WalkSpeedMetersPerMin is the standard walking speed (80 m/min).
	WalkSpeedMetersPerMin = 80.0
	// WinSpeedMetersPerMin is the motorcycle taxi feeder speed (300 m/min).
	WinSpeedMetersPerMin = 300.0
	// BangkokDetourFactor is the empirical median ratio of OSRM real street walking distance
	// to straight-line distance measured across 3,800+ Bangkok building-to-station routes.
	BangkokDetourFactor = 1.40
)

// Graph is an in-memory Bangkok rail network supporting Dijkstra routing and Thai fare rules.
type Graph struct {
	stations  map[string]Station
	adj       map[string][]Edge
	fareRules map[string]map[int]int
	maxHops   map[string]int
}

// NewGraph builds an in-memory transit graph from stations, directed edges, and fare rules.
func NewGraph(stations []Station, edges []Edge, rules []FareRule) *Graph {
	g := &Graph{
		stations:  make(map[string]Station, len(stations)),
		adj:       make(map[string][]Edge, len(stations)),
		fareRules: make(map[string]map[int]int),
		maxHops:   make(map[string]int),
	}

	for _, s := range stations {
		g.stations[s.ID] = s
	}
	for _, e := range edges {
		g.adj[e.From] = append(g.adj[e.From], e)
	}
	for _, r := range rules {
		if g.fareRules[r.Operator] == nil {
			g.fareRules[r.Operator] = make(map[int]int)
		}
		g.fareRules[r.Operator][r.Hops] = r.FareTHB
		if r.Hops > g.maxHops[r.Operator] {
			g.maxHops[r.Operator] = r.Hops
		}
	}

	return g
}

// Station returns a station by ID.
func (g *Graph) Station(id string) (Station, bool) {
	s, ok := g.stations[id]
	return s, ok
}

// AllStations returns all stations in the graph.
func (g *Graph) AllStations() []Station {
	out := make([]Station, 0, len(g.stations))
	for _, s := range g.stations {
		out = append(out, s)
	}
	return out
}

type stateKey struct {
	stationID string
	lineCode  string
}

type pqItem struct {
	key  stateKey
	dist int
}

type priorityQueue []pqItem

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].dist < pq[j].dist }
func (pq priorityQueue) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i] }
func (pq *priorityQueue) Push(x any)        { *pq = append(*pq, x.(pqItem)) }
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

// ShortestPath computes the fastest rail route from originID to destID using Dijkstra,
// accounting for explicit interchange edges and same-station line switches (e.g. BTS Siam).
func (g *Graph) ShortestPath(originID, destID string) (RouteResult, error) {
	if _, ok := g.stations[originID]; !ok {
		return RouteResult{}, fmt.Errorf("unknown origin station: %s", originID)
	}
	if _, ok := g.stations[destID]; !ok {
		return RouteResult{}, fmt.Errorf("unknown destination station: %s", destID)
	}
	if originID == destID {
		return RouteResult{
			OriginID:   originID,
			DestID:     destID,
			StationIDs: []string{originID},
		}, nil
	}

	dist, prev := g.runDijkstra(originID, -1)

	var (
		bestKey  stateKey
		bestDist = math.MaxInt
	)
	for k, d := range dist {
		if k.stationID == destID && d < bestDist {
			bestDist = d
			bestKey = k
		}
	}
	if bestDist == math.MaxInt {
		return RouteResult{}, fmt.Errorf("no route from %s to %s", originID, destID)
	}

	path := reconstructPath(bestKey, prev)
	return g.buildRouteResult(path, bestDist), nil
}

// ReachableToDest computes the shortest route from every station in the graph to destID
// whose total rail time (including transfers) is <= maxRailMinutes.
func (g *Graph) ReachableToDest(destID string, maxRailMinutes int) (map[string]RouteResult, error) {
	if _, ok := g.stations[destID]; !ok {
		return nil, fmt.Errorf("unknown destination station: %s", destID)
	}

	// Since the Bangkok rail graph is bidirectional and symmetric, we run Dijkstra
	// outward from destID and reverse each reconstructed path to get origin -> destID.
	dist, prev := g.runDijkstra(destID, maxRailMinutes)

	bestByStation := make(map[string]stateKey, len(g.stations))
	bestDistByStation := make(map[string]int, len(g.stations))
	for k, d := range dist {
		if maxRailMinutes >= 0 && d > maxRailMinutes {
			continue
		}
		if prevD, exists := bestDistByStation[k.stationID]; !exists || d < prevD {
			bestDistByStation[k.stationID] = d
			bestByStation[k.stationID] = k
		}
	}

	results := make(map[string]RouteResult, len(bestByStation))
	for stationID, key := range bestByStation {
		if stationID == destID {
			results[stationID] = RouteResult{
				OriginID:   destID,
				DestID:     destID,
				StationIDs: []string{destID},
			}
			continue
		}
		revPath := reconstructPath(key, prev)
		// Reverse path so it goes stationID -> destID
		for i, j := 0, len(revPath)-1; i < j; i, j = i+1, j-1 {
			revPath[i], revPath[j] = revPath[j], revPath[i]
		}
		results[stationID] = g.buildRouteResult(revPath, bestDistByStation[stationID])
	}

	return results, nil
}

func (g *Graph) runDijkstra(startID string, maxMinutes int) (map[stateKey]int, map[stateKey]stateKey) {
	startKey := stateKey{stationID: startID, lineCode: ""}
	dist := map[stateKey]int{startKey: 0}
	prev := make(map[stateKey]stateKey)

	pq := &priorityQueue{{key: startKey, dist: 0}}
	heap.Init(pq)

	for pq.Len() > 0 {
		curr := heap.Pop(pq).(pqItem)
		if curr.dist > dist[curr.key] {
			continue
		}
		if maxMinutes >= 0 && curr.dist > maxMinutes {
			break
		}

		fromStation := g.stations[curr.key.stationID]
		for _, edge := range g.adj[curr.key.stationID] {
			toStation := g.stations[edge.To]
			edgeLine := inferEdgeLine(fromStation, toStation, edge)

			cost := edge.TravelMinutes
			// Add same-station line transfer penalty (e.g. BTS_SUK <-> BTS_SIL at Siam BTS_CEN).
			if !edge.IsTransfer && curr.key.lineCode != "" && edgeLine != "" && curr.key.lineCode != edgeLine {
				cost += SameOperatorLineTransferPenalty
			}

			nextKey := stateKey{stationID: edge.To, lineCode: edgeLine}
			nextDist := curr.dist + cost
			if maxMinutes >= 0 && nextDist > maxMinutes {
				continue
			}

			if oldDist, exists := dist[nextKey]; !exists || nextDist < oldDist {
				dist[nextKey] = nextDist
				prev[nextKey] = curr.key
				heap.Push(pq, pqItem{key: nextKey, dist: nextDist})
			}
		}
	}

	return dist, prev
}

// inferEdgeLine determines which transit line a rail edge belongs to.
// At shared multi-line hubs (Siam BTS_CEN, Tao Poon MRT_BL10, Muang Thong Thani MRT_PK10,
// Krung Thep Aphiwat SRT_RN01), the neighbor's line code identifies which line the rider is on.
func inferEdgeLine(from, to Station, edge Edge) string {
	if edge.IsTransfer {
		return ""
	}
	isSharedHub := func(id string) bool {
		return id == "BTS_CEN" || id == "MRT_BL10" || id == "MRT_PK10" || id == "SRT_RN01"
	}
	if isSharedHub(from.ID) {
		return to.LineCode
	}
	if isSharedHub(to.ID) {
		return from.LineCode
	}
	return to.LineCode
}

func reconstructPath(endKey stateKey, prev map[stateKey]stateKey) []string {
	var path []string
	curr := endKey
	for {
		path = append(path, curr.stationID)
		p, ok := prev[curr]
		if !ok {
			break
		}
		curr = p
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func (g *Graph) buildRouteResult(path []string, totalMinutes int) RouteResult {
	transferCount := 0
	transferMinutes := 0
	railMinutes := 0
	prevLine := ""

	for i := 0; i < len(path)-1; i++ {
		fromID, toID := path[i], path[i+1]
		edge, ok := g.findEdge(fromID, toID)
		if !ok {
			continue
		}
		fromSt := g.stations[fromID]
		toSt := g.stations[toID]
		edgeLine := inferEdgeLine(fromSt, toSt, edge)

		if edge.IsTransfer {
			transferCount++
			if fromSt.Operator == toSt.Operator {
				// e.g., MRT Blue <-> Purple at Tao Poon: 3 min transfer + remaining ride time
				transferMinutes += SameOperatorLineTransferPenalty
				if edge.TravelMinutes > SameOperatorLineTransferPenalty {
					railMinutes += edge.TravelMinutes - SameOperatorLineTransferPenalty
				}
			} else {
				transferMinutes += edge.TravelMinutes
			}
			prevLine = ""
		} else {
			if prevLine != "" && edgeLine != "" && prevLine != edgeLine {
				transferCount++
				transferMinutes += SameOperatorLineTransferPenalty
			}
			railMinutes += edge.TravelMinutes
			prevLine = edgeLine
		}
	}

	fare, legs := g.CalculateFare(path)

	return RouteResult{
		OriginID:         path[0],
		DestID:           path[len(path)-1],
		StationIDs:       path,
		RailMinutes:      railMinutes,
		TransferCount:    transferCount,
		TransferMinutes:  transferMinutes,
		TotalRailMinutes: totalMinutes,
		FareTHB:          fare,
		Legs:             legs,
	}
}

func (g *Graph) findEdge(fromID, toID string) (Edge, bool) {
	for _, e := range g.adj[fromID] {
		if e.To == toID {
			return e, true
		}
	}
	return Edge{}, false
}

// CalculateFare computes total one-way fare across operators, charging separate tickets
// whenever the rider transfers across different transit operators (BTS / MRT / ARL).
func (g *Graph) CalculateFare(path []string) (int, []FareLeg) {
	if len(path) < 2 {
		return 0, nil
	}

	// Group consecutive stations by operator.
	var segments [][]Station
	for _, id := range path {
		st := g.stations[id]
		if len(segments) == 0 || segments[len(segments)-1][0].Operator != st.Operator {
			segments = append(segments, []Station{st})
		} else {
			segments[len(segments)-1] = append(segments[len(segments)-1], st)
		}
	}

	totalFare := 0
	legs := make([]FareLeg, 0, len(segments))

	for _, seg := range segments {
		// A single station in an operator segment means the rider only walked through
		// an interchange transfer without actually riding that operator's train.
		if len(seg) < 2 {
			continue
		}

		op := seg[0].Operator
		hops := len(seg) - 1
		fare, hasExt := g.operatorSegmentFare(op, seg)

		totalFare += fare
		legs = append(legs, FareLeg{
			Operator:      op,
			FromStationID: seg[0].ID,
			ToStationID:   seg[len(seg)-1].ID,
			Hops:          hops,
			HasExtension:  hasExt,
			FareTHB:       fare,
		})
	}

	return totalFare, legs
}

func (g *Graph) operatorSegmentFare(operator string, seg []Station) (int, bool) {
	hops := len(seg) - 1
	hasExtension := false
	allExtension := true
	coreHops := 0

	for i, st := range seg {
		if st.IsExtension {
			hasExtension = true
		} else {
			allExtension = false
		}
		if i > 0 && !seg[i-1].IsExtension && !st.IsExtension {
			coreHops++
		}
	}

	if operator == "BTS" {
		if allExtension {
			return BTSExtensionFlatFareTHB, true
		}
		if hasExtension {
			// Core hops fare + 15 THB extension surcharge, capped at 62 THB.
			base := g.lookupFare(operator, coreHops)
			total := base + BTSExtensionSurchargeTHB
			if total > BTSMaxFareTHB {
				total = BTSMaxFareTHB
			}
			return total, true
		}
	}

	return g.lookupFare(operator, hops), hasExtension
}

func (g *Graph) lookupFare(operator string, hops int) int {
	rules, ok := g.fareRules[operator]
	if !ok {
		return 0
	}
	maxH := g.maxHops[operator]
	if hops > maxH {
		hops = maxH
	}
	if fare, ok := rules[hops]; ok {
		return fare
	}
	return 0
}

// CalculateAccess computes walking or motorcycle taxi (win) time and cost
// based on distance in meters and the user's max comfortable walking distance.
func CalculateAccess(distanceM float64, walkMaxM int) AccessLeg {
	return CalculateAccessWithSource(distanceM, walkMaxM, "osrm_foot")
}

// CalculateOfficeAccess estimates real street walking distance from an arbitrary office pin
// by scaling straight-line PostGIS distance by Bangkok's empirical street detour factor (1.40x).
func CalculateOfficeAccess(straightLineM float64, walkMaxM int) AccessLeg {
	if straightLineM <= 0 {
		return AccessLeg{DistanceM: 0, Mode: "walk", Minutes: 0, OneWayCostTHB: 0, DistanceSource: "estimated_detour"}
	}
	estimatedStreetM := straightLineM * BangkokDetourFactor
	return CalculateAccessWithSource(estimatedStreetM, walkMaxM, "estimated_detour")
}

// CalculateAccessWithSource computes walking or motorcycle taxi (win) time and cost with provenance label.
func CalculateAccessWithSource(distanceM float64, walkMaxM int, source string) AccessLeg {
	if walkMaxM <= 0 {
		walkMaxM = 800
	}
	distInt := int(math.Round(distanceM))
	if distInt <= 0 {
		return AccessLeg{DistanceM: 0, Mode: "walk", Minutes: 0, OneWayCostTHB: 0, DistanceSource: source}
	}

	if distInt <= walkMaxM {
		mins := int(math.Ceil(distanceM / WalkSpeedMetersPerMin))
		if mins < 1 {
			mins = 1
		}
		return AccessLeg{
			DistanceM:      distInt,
			Mode:           "walk",
			Minutes:        mins,
			OneWayCostTHB:  0,
			DistanceSource: source,
		}
	}

	// Switch to motorcycle taxi (วินมอเตอร์ไซค์) when beyond walkMaxM.
	mins := int(math.Ceil(distanceM / WinSpeedMetersPerMin))
	if mins < 2 {
		mins = 2
	}
	cost := 15
	if distInt > 2000 {
		cost = 25
	} else if distInt > 1200 {
		cost = 20
	}

	return AccessLeg{
		DistanceM:      distInt,
		Mode:           "win",
		Minutes:        mins,
		OneWayCostTHB:  cost,
		DistanceSource: source,
	}
}

// CalculateMonthlyCost computes True Monthly Cost and monthly commute hours.
func CalculateMonthlyCost(monthlyRentTHB int, homeAccess, officeAccess AccessLeg, route RouteResult, workdays int) CommuteCostSummary {
	if workdays <= 0 {
		workdays = 22
	}
	waitMinutes := 0
	if len(route.StationIDs) > 1 {
		waitMinutes = DefaultWaitMinutes
	}

	oneWayMinutes := homeAccess.Minutes + waitMinutes + route.TotalRailMinutes + officeAccess.Minutes
	oneWayFeeder := homeAccess.OneWayCostTHB + officeAccess.OneWayCostTHB
	monthlyRail := route.FareTHB * 2 * workdays
	monthlyFeeder := oneWayFeeder * 2 * workdays
	monthlyCommute := monthlyRail + monthlyFeeder
	trueMonthlyCost := monthlyRentTHB + monthlyCommute
	monthlyHours := math.Round((float64(oneWayMinutes*2*workdays)/60.0)*10) / 10

	return CommuteCostSummary{
		OneWayMinutes:       oneWayMinutes,
		OneWayFareTHB:       route.FareTHB,
		OneWayFeederTHB:     oneWayFeeder,
		MonthlyRailTHB:      monthlyRail,
		MonthlyFeederTHB:    monthlyFeeder,
		MonthlyCommuteTHB:   monthlyCommute,
		TrueMonthlyCostTHB:  trueMonthlyCost,
		MonthlyCommuteHours: monthlyHours,
	}
}
