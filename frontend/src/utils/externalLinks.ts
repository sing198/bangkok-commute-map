import type { ListingResult } from '../types/commute';

export interface PriceLinkItem {
  url: string;
  label: string;
  compactLabel: string;
  isDirectBuilding: boolean;
  tone: 'emerald' | 'indigo' | 'slate';
}

/**
 * Direct DDproperty rental listings search URL for a specific building/project name.
 */
export function buildDDpropertySearchUrl(
  listing: Pick<ListingResult, 'title_th' | 'title_en'>
): string {
  const name = (listing.title_en || listing.title_th || '').trim();
  return `https://www.ddproperty.com/${encodeURIComponent('รวมประกาศให้เช่า')}?freetext=${encodeURIComponent(name)}`;
}

/**
 * Direct Google Maps link to inspect building photos, street view, and real tenant reviews.
 */
export function buildGoogleMapsUrl(
  listing: Pick<ListingResult, 'title_th' | 'title_en' | 'lat' | 'lng'>
): string {
  const name = listing.title_th || listing.title_en;
  return `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(
    `${name} ${listing.lat.toFixed(6)},${listing.lng.toFixed(6)}`
  )}`;
}

/**
 * Resolves the best direct real-price links for a building:
 *  1. Tenant-submitted verified source URL (highest priority)
 *  2. Verified direct building page in DB (PropertyHub project, RentHub apartment, or official residence site)
 *  3. Direct rental portal fallback (DDproperty project search + verified PropertyHub/RentHub station page)
 */
export function getRealPriceLinks(listing: ListingResult): {
  primary: PriceLinkItem;
  secondary?: PriceLinkItem;
} {
  // Priority 1: Tenant-verified real price link
  if (listing.latest_source_url) {
    return {
      primary: {
        url: listing.latest_source_url,
        label: 'แหล่งราคาจริง (ผู้เช่า)',
        compactLabel: 'แหล่งราคาจริง',
        isDirectBuilding: true,
        tone: 'emerald',
      },
      secondary: listing.listing_url
        ? {
            url: listing.listing_url,
            label: listing.listing_url_label
              ? `ดูบน ${listing.listing_url_label}`
              : 'หน้าเว็บที่พัก',
            compactLabel: listing.listing_url_label || 'เว็บที่พัก',
            isDirectBuilding: true,
            tone: 'indigo',
          }
        : {
            url: buildDDpropertySearchUrl(listing),
            label: 'DDproperty',
            compactLabel: 'DDproperty',
            isDirectBuilding: false,
            tone: 'indigo',
          },
    };
  }

  // Priority 2: Verified direct building URL stored in DB (GPS & HTTP 200 verified)
  if (listing.listing_url) {
    const provider = listing.listing_url_label || 'เว็บที่พัก';
    const fullLabel =
      provider === 'เว็บที่พัก'
        ? 'ดูราคาที่เว็บที่พัก'
        : `ดูราคาบน ${provider}`;
    return {
      primary: {
        url: listing.listing_url,
        label: fullLabel,
        compactLabel: provider,
        isDirectBuilding: true,
        tone: 'emerald',
      },
      secondary:
        listing.property_type === 'condo'
          ? {
              url: buildDDpropertySearchUrl(listing),
              label: 'DDproperty',
              compactLabel: 'DDproperty',
              isDirectBuilding: false,
              tone: 'indigo',
            }
          : undefined,
    };
  }

  // Priority 3: Direct rental portal links (DDproperty project search + verified station portal)
  if (listing.property_type === 'condo') {
    return {
      primary: {
        url: buildDDpropertySearchUrl(listing),
        label: 'เช็กราคาบน DDproperty',
        compactLabel: 'DDproperty',
        isDirectBuilding: false,
        tone: 'indigo',
      },
      secondary: listing.origin_station.portal_condo_url
        ? {
            url: listing.origin_station.portal_condo_url,
            label: `คอนโดใกล้ ${listing.origin_station.name_th}`,
            compactLabel: 'PropertyHub',
            isDirectBuilding: false,
            tone: 'slate',
          }
        : undefined,
    };
  }

  return {
    primary: {
      url: buildDDpropertySearchUrl(listing),
      label: 'เช็กราคาบน DDproperty',
      compactLabel: 'DDproperty',
      isDirectBuilding: false,
      tone: 'indigo',
    },
    secondary: listing.origin_station.portal_apt_url
      ? {
          url: listing.origin_station.portal_apt_url,
          label: `หอพักใกล้ ${listing.origin_station.name_th} (RentHub)`,
          compactLabel: 'RentHub',
          isDirectBuilding: false,
          tone: 'slate',
        }
      : undefined,
  };
}
