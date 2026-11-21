import { useEffect, useRef, useState } from 'react';
import { Loader2, MapPin, Search, X } from 'lucide-react';
import { searchBangkokPlaces, type PlaceResult } from '../api/places';

interface OfficeSearchBoxProps {
  selectedLabel: string | null;
  onSelectPlace: (place: PlaceResult) => void;
}

export function OfficeSearchBox({ selectedLabel, onSelectPlace }: OfficeSearchBoxProps) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<PlaceResult[]>([]);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [searchedEmpty, setSearchedEmpty] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  // Close dropdown on click outside
  useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, []);

  // Debounced OSM place search with AbortController
  useEffect(() => {
    const trimmed = query.trim();
    if (trimmed.length < 2) {
      setResults([]);
      setSearchedEmpty(false);
      setLoading(false);
      return;
    }

    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setLoading(true);
      setSearchedEmpty(false);
      searchBangkokPlaces(trimmed, controller.signal)
        .then((items) => {
          setResults(items);
          setActiveIndex(items.length > 0 ? 0 : -1);
          setSearchedEmpty(items.length === 0);
          setOpen(true);
        })
        .catch((err) => {
          if (err instanceof DOMException && err.name === 'AbortError') return;
          setResults([]);
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false);
        });
    }, 320);

    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [query]);

  const handlePick = (place: PlaceResult) => {
    setQuery(place.name);
    setOpen(false);
    setActiveIndex(-1);
    onSelectPlace(place);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!open || results.length === 0) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActiveIndex((prev) => (prev + 1) % results.length);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActiveIndex((prev) => (prev <= 0 ? results.length - 1 : prev - 1));
    } else if (e.key === 'Enter' && activeIndex >= 0) {
      e.preventDefault();
      handlePick(results[activeIndex]);
    } else if (e.key === 'Escape') {
      setOpen(false);
    }
  };

  return (
    <div ref={containerRef} className="relative">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
        <input
          type="text"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            if (!open) setOpen(true);
          }}
          onFocus={() => {
            if (results.length > 0 || searchedEmpty) setOpen(true);
          }}
          onKeyDown={handleKeyDown}
          placeholder="ค้นหาตึกออฟฟิศ / ห้าง / ย่าน (เช่น G Tower, เซ็นทรัลเวิลด์)"
          aria-label="ค้นหาสถานที่ตั้งออฟฟิศในกรุงเทพฯ"
          className="w-full rounded-xl border border-slate-200 bg-slate-50/70 py-2 pl-9 pr-8 text-xs text-slate-900 placeholder:text-slate-400 transition focus:border-indigo-500 focus:bg-white focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
        />
        {loading ? (
          <Loader2 className="absolute right-2.5 top-1/2 h-4 w-4 -translate-y-1/2 animate-spin text-indigo-500" />
        ) : query.length > 0 ? (
          <button
            type="button"
            onClick={() => {
              setQuery('');
              setResults([]);
              setSearchedEmpty(false);
              setOpen(false);
            }}
            aria-label="ล้างคำค้นหา"
            className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-0.5 text-slate-400 hover:bg-slate-200/70 hover:text-slate-700"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        ) : null}
      </div>

      {selectedLabel && (
        <div className="mt-1.5 flex items-center gap-1 truncate text-[11px] text-indigo-700">
          <MapPin className="h-3 w-3 shrink-0" />
          <span className="truncate font-medium">หมุดปัจจุบัน: {selectedLabel}</span>
        </div>
      )}

      {open && (results.length > 0 || searchedEmpty) && (
        <div className="absolute left-0 right-0 top-full z-[600] mt-1.5 max-h-60 overflow-y-auto rounded-xl border border-slate-200 bg-white py-1 shadow-xl">
          {results.length > 0 ? (
            results.map((item, idx) => (
              <button
                key={item.id}
                type="button"
                onClick={() => handlePick(item)}
                onMouseEnter={() => setActiveIndex(idx)}
                className={`flex w-full items-start gap-2.5 px-3 py-2 text-left transition ${
                  idx === activeIndex ? 'bg-indigo-50 text-indigo-950' : 'text-slate-700 hover:bg-slate-50'
                }`}
              >
                <MapPin className="mt-0.5 h-3.5 w-3.5 shrink-0 text-indigo-500" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-xs font-semibold">{item.name}</div>
                  <div className="truncate text-[11px] text-slate-500">{item.subtitle}</div>
                </div>
              </button>
            ))
          ) : (
            <div className="px-3 py-2.5 text-xs text-slate-500">
              ไม่พบสถานที่ในเขตกรุงเทพฯ ลองพิมพ์ชื่อภาษาอังกฤษหรือชื่อย่านใกล้เคียงดูครับ
            </div>
          )}
          <div className="border-t border-slate-100 px-3 py-1 text-right text-[10px] text-slate-400">
            Search by OpenStreetMap
          </div>
        </div>
      )}
    </div>
  );
}
