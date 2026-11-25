import type { ListingResult } from '../types/commute';

/**
 * Generates a 1-click Google Search URL to check real up-to-date monthly room prices
 * (official residence website, Renthub, DDproperty, LivingInsider) without fabricating estimates.
 */
export function buildRealPriceSearchUrl(listing: Pick<ListingResult, 'title_th' | 'title_en'>): string {
  const name = listing.title_th || listing.title_en;
  const query = `"${name}" ค่าเช่า ราคา ต่อเดือน`;
  return `https://www.google.com/search?q=${encodeURIComponent(query)}`;
}

/**
 * Generates a direct Google Maps link to inspect building photos, street view, and real tenant reviews.
 */
export function buildGoogleMapsUrl(
  listing: Pick<ListingResult, 'title_th' | 'title_en' | 'lat' | 'lng'>
): string {
  const name = listing.title_th || listing.title_en;
  return `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(
    `${name} ${listing.lat.toFixed(6)},${listing.lng.toFixed(6)}`
  )}`;
}
