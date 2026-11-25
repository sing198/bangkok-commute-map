package transit

// Station represents a Bangkok rail transit station node in the graph.
type Station struct {
	ID          string  `json:"id"`
	NameTH      string  `json:"name_th"`
	NameEN      string  `json:"name_en"`
	LineCode    string  `json:"line_code"`
	Operator    string  `json:"operator"`
	IsExtension bool    `json:"is_extension"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

// Edge represents a directed rail or interchange transfer link between two stations.
type Edge struct {
	From          string `json:"from"`
	To            string `json:"to"`
	TravelMinutes int    `json:"travel_minutes"`
	IsTransfer    bool   `json:"is_transfer"`
}

// FareRule defines the fare in THB for a given operator and hop count.
type FareRule struct {
	Operator string `json:"operator"`
	Hops     int    `json:"hops"`
	FareTHB  int    `json:"fare_thb"`
}

// FareLeg breaks down the fare for a single contiguous operator segment.
type FareLeg struct {
	Operator      string `json:"operator"`
	FromStationID string `json:"from_station_id"`
	ToStationID   string `json:"to_station_id"`
	Hops          int    `json:"hops"`
	HasExtension  bool   `json:"has_extension"`
	FareTHB       int    `json:"fare_thb"`
}

// RouteResult holds the shortest rail route, timing breakdown, and fare calculation.
type RouteResult struct {
	OriginID         string    `json:"origin_id"`
	DestID           string    `json:"dest_id"`
	StationIDs       []string  `json:"station_ids"`
	RailMinutes      int       `json:"rail_minutes"`
	TransferCount    int       `json:"transfer_count"`
	TransferMinutes  int       `json:"transfer_minutes"`
	TotalRailMinutes int       `json:"total_rail_minutes"`
	FareTHB          int       `json:"fare_thb"`
	Legs             []FareLeg `json:"legs"`
}

// AccessLeg models the first-mile or last-mile trip between a location and a station.
type AccessLeg struct {
	DistanceM      int    `json:"distance_m"`
	Mode           string `json:"mode"` // "walk" or "win" (motorcycle taxi)
	Minutes        int    `json:"minutes"`
	OneWayCostTHB  int    `json:"one_way_cost_thb"`
	DistanceSource string `json:"distance_source,omitempty"` // "osrm_foot" | "estimated_detour" | "straight_line"
}

// CommuteCostSummary holds monthly cost and time totals for a listing.
type CommuteCostSummary struct {
	OneWayMinutes       int     `json:"one_way_minutes"`
	OneWayFareTHB       int     `json:"one_way_fare_thb"`
	OneWayFeederTHB     int     `json:"one_way_feeder_thb"`
	MonthlyRailTHB      int     `json:"monthly_rail_thb"`
	MonthlyFeederTHB    int     `json:"monthly_feeder_thb"`
	MonthlyCommuteTHB   int     `json:"monthly_commute_thb"`
	TrueMonthlyCostTHB  int     `json:"true_monthly_cost_thb"`
	MonthlyCommuteHours float64 `json:"monthly_commute_hours"`
}
