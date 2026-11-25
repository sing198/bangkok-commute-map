export type OperatorCode =
  | 'BTS'
  | 'BTS_GOLD'
  | 'MRT'
  | 'MRT_YL'
  | 'MRT_PK'
  | 'ARL'
  | 'SRT_RED';

export const OPERATOR_LABEL: Record<OperatorCode, string> = {
  BTS: 'BTS',
  BTS_GOLD: 'BTS สีทอง',
  MRT: 'MRT',
  MRT_YL: 'MRT เหลือง',
  MRT_PK: 'MRT ชมพู',
  ARL: 'ARL',
  SRT_RED: 'SRT แดง',
};

export const OPERATOR_PILL_STYLE: Record<OperatorCode, string> = {
  BTS: 'bg-lime-100 text-lime-800',
  BTS_GOLD: 'bg-amber-100 text-amber-900',
  MRT: 'bg-sky-100 text-sky-800',
  MRT_YL: 'bg-yellow-100 text-yellow-900',
  MRT_PK: 'bg-pink-100 text-pink-800',
  ARL: 'bg-rose-100 text-rose-800',
  SRT_RED: 'bg-red-100 text-red-800',
};

export type SortMode = 'time' | 'commute_cost' | 'true_cost';

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
  operator: OperatorCode;
  is_extension: boolean;
  lat: number;
  lng: number;
  portal_condo_url?: string;
  portal_apt_url?: string;
}

export interface NearestStation {
  station: Station;
  distance_m: number;
}

export interface FareLeg {
  operator: OperatorCode;
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
  distance_source?: 'osrm_foot' | 'estimated_detour' | 'straight_line';
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
  osm_id?: string;
  title_th: string;
  title_en: string;
  property_type: 'condo' | 'apartment';
  district: string;
  monthly_rent_thb: number; // >0 ONLY when price_status === 'verified'
  price_status: 'verified' | 'unverified';
  data_source?: string;
  listing_url?: string;
  listing_url_label?: string;
  verified_rent_median_thb?: number;
  verified_report_count: number;
  latest_report_month?: string;
  latest_source_url?: string;
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
  maxCommuteBudget: number; // 0 = unlimited; factual monthly rail+feeder budget in THB
  verifiedOnly: boolean;
  workdays: number;
  walkMaxM: number;
}

export interface SearchResponse {
  office: OfficePoint;
  params: {
    max_minutes: number;
    max_budget: number;
    max_commute_budget: number;
    verified_only: boolean;
    workdays: number;
    walk_max_m: number;
    sort: SortMode;
    limit: number;
  };
  detour_factor: number;
  total_listings_count: number;
  returned_listings_count: number;
  nearest_office_stations: NearestStation[];
  reachable_stations: ReachableStation[];
  listings: ListingResult[];
}

export interface CompareSummary {
  lowest_true_cost_id: string;
  lowest_commute_cost_id: string;
  fastest_commute_id: string;
  max_monthly_savings_thb: number;
  max_monthly_commute_savings_thb: number;
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

export type RoomType = 'studio' | '1br' | '2br' | 'dorm';

export interface PriceReportRecord {
  id: number;
  listing_id: string;
  monthly_rent_thb: number;
  room_size_sqm: number;
  room_type: RoomType;
  reported_month: string;
  note?: string;
  source_url?: string;
  is_outlier: boolean;
  created_at: string;
}

export interface VerifiedPriceStats {
  median_rent_thb: number;
  min_rent_thb: number;
  max_rent_thb: number;
  report_count: number;
  latest_report_month: string;
  latest_source_url?: string;
}

export interface ListPriceReportsResponse {
  listing_id: string;
  price_status: 'verified' | 'unverified';
  stats: VerifiedPriceStats;
  reports: PriceReportRecord[];
}

export interface SubmitPriceReportRequest {
  monthly_rent_thb: number;
  room_size_sqm: number;
  room_type: RoomType;
  reported_month: string;
  note?: string;
  source_url?: string;
}

export interface SubmitPriceReportResponse {
  report: PriceReportRecord;
  stats: VerifiedPriceStats;
  price_status: 'verified' | 'unverified';
  message: string;
}
