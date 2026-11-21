import { useCallback, useEffect, useState } from 'react';
import { compareListings, fetchPresets, searchCommute } from './api/client';
import type { PlaceResult } from './api/places';
import { CommuteMap } from './components/CommuteMap';
import { CompareModal } from './components/CompareModal';
import { FilterSidebar } from './components/FilterSidebar';
import { ListingDrawer, type SortMode } from './components/ListingDrawer';
import type {
  CompareResponse,
  ListingResult,
  OfficePoint,
  OfficePreset,
  SearchFilters,
  SearchResponse,
  TelemetryInfo,
} from './types/commute';

const DEFAULT_OFFICE: OfficePoint = { lat: 13.7367, lng: 100.5603 }; // Asok

const DEFAULT_FILTERS: SearchFilters = {
  maxMinutes: 35,
  maxBudget: 22000,
  workdays: 22,
  walkMaxM: 800,
};

export default function App() {
  const [presets, setPresets] = useState<OfficePreset[]>([]);
  const [activePresetId, setActivePresetId] = useState<string | null>('asok');
  const [selectedPlaceLabel, setSelectedPlaceLabel] = useState<string | null>(null);
  const [office, setOffice] = useState<OfficePoint>(DEFAULT_OFFICE);
  const [filters, setFilters] = useState<SearchFilters>(DEFAULT_FILTERS);

  const [searchData, setSearchData] = useState<SearchResponse | null>(null);
  const [telemetry, setTelemetry] = useState<TelemetryInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [sortMode, setSortMode] = useState<SortMode>('true_cost');
  const [selectedListingId, setSelectedListingId] = useState<string | null>(null);

  const [compareIds, setCompareIds] = useState<string[]>([]);
  const [compareOpen, setCompareOpen] = useState(false);
  const [compareLoading, setCompareLoading] = useState(false);
  const [compareError, setCompareError] = useState<string | null>(null);
  const [compareData, setCompareData] = useState<CompareResponse | null>(null);

  // Load office presets on mount
  useEffect(() => {
    fetchPresets()
      .then((list) => setPresets(list))
      .catch(() => {
        // Fallback if API is starting up
      });
  }, []);

  // Run commute search whenever office or filters change (debounced slightly for smooth sliders)
  useEffect(() => {
    const timer = window.setTimeout(() => {
      let cancelled = false;
      setLoading(true);
      setError(null);

      searchCommute(office, filters)
        .then(({ data, telemetry: meta }) => {
          if (cancelled) return;
          setSearchData(data);
          setTelemetry(meta);
          setSelectedListingId((prev) =>
            prev && data.listings.some((l) => l.id === prev) ? prev : null
          );
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          setError(err instanceof Error ? err.message : 'ไม่สามารถเชื่อมต่อ API ได้');
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });

      return () => {
        cancelled = true;
      };
    }, 180);

    return () => window.clearTimeout(timer);
  }, [office, filters]);

  const handleSelectPreset = useCallback((preset: OfficePreset) => {
    setActivePresetId(preset.id);
    setSelectedPlaceLabel(null);
    setOffice({ lat: preset.lat, lng: preset.lng });
  }, []);

  const handleSelectPlace = useCallback((place: PlaceResult) => {
    setActivePresetId(null);
    setSelectedPlaceLabel(place.name);
    setOffice({ lat: place.lat, lng: place.lng });
  }, []);

  const handleMoveOffice = useCallback((next: OfficePoint) => {
    setActivePresetId(null);
    setSelectedPlaceLabel(null);
    setOffice(next);
  }, []);

  const handleSelectListing = useCallback((listing: ListingResult) => {
    setSelectedListingId((prev) => (prev === listing.id ? null : listing.id));
  }, []);

  const handleToggleCompare = useCallback((id: string) => {
    setCompareIds((prev) => {
      if (prev.includes(id)) return prev.filter((item) => item !== id);
      if (prev.length >= 3) return prev;
      return [...prev, id];
    });
  }, []);

  const handleOpenCompare = useCallback(async () => {
    if (compareIds.length < 2) return;
    setCompareOpen(true);
    setCompareLoading(true);
    setCompareError(null);
    try {
      const resp = await compareListings(
        office,
        filters.workdays,
        filters.walkMaxM,
        compareIds
      );
      setCompareData(resp);
    } catch (err: unknown) {
      setCompareError(err instanceof Error ? err.message : 'เปรียบเทียบไม่สำเร็จ');
    } finally {
      setCompareLoading(false);
    }
  }, [compareIds, office, filters.workdays, filters.walkMaxM]);

  return (
    <div className="flex h-screen w-screen flex-col overflow-hidden bg-slate-50 lg:flex-row">
      {/* Left Sidebar: Presets & Filters */}
      <FilterSidebar
        presets={presets}
        activePresetId={activePresetId}
        onSelectPreset={handleSelectPreset}
        selectedPlaceLabel={selectedPlaceLabel}
        onSelectPlace={handleSelectPlace}
        filters={filters}
        onChangeFilters={setFilters}
        nearestStations={searchData?.nearest_office_stations ?? []}
        reachableCount={searchData?.reachable_stations.length ?? 0}
        listingCount={searchData?.listings.length ?? 0}
        telemetry={telemetry}
        loading={loading}
      />

      {/* Center: Interactive Leaflet Map */}
      <main className="relative h-[45vh] min-h-[300px] flex-1 lg:h-full">
        {error && (
          <div className="absolute left-4 right-4 top-4 z-[500] rounded-xl border border-rose-200 bg-rose-50 px-4 py-2.5 text-xs font-medium text-rose-800 shadow-md">
            เชื่อมต่อ Backend ไม่สำเร็จ: {error} (ตรวจสอบว่ารัน <code className="font-mono">API_PORT=8081 make api</code> อยู่)
          </div>
        )}
        <CommuteMap
          office={office}
          onMoveOffice={handleMoveOffice}
          reachableStations={searchData?.reachable_stations ?? []}
          listings={searchData?.listings ?? []}
          selectedListingId={selectedListingId}
          onSelectListing={handleSelectListing}
          compareIds={compareIds}
        />
      </main>

      {/* Right Drawer: Listings & Compare Trigger */}
      <ListingDrawer
        listings={searchData?.listings ?? []}
        sortMode={sortMode}
        onChangeSortMode={setSortMode}
        selectedListingId={selectedListingId}
        onSelectListing={handleSelectListing}
        compareIds={compareIds}
        onToggleCompare={handleToggleCompare}
        onClearCompare={() => setCompareIds([])}
        onOpenCompare={handleOpenCompare}
        loading={loading}
      />

      {/* Compare 2-3 Locations Modal */}
      <CompareModal
        open={compareOpen}
        loading={compareLoading}
        error={compareError}
        data={compareData}
        onClose={() => setCompareOpen(false)}
      />
    </div>
  );
}
