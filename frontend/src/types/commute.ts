export interface OfficePreset {
  id: string;
  name_th: string;
  name_en: string;
  subtitle_th: string;
  lat: number;
  lng: number;
  nearest_hint: string;
}

export interface Station {
  id: string;
  name_th: string;
  name_en: string;
  line_code: string;
  operator: 'BTS' | 'MRT' | 'ARL';
  is_extension: boolean;
  lat: number;
  lng: number;
}

export interface NearestStation {
  station: Station;
  distance_m: number;
}

export interface FareLeg {
  operator: 'BTS' | 'MRT' | 'ARL';
  from_station_id: string;
  to_station_id: string;
  hops: number;
  has_extension: boolean;
  fare_thb: number;
}

export interface RouteResult {
  origin_id: string;
  dest_id: string;
  station_ids: string[];
  rail_minutes: number;
  transfer_count: number;
  transfer_minutes: number;
  total_rail_minutes: number;
  fare_thb: number;
  legs: FareLeg[];
}

export interface AccessLeg {
  distance_m: number;
  mode: 'walk' | 'win';
  minutes: number;
  one_way_cost_thb: number;
}

export interface CommuteCostSummary {
  one_way_minutes: number;
  one_way_fare_thb: number;
  one_way_feeder_thb: number;
  monthly_rail_thb: number;
  monthly_feeder_thb: number;
  monthly_commute_thb: number;
  true_monthly_cost_thb: number;
  monthly_commute_hours: number;
}

export type ZoneColor = 'green' | 'yellow' | 'orange' | 'red';

export interface ReachableStation {
  station: Station;
  zone_color: ZoneColor;
  commute_minutes: number;
  one_way_fare_thb: number;
  transfers: number;
  route: RouteResult;
  office_access: AccessLeg;
}

export interface ListingResult {
  id: string;
  title_th: string;
  title_en: string;
  property_type: 'condo' | 'apartment';
  district: string;
  monthly_rent_thb: number;
  room_size_sqm: number;
  bedrooms: number;
  lat: number;
  lng: number;
  origin_station: Station;
  dest_station: Station;
  home_access: AccessLeg;
  office_access: AccessLeg;
  route: RouteResult;
  cost: CommuteCostSummary;
  zone_color: ZoneColor;
}

export interface OfficePoint {
  lat: number;
  lng: number;
}

export interface SearchFilters {
  maxMinutes: number;
  maxBudget: number;
  workdays: number;
  walkMaxM: number;
}

export interface SearchResponse {
  office: OfficePoint;
  params: {
    max_minutes: number;
    max_budget: number;
    workdays: number;
    walk_max_m: number;
  };
  nearest_office_stations: NearestStation[];
  reachable_stations: ReachableStation[];
  listings: ListingResult[];
}

export interface CompareSummary {
  lowest_true_cost_id: string;
  fastest_commute_id: string;
  max_monthly_savings_thb: number;
  max_monthly_hours_saved: number;
}

export interface CompareResponse {
  office: OfficePoint;
  workdays: number;
  walk_max_m: number;
  nearest_office_stations: NearestStation[];
  items: ListingResult[];
  summary: CompareSummary;
}

export interface TelemetryInfo {
  cacheStatus: 'HIT' | 'MISS' | 'UNKNOWN';
  queryTimeMs: string;
}
