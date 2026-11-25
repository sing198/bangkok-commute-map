package commute

import (
	"encoding/json"
	"io/fs"

	"github.com/tk/bangkok-commute-map/backend/data"
)

// StationRentBenchmark represents open-data monthly rental statistics around a rail station
// (e.g. Inside Airbnb Bangkok CC0 snapshot, prioritizing minimum_nights >= 28 residential stays).
type StationRentBenchmark struct {
	StationID        string `json:"station_id"`
	SampleCount      int    `json:"sample_count"`
	P25MonthlyTHB    int    `json:"p25_monthly_thb"`
	MedianMonthlyTHB int    `json:"median_monthly_thb"`
	P75MonthlyTHB    int    `json:"p75_monthly_thb"`
	Basis            string `json:"basis"` // "monthly_28plus" or "entire_home_30x"
	SnapshotDate     string `json:"snapshot_date"`
	Source           string `json:"source"`
	License          string `json:"license,omitempty"`
}

type stationBenchmarkFile struct {
	Meta struct {
		Source       string `json:"source"`
		License      string `json:"license"`
		SnapshotDate string `json:"snapshot_date"`
	} `json:"meta"`
	Stations []StationRentBenchmark `json:"stations"`
}

// LoadStationRentBenchmarks loads embedded open-data station rent benchmarks indexed by station ID.
func LoadStationRentBenchmarks() map[string]StationRentBenchmark {
	out := make(map[string]StationRentBenchmark)
	raw, err := fs.ReadFile(data.FS, "station_rent_benchmarks.json")
	if err != nil {
		return out
	}
	var parsed stationBenchmarkFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return out
	}
	for _, st := range parsed.Stations {
		if st.StationID == "" || st.MedianMonthlyTHB <= 0 {
			continue
		}
		if st.Source == "" {
			st.Source = parsed.Meta.Source
		}
		if st.License == "" {
			st.License = parsed.Meta.License
		}
		if st.SnapshotDate == "" {
			st.SnapshotDate = parsed.Meta.SnapshotDate
		}
		out[st.StationID] = st
	}
	return out
}
