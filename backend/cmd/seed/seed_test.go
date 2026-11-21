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
	if len(ds.Listings) < 150 {
		t.Fatalf("expected at least 150 real/curated listings, got %d", len(ds.Listings))
	}
}
