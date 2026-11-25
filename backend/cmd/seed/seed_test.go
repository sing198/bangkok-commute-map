package main

import (
	"path/filepath"
	"testing"
)

func TestCuratedDatasetIsValidAndConnected(t *testing.T) {
	ds, err := loadDataset(filepath.Join("..", "..", "data"))
	if err != nil {
		t.Fatalf("loadDataset failed: %v", err)
	}
	if err := validateDataset(ds); err != nil {
		t.Fatalf("validateDataset failed: %v", err)
	}
	if len(ds.Stations) < 180 {
		t.Fatalf("expected at least 180 stations across all Bangkok lines, got %d", len(ds.Stations))
	}
	if len(ds.Listings) < 1000 {
		t.Fatalf("expected at least 1000 real OSM/curated listings, got %d", len(ds.Listings))
	}
	if len(ds.Access) < 2000 {
		t.Fatalf("expected at least 2000 OSRM walking access rows, got %d", len(ds.Access))
	}
}
