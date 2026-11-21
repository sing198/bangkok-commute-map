export interface PlaceResult {
  id: string;
  name: string;
  subtitle: string;
  lat: number;
  lng: number;
  source: 'photon' | 'nominatim';
}

// Greater Bangkok bounding box matching backend validation (13.40-14.15, 100.25-100.95)
const BKK_MIN_LAT = 13.42;
const BKK_MAX_LAT = 14.12;
const BKK_MIN_LNG = 100.27;
const BKK_MAX_LNG = 100.93;

function isInBangkok(lat: number, lng: number): boolean {
  return (
    Number.isFinite(lat) &&
    Number.isFinite(lng) &&
    lat >= BKK_MIN_LAT &&
    lat <= BKK_MAX_LAT &&
    lng >= BKK_MIN_LNG &&
    lng <= BKK_MAX_LNG
  );
}

interface PhotonFeature {
  geometry?: { coordinates?: [number, number] };
  properties?: {
    osm_id?: number;
    osm_type?: string;
    name?: string;
    street?: string;
    district?: string;
    city?: string;
    county?: string;
    state?: string;
  };
}

interface NominatimItem {
  place_id: number;
  display_name: string;
  name?: string;
  lat: string;
  lon: string;
}

async function queryPhoton(query: string, signal?: AbortSignal): Promise<PlaceResult[]> {
  const params = new URLSearchParams({
    q: query,
    lat: '13.7463',
    lon: '100.5349',
    bbox: `${BKK_MIN_LNG},${BKK_MIN_LAT},${BKK_MAX_LNG},${BKK_MAX_LAT}`,
    limit: '6',
  });

  const res = await fetch(`https://photon.komoot.io/api/?${params.toString()}`, { signal });
  if (!res.ok) return [];
  const data = (await res.json()) as { features?: PhotonFeature[] };

  const results: PlaceResult[] = [];
  for (const f of data.features ?? []) {
    const coords = f.geometry?.coordinates;
    const props = f.properties;
    if (!coords || coords.length < 2 || !props) continue;

    const [lng, lat] = coords;
    if (!isInBangkok(lat, lng)) continue;

    const name = props.name || props.street || '';
    if (!name) continue;

    const subParts = [props.street !== name ? props.street : '', props.district, props.city || props.county || props.state]
      .filter((v): v is string => Boolean(v && v.trim()))
      .filter((v, idx, arr) => arr.indexOf(v) === idx);

    results.push({
      id: `photon-${props.osm_type ?? 'x'}-${props.osm_id ?? results.length}-${lat.toFixed(4)}`,
      name,
      subtitle: subParts.join(', ') || 'กรุงเทพมหานคร',
      lat,
      lng,
      source: 'photon',
    });
  }
  return results;
}

async function queryNominatim(query: string, signal?: AbortSignal): Promise<PlaceResult[]> {
  const params = new URLSearchParams({
    q: query,
    format: 'jsonv2',
    countrycodes: 'th',
    viewbox: `${BKK_MIN_LNG},${BKK_MAX_LAT},${BKK_MAX_LNG},${BKK_MIN_LAT}`,
    bounded: '1',
    limit: '6',
    'accept-language': 'th,en',
  });

  const res = await fetch(`https://nominatim.openstreetmap.org/search?${params.toString()}`, {
    signal,
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) return [];
  const items = (await res.json()) as NominatimItem[];

  const results: PlaceResult[] = [];
  for (const item of items) {
    const lat = Number(item.lat);
    const lng = Number(item.lon);
    if (!isInBangkok(lat, lng)) continue;

    const parts = item.display_name.split(',').map((s) => s.trim());
    const name = item.name || parts[0] || item.display_name;
    const subtitle = parts.slice(1, 4).join(', ') || 'กรุงเทพมหานคร';

    results.push({
      id: `nom-${item.place_id}`,
      name,
      subtitle,
      lat,
      lng,
      source: 'nominatim',
    });
  }
  return results;
}

/**
 * Searches Bangkok landmarks, office towers, malls, and districts using free OpenStreetMap APIs:
 * Photon (fast autocomplete) with automatic Nominatim fallback for Thai queries.
 */
export async function searchBangkokPlaces(
  rawQuery: string,
  signal?: AbortSignal
): Promise<PlaceResult[]> {
  const query = rawQuery.trim();
  if (query.length < 2) return [];

  // For Thai script queries, Nominatim often has stronger Thai alias coverage;
  // for Latin/mixed queries, Photon is ultra-fast for autocomplete.
  const hasThai = /[\u0E00-\u0E7F]/.test(query);

  try {
    if (hasThai) {
      const nom = await queryNominatim(query, signal);
      if (nom.length > 0) return nom;
      return await queryPhoton(query, signal);
    }

    const photon = await queryPhoton(query, signal);
    if (photon.length >= 2) return photon;

    const nom = await queryNominatim(query, signal);
    if (nom.length === 0) return photon;

    // Merge unique results favoring Photon first
    const merged = [...photon];
    for (const item of nom) {
      const dup = merged.some(
        (m) => Math.abs(m.lat - item.lat) < 0.0008 && Math.abs(m.lng - item.lng) < 0.0008
      );
      if (!dup) merged.push(item);
    }
    return merged.slice(0, 6);
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw err;
    }
    // Fallback attempt if primary provider errored
    try {
      return hasThai ? await queryPhoton(query, signal) : await queryNominatim(query, signal);
    } catch {
      return [];
    }
  }
}
