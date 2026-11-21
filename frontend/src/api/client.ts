import type {
  CompareResponse,
  OfficePoint,
  OfficePreset,
  SearchFilters,
  SearchResponse,
  TelemetryInfo,
} from '../types/commute';

const API_BASE = import.meta.env.VITE_API_BASE_URL || '';

export async function fetchPresets(): Promise<OfficePreset[]> {
  const res = await fetch(`${API_BASE}/api/presets`);
  if (!res.ok) {
    throw new Error(`Failed to load presets (${res.status})`);
  }
  const data = (await res.json()) as { presets: OfficePreset[] };
  return data.presets;
}

export async function searchCommute(
  office: OfficePoint,
  filters: SearchFilters
): Promise<{ data: SearchResponse; telemetry: TelemetryInfo }> {
  const qs = new URLSearchParams({
    lat: office.lat.toFixed(5),
    lng: office.lng.toFixed(5),
    max_minutes: String(filters.maxMinutes),
    max_budget: String(filters.maxBudget),
    workdays: String(filters.workdays),
    walk_max_m: String(filters.walkMaxM),
  });

  const res = await fetch(`${API_BASE}/api/commute/search?${qs.toString()}`);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `Search failed (${res.status})`);
  }

  const rawCache = res.headers.get('X-Cache');
  const cacheStatus: TelemetryInfo['cacheStatus'] =
    rawCache === 'HIT' || rawCache === 'MISS' ? rawCache : 'UNKNOWN';
  const queryTimeMs = res.headers.get('X-Query-Time-Ms') || '—';

  const data = (await res.json()) as SearchResponse;
  return {
    data,
    telemetry: { cacheStatus, queryTimeMs },
  };
}

export async function compareListings(
  office: OfficePoint,
  workdays: number,
  walkMaxM: number,
  listingIds: string[]
): Promise<CompareResponse> {
  const res = await fetch(`${API_BASE}/api/commute/compare`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      office,
      workdays,
      walk_max_m: walkMaxM,
      listing_ids: listingIds,
    }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `Compare failed (${res.status})`);
  }
  return (await res.json()) as CompareResponse;
}
