import { Building2, Clock, Footprints, Gauge, MapPin, Wallet, Zap } from 'lucide-react';
import type { PlaceResult } from '../api/places';
import type {
  NearestStation,
  OfficePreset,
  SearchFilters,
  TelemetryInfo,
} from '../types/commute';
import { OfficeSearchBox } from './OfficeSearchBox';

interface FilterSidebarProps {
  presets: OfficePreset[];
  activePresetId: string | null;
  onSelectPreset: (preset: OfficePreset) => void;
  selectedPlaceLabel: string | null;
  onSelectPlace: (place: PlaceResult) => void;
  filters: SearchFilters;
  onChangeFilters: (next: SearchFilters) => void;
  nearestStations: NearestStation[];
  reachableCount: number;
  listingCount: number;
  telemetry: TelemetryInfo | null;
  loading: boolean;
}

const WORKDAY_OPTIONS = [
  { label: 'Hybrid 12 วัน', value: 12 },
  { label: '16 วัน', value: 16 },
  { label: 'เต็มเดือน 22 วัน', value: 22 },
];

export function FilterSidebar({
  presets,
  activePresetId,
  onSelectPreset,
  selectedPlaceLabel,
  onSelectPlace,
  filters,
  onChangeFilters,
  nearestStations,
  reachableCount,
  listingCount,
  telemetry,
  loading,
}: FilterSidebarProps) {
  return (
    <aside className="flex h-full w-full flex-col border-r border-slate-200 bg-white lg:w-[340px] lg:shrink-0">
      {/* Brand Header */}
      <div className="border-b border-slate-200 px-5 py-4">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2.5">
            <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-sm">
              <MapPin className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-base font-bold tracking-tight text-slate-900">
                Bangkok Commute Map
              </h1>
              <p className="text-xs text-slate-500">
                ค่าเช่าจริง = ค่าห้อง + ค่าเดินทาง BTS/MRT/ARL
              </p>
            </div>
          </div>
        </div>

        {/* Live Telemetry Badge (PostGIS + Redis Proof) */}
        {telemetry && (
          <div className="mt-3 flex items-center justify-between rounded-lg border border-slate-200 bg-slate-50 px-3 py-1.5 text-[11px]">
            <span className="flex items-center gap-1.5 font-medium text-slate-600">
              <Zap className="h-3.5 w-3.5 text-amber-500" />
              Redis Cache
            </span>
            <div className="flex items-center gap-2">
              <span
                className={`rounded px-1.5 py-0.5 font-mono font-semibold ${
                  telemetry.cacheStatus === 'HIT'
                    ? 'bg-emerald-100 text-emerald-700'
                    : 'bg-amber-100 text-amber-700'
                }`}
              >
                {telemetry.cacheStatus}
              </span>
              <span className="flex items-center gap-0.5 font-mono text-slate-500">
                <Gauge className="h-3 w-3" />
                {telemetry.queryTimeMs} ms
              </span>
            </div>
          </div>
        )}
      </div>

      {/* Scrollable Controls */}
      <div className="flex-1 space-y-5 overflow-y-auto px-5 py-4">
        {/* Free OSM Place Search */}
        <section>
          <OfficeSearchBox
            selectedLabel={selectedPlaceLabel}
            onSelectPlace={onSelectPlace}
          />
        </section>

        {/* Office Presets */}
        <section>
          <div className="mb-2 flex items-center justify-between">
            <h2 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-slate-500">
              <Building2 className="h-3.5 w-3.5" />
              ย่านออฟฟิศยอดฮิต
            </h2>
            <span className="text-[11px] text-indigo-600">หรือคลิก/ลากหมุดบนแผนที่</span>
          </div>
          <div className="grid grid-cols-2 gap-2">
            {presets.map((p) => {
              const active = activePresetId === p.id;
              return (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => onSelectPreset(p)}
                  className={`rounded-xl border px-3 py-2 text-left transition ${
                    active
                      ? 'border-indigo-600 bg-indigo-50/80 text-indigo-900 shadow-xs ring-1 ring-indigo-600'
                      : 'border-slate-200 bg-white text-slate-700 hover:border-slate-300 hover:bg-slate-50'
                  }`}
                >
                  <div className="text-sm font-semibold leading-tight">{p.name_th}</div>
                  <div className="mt-0.5 truncate text-[11px] text-slate-500">{p.name_en}</div>
                </button>
              );
            })}
          </div>

          {nearestStations.length > 0 && (
            <div className="mt-2.5 rounded-lg bg-slate-50 px-3 py-2 text-xs text-slate-600">
              <span className="font-medium text-slate-700">สถานีใกล้ออฟฟิศสุด: </span>
              {nearestStations[0].station.operator} {nearestStations[0].station.name_th} (
              {Math.round(nearestStations[0].distance_m)} ม.)
            </div>
          )}
        </section>

        {/* Max Commute Time */}
        <section>
          <div className="mb-1.5 flex items-center justify-between text-sm">
            <label className="flex items-center gap-1.5 font-medium text-slate-700">
              <Clock className="h-4 w-4 text-indigo-600" />
              เวลาเดินทางสูงสุด
            </label>
            <span className="font-semibold text-indigo-700">{filters.maxMinutes} นาที/เที่ยว</span>
          </div>
          <input
            type="range"
            min={15}
            max={60}
            step={5}
            value={filters.maxMinutes}
            onChange={(e) =>
              onChangeFilters({ ...filters, maxMinutes: Number(e.target.value) })
            }
            className="h-2 w-full cursor-pointer appearance-none rounded-lg bg-slate-200 accent-indigo-600"
          />
          <div className="mt-1 flex justify-between text-[11px] text-slate-400">
            <span>15 นาที</span>
            <span>35 นาที</span>
            <span>60 นาที</span>
          </div>
        </section>

        {/* Max Monthly Rent Budget */}
        <section>
          <div className="mb-1.5 flex items-center justify-between text-sm">
            <label className="flex items-center gap-1.5 font-medium text-slate-700">
              <Wallet className="h-4 w-4 text-emerald-600" />
              งบค่าเช่าสูงสุด
            </label>
            <span className="font-semibold text-emerald-700">
              {filters.maxBudget === 0
                ? 'ไม่จำกัด'
                : `฿${filters.maxBudget.toLocaleString()}/เดือน`}
            </span>
          </div>
          <input
            type="range"
            min={0}
            max={35000}
            step={1000}
            value={filters.maxBudget}
            onChange={(e) =>
              onChangeFilters({ ...filters, maxBudget: Number(e.target.value) })
            }
            className="h-2 w-full cursor-pointer appearance-none rounded-lg bg-slate-200 accent-emerald-600"
          />
          <div className="mt-1 flex justify-between text-[11px] text-slate-400">
            <span>ไม่จำกัด</span>
            <span>฿18,000</span>
            <span>฿35,000</span>
          </div>
        </section>

        {/* Workdays per month */}
        <section>
          <div className="mb-2 flex items-center justify-between text-sm">
            <span className="font-medium text-slate-700">วันเข้าออฟฟิศ / เดือน</span>
            <span className="text-xs text-slate-500">{filters.workdays} วัน</span>
          </div>
          <div className="grid grid-cols-3 gap-1.5">
            {WORKDAY_OPTIONS.map((opt) => {
              const active = filters.workdays === opt.value;
              return (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => onChangeFilters({ ...filters, workdays: opt.value })}
                  className={`rounded-lg border px-2 py-1.5 text-xs font-medium transition ${
                    active
                      ? 'border-indigo-600 bg-indigo-600 text-white'
                      : 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50'
                  }`}
                >
                  {opt.label}
                </button>
              );
            })}
          </div>
        </section>

        {/* Max Walking Distance */}
        <section>
          <div className="mb-1.5 flex items-center justify-between text-sm">
            <label className="flex items-center gap-1.5 font-medium text-slate-700">
              <Footprints className="h-4 w-4 text-slate-500" />
              ระยะเดินถึงสถานี
            </label>
            <span className="font-semibold text-slate-700">{filters.walkMaxM} ม.</span>
          </div>
          <input
            type="range"
            min={400}
            max={1200}
            step={100}
            value={filters.walkMaxM}
            onChange={(e) =>
              onChangeFilters({ ...filters, walkMaxM: Number(e.target.value) })
            }
            className="h-2 w-full cursor-pointer appearance-none rounded-lg bg-slate-200 accent-slate-700"
          />
          <p className="mt-1 text-[11px] text-slate-400">
            เกิน {filters.walkMaxM} ม. ระบบคำนวณสลับเป็นวินมอเตอร์ไซค์อัตโนมัติ
          </p>
        </section>

        {/* Color-Coded Zone Legend */}
        <section className="rounded-xl border border-slate-200 bg-slate-50/70 p-3">
          <div className="mb-2 text-xs font-semibold text-slate-700">สีโซนเวลาเดินทางจริง</div>
          <div className="space-y-1.5 text-xs text-slate-600">
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-emerald-500" />
              <span>เขียว: เร็วมาก (≤ 20 นาที)</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-amber-400" />
              <span>เหลือง: กำลังดี (21–35 นาที)</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-orange-500" />
              <span>ส้ม: ไกลหน่อย (36–45 นาที)</span>
            </div>
          </div>
        </section>
      </div>

      {/* Footer Stats */}
      <div className="border-t border-slate-200 bg-slate-50 px-5 py-3 text-xs text-slate-600">
        {loading ? (
          <span>กำลังคำนวณเส้นทาง PostGIS + Dijkstra...</span>
        ) : (
          <div className="flex items-center justify-between">
            <span>สถานีถึงทันเวลา: <strong className="text-slate-900">{reachableCount}</strong></span>
            <span>ที่พักที่ตรงเงื่อนไข: <strong className="text-indigo-700">{listingCount}</strong></span>
          </div>
        )}
      </div>
    </aside>
  );
}
