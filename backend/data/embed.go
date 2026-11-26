package data

import "embed"

// FS embeds the validated Greater Bangkok rail network, official fare tables,
// real OSM residential buildings, precomputed OSRM walking distances, and
// real Thai portal rental prices (RentHub + PropertyHub).
//
//go:embed stations.json edges.json fare_rules.json listings.json listing_station_access.json portal_prices.json
var FS embed.FS
