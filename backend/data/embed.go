package data

import "embed"

// FS embeds the validated Greater Bangkok rail network, official fare tables,
// real OSM residential buildings, precomputed OSRM walking distances, and
// open-data station rental benchmarks (Inside Airbnb CC0).
//
//go:embed stations.json edges.json fare_rules.json listings.json listing_station_access.json station_rent_benchmarks.json
var FS embed.FS
