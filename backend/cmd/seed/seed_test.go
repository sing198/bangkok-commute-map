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
	if len(ds.Stations) < 70 {
		t.Fatalf("expected at least 70 stations, got %d", len(ds.Stations))
	}

	listings := generateListings(ds.Stations)
	if len(listings) < 150 {
		t.Fatalf("expected at least 150 generated listings, got %d", len(listings))
	}

	// Verify determinism across two runs.
	listings2 := generateListings(ds.Stations)
	if len(listings) != len(listings2) || listings[0] != listings2[0] || listings[len(listings)-1] != listings2[len(listings)-1] {
		t.Fatalf("listing generator must be deterministic")
	}
}
