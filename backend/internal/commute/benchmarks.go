package commute

import (
	"encoding/json"
	"io/fs"

	"github.com/tk/bangkok-commute-map/backend/data"
)

// BuildingPortalPrice holds real monthly rental stats fetched directly from a building's
// RentHub (apartment/mansion) or PropertyHub (condo) listing page.
type BuildingPortalPrice struct {
	MinRentTHB    int    `json:"min_rent_thb"`
	MedianRentTHB int    `json:"median_rent_thb"`
	MaxRentTHB    int    `json:"max_rent_thb"`
	SampleCount   int    `json:"sample_count"`
	Source        string `json:"source"` // "RentHub" or "PropertyHub"
	SourceURL     string `json:"source_url,omitempty"`
	MatchType     string `json:"match_type,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

// StationRentBenchmark represents real Thai monthly rental statistics around a rail station,
// strictly separated by property_type ("condo" from PropertyHub vs "apartment" from RentHub).
type StationRentBenchmark struct {
	StationID        string `json:"station_id,omitempty"`
	PropertyType     string `json:"property_type,omitempty"` // "condo" or "apartment"
	SampleCount      int    `json:"sample_count"`
	P25MonthlyTHB    int    `json:"p25_monthly_thb"`
	MedianMonthlyTHB int    `json:"median_monthly_thb"`
	P75MonthlyTHB    int    `json:"p75_monthly_thb"`
	SnapshotDate     string `json:"snapshot_date,omitempty"`
	Source           string `json:"source"` // "PropertyHub" or "RentHub"
	SourceURL        string `json:"source_url,omitempty"`
}

// StationPortalPrices holds both condo and apartment station-level ranges for map tooltips.
type StationPortalPrices struct {
	Condo     *StationRentBenchmark `json:"condo,omitempty"`
	Apartment *StationRentBenchmark `json:"apartment,omitempty"`
}

// PortalPriceStore holds exact building-level prices and per-station condo/apartment benchmarks.
type PortalPriceStore struct {
	Buildings map[string]BuildingPortalPrice
	Stations  map[string]StationPortalPrices
}

type portalPricesFile struct {
	Meta struct {
		UpdatedDate string `json:"updated_date"`
	} `json:"meta"`
	Buildings map[string]BuildingPortalPrice `json:"buildings"`
	Stations  map[string]struct {
		Condo     *stationPortalRaw `json:"condo,omitempty"`
		Apartment *stationPortalRaw `json:"apartment,omitempty"`
	} `json:"stations"`
}

type stationPortalRaw struct {
	P25MonthlyTHB    int    `json:"p25_monthly_thb"`
	MedianMonthlyTHB int    `json:"median_monthly_thb"`
	P75MonthlyTHB    int    `json:"p75_monthly_thb"`
	SampleCount      int    `json:"sample_count"`
	Source           string `json:"source"`
	SourceURL        string `json:"source_url,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

// LoadPortalPrices loads embedded real Thai rental prices from portal_prices.json.
func LoadPortalPrices() PortalPriceStore {
	store := PortalPriceStore{
		Buildings: make(map[string]BuildingPortalPrice),
		Stations:  make(map[string]StationPortalPrices),
	}
	raw, err := fs.ReadFile(data.FS, "portal_prices.json")
	if err != nil {
		return store
	}
	var parsed portalPricesFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return store
	}
	for id, bp := range parsed.Buildings {
		if bp.MedianRentTHB > 0 {
			if bp.UpdatedAt == "" {
				bp.UpdatedAt = parsed.Meta.UpdatedDate
			}
			store.Buildings[id] = bp
		}
	}
	for sid, sp := range parsed.Stations {
		var entry StationPortalPrices
		if sp.Condo != nil && sp.Condo.MedianMonthlyTHB > 0 {
			snap := sp.Condo.UpdatedAt
			if snap == "" {
				snap = parsed.Meta.UpdatedDate
			}
			entry.Condo = &StationRentBenchmark{
				StationID:        sid,
				PropertyType:     "condo",
				SampleCount:      sp.Condo.SampleCount,
				P25MonthlyTHB:    sp.Condo.P25MonthlyTHB,
				MedianMonthlyTHB: sp.Condo.MedianMonthlyTHB,
				P75MonthlyTHB:    sp.Condo.P75MonthlyTHB,
				SnapshotDate:     snap,
				Source:           sp.Condo.Source,
				SourceURL:        sp.Condo.SourceURL,
			}
		}
		if sp.Apartment != nil && sp.Apartment.MedianMonthlyTHB > 0 {
			snap := sp.Apartment.UpdatedAt
			if snap == "" {
				snap = parsed.Meta.UpdatedDate
			}
			entry.Apartment = &StationRentBenchmark{
				StationID:        sid,
				PropertyType:     "apartment",
				SampleCount:      sp.Apartment.SampleCount,
				P25MonthlyTHB:    sp.Apartment.P25MonthlyTHB,
				MedianMonthlyTHB: sp.Apartment.MedianMonthlyTHB,
				P75MonthlyTHB:    sp.Apartment.P75MonthlyTHB,
				SnapshotDate:     snap,
				Source:           sp.Apartment.Source,
				SourceURL:        sp.Apartment.SourceURL,
			}
		}
		if entry.Condo != nil || entry.Apartment != nil {
			store.Stations[sid] = entry
		}
	}
	return store
}
