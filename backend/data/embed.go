package data

import "embed"

// FS embeds the validated Greater Bangkok rail network, official fare tables,
// real OSM residential buildings, and precomputed OSRM walking distances.
//
//go:embed stations.json edges.json fare_rules.json listings.json listing_station_access.json
var FS embed.FS
