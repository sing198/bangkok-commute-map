import {
  ArrowDownUp,
  Bike,
  CheckSquare,
  Clock,
  Footprints,
  Scale,
  Square,
  TrainFront,
  X,
} from 'lucide-react';
import type { ListingResult, ZoneColor } from '../types/commute';

export type SortMode = 'true_cost' | 'fastest';

interface ListingDrawerProps {
  listings: ListingResult[];
  sortMode: SortMode;
  onChangeSortMode: (mode: SortMode) => void;
  selectedListingId: string | null;
  onSelectListing: (listing: ListingResult) => void;
  compareIds: string[];
  onToggleCompare: (id: string) => void;
  onClearCompare: () => void;
  onOpenCompare: () => void;
  loading: boolean;
}

const ZONE_BADGE: Record<ZoneColor, string> = {
  green: 'bg-emerald-100 text-emerald-800 border-emerald-200',
  yellow: 'bg-amber-100 text-amber-800 border-amber-200',
  orange: 'bg-orange-100 text-orange-800 border-orange-200',
  red: 'bg-rose-100 text-rose-800 border-rose-200',
};

const OPERATOR_PILL: Record<string, string> = {
  BTS: 'bg-lime-100 text-lime-800',
  MRT: 'bg-sky-100 text-sky-800',
  ARL: 'bg-rose-100 text-rose-800',
};

export function ListingDrawer({
  listings,
  sortMode,
  onChangeSortMode,
  selectedListingId,
  onSelectListing,
  compareIds,
  onToggleCompare,
  onClearCompare,
  onOpenCompare,
  loading,
}: ListingDrawerProps) {
  const sorted = [...listings].sort((a, b) => {
    if (sortMode === 'fastest') {
      if (a.cost.one_way_minutes !== b.cost.one_way_minutes) {
        return a.cost.one_way_minutes - b.cost.one_way_minutes;
      }
      return a.cost.true_monthly_cost_thb - b.cost.true_monthly_cost_thb;
    }
    if (a.cost.true_monthly_cost_thb !== b.cost.true_monthly_cost_thb) {
      return a.cost.true_monthly_cost_thb - b.cost.true_monthly_cost_thb;
    }
    return a.cost.one_way_minutes - b.cost.one_way_minutes;
  });

  return (
    <section className="flex h-full w-full flex-col border-l border-slate-200 bg-white lg:w-[400px] lg:shrink-0">
      {/* Header & Sort Switcher */}
      <div className="border-b border-slate-200 px-4 py-3.5">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-bold text-slate-900">
            ที่พักที่ถึงทันเวลา ({listings.length})
          </h2>
          <span className="flex items-center gap-1 text-[11px] text-slate-500">
            <ArrowDownUp className="h-3 w-3" />
            เรียงตาม
          </span>
        </div>

        <div className="mt-2.5 grid grid-cols-2 gap-1.5 rounded-xl bg-slate-100 p-1">
          <button
            type="button"
            onClick={() => onChangeSortMode('true_cost')}
            className={`rounded-lg px-2.5 py-1.5 text-xs font-semibold transition ${
              sortMode === 'true_cost'
                ? 'bg-white text-indigo-700 shadow-xs'
                : 'text-slate-600 hover:text-slate-900'
            }`}
          >
            ค่าครองชีพรวมจริงต่ำสุด
          </button>
          <button
            type="button"
            onClick={() => onChangeSortMode('fastest')}
            className={`rounded-lg px-2.5 py-1.5 text-xs font-semibold transition ${
              sortMode === 'fastest'
                ? 'bg-white text-indigo-700 shadow-xs'
                : 'text-slate-600 hover:text-slate-900'
            }`}
          >
            เวลาเดินทางเร็วสุด
          </button>
        </div>

        <p className="mt-2 text-[11px] text-slate-500">
          พิกัดอาคารจริง (OpenStreetMap &amp; Curated) · ราคาเช่าประเมินอ้างอิงตลาด
        </p>
      </div>

      {/* Listing Cards */}
      <div className="flex-1 space-y-3 overflow-y-auto bg-slate-50/70 p-3.5">
        {loading && listings.length === 0 ? (
          <div className="py-12 text-center text-sm text-slate-500">
            กำลังค้นหาที่พักด้วย PostGIS...
          </div>
        ) : sorted.length === 0 ? (
          <div className="rounded-xl border border-dashed border-slate-300 bg-white p-8 text-center text-sm text-slate-500">
            ไม่พบที่พักในเงื่อนไขนี้ ลองเพิ่มเวลาเดินทางสูงสุดหรือขยายงบค่าเช่าดูครับ
          </div>
        ) : (
          sorted.map((item) => {
            const isSelected = item.id === selectedListingId;
            const isCompared = compareIds.includes(item.id);
            const canAddMore = compareIds.length < 3 || isCompared;

            return (
              <article
                key={item.id}
                onClick={() => onSelectListing(item)}
                className={`cursor-pointer rounded-2xl border bg-white p-3.5 transition ${
                  isSelected
                    ? 'border-indigo-500 ring-2 ring-indigo-500/20 shadow-sm'
                    : 'border-slate-200 hover:border-slate-300 hover:shadow-xs'
                }`}
              >
                {/* Top row: Title & Commute Time Badge */}
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5">
                      <span className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-slate-600">
                        {item.property_type === 'condo' ? 'คอนโด' : 'อพาร์ตเมนต์'}
                      </span>
                      <span className="text-[11px] text-slate-400">
                        {item.room_size_sqm} ตร.ม.
                      </span>
                    </div>
                    <h3 className="mt-1 truncate text-sm font-bold text-slate-900">
                      {item.title_th}
                    </h3>
                    {item.title_en !== item.title_th && (
                      <p className="truncate text-[11px] text-slate-500">{item.title_en}</p>
                    )}
                  </div>

                  <span
                    className={`shrink-0 rounded-xl border px-2.5 py-1 text-center text-xs font-bold ${
                      ZONE_BADGE[item.zone_color]
                    }`}
                  >
                    <div className="flex items-center gap-1">
                      <Clock className="h-3 w-3" />
                      {item.cost.one_way_minutes} นาที
                    </div>
                    <div className="text-[10px] font-medium opacity-80">
                      {item.cost.monthly_commute_hours} ชม./ด.
                    </div>
                  </span>
                </div>

                {/* Cost Breakdown Box */}
                <div className="mt-3 flex items-baseline justify-between rounded-xl bg-slate-50 px-3 py-2">
                  <div>
                    <div className="text-[11px] font-medium text-slate-500">
                      ค่าครองชีพรวมจริง / เดือน
                    </div>
                    <div className="text-lg font-extrabold tracking-tight text-indigo-700">
                      ฿{item.cost.true_monthly_cost_thb.toLocaleString()}
                    </div>
                  </div>
                  <div className="text-right text-[11px] text-slate-600">
                    <div>
                      ค่าเช่า <strong>฿{item.monthly_rent_thb.toLocaleString()}</strong>
                    </div>
                    <div className="text-amber-700">
                      + เดินทาง <strong>฿{item.cost.monthly_commute_thb.toLocaleString()}</strong>
                    </div>
                  </div>
                </div>

                {/* Transit Route & Access Mode */}
                <div className="mt-2.5 flex flex-wrap items-center gap-1.5 text-[11px]">
                  <span
                    className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 font-semibold ${
                      OPERATOR_PILL[item.origin_station.operator] || 'bg-slate-100 text-slate-700'
                    }`}
                  >
                    <TrainFront className="h-3 w-3" />
                    {item.origin_station.operator} {item.origin_station.name_th}
                  </span>

                  <span className="inline-flex items-center gap-1 rounded-md bg-slate-100 px-2 py-0.5 text-slate-600">
                    {item.home_access.mode === 'walk' ? (
                      <Footprints className="h-3 w-3" />
                    ) : (
                      <Bike className="h-3 w-3 text-orange-600" />
                    )}
                    {item.home_access.distance_m} ม. ({item.home_access.minutes} น.
                    {item.home_access.one_way_cost_thb > 0
                      ? ` • วิน ฿${item.home_access.one_way_cost_thb}`
                      : ''}
                    )
                  </span>

                  {item.route.legs && item.route.legs.length > 0 && (
                    <span className="rounded-md bg-slate-100 px-2 py-0.5 text-slate-600">
                      {item.route.legs
                        .map((leg) => `${leg.operator} ฿${leg.fare_thb}`)
                        .join(' + ')}
                    </span>
                  )}

                  {item.office_access.one_way_cost_thb > 0 && (
                    <span className="inline-flex items-center gap-1 rounded-md bg-amber-50 px-2 py-0.5 text-amber-800">
                      <Bike className="h-3 w-3 text-amber-600" />
                      ต่อวินถึงออฟฟิศ ฿{item.office_access.one_way_cost_thb}
                    </span>
                  )}
                </div>

                {/* Compare Checkbox */}
                <div className="mt-3 flex items-center justify-between border-t border-slate-100 pt-2.5">
                  <span className="text-[11px] text-slate-400">
                    {item.route.transfer_count === 0
                      ? 'นั่งต่อเดียวถึงใกล้ที่ทำงาน'
                      : `เปลี่ยนสาย ${item.route.transfer_count} ครั้ง`}
                  </span>
                  <button
                    type="button"
                    disabled={!canAddMore}
                    onClick={(e) => {
                      e.stopPropagation();
                      onToggleCompare(item.id);
                    }}
                    className={`inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-semibold transition ${
                      isCompared
                        ? 'bg-indigo-600 text-white'
                        : canAddMore
                          ? 'bg-slate-100 text-slate-700 hover:bg-indigo-50 hover:text-indigo-700'
                          : 'cursor-not-allowed bg-slate-100 text-slate-400'
                    }`}
                  >
                    {isCompared ? (
                      <CheckSquare className="h-3.5 w-3.5" />
                    ) : (
                      <Square className="h-3.5 w-3.5" />
                    )}
                    {isCompared ? 'เลือกเทียบแล้ว' : 'เทียบทำเล'}
                  </button>
                </div>
              </article>
            );
          })
        )}
      </div>

      {/* Sticky Compare Bar */}
      {compareIds.length > 0 && (
        <div className="border-t border-indigo-200 bg-indigo-50 px-4 py-3">
          <div className="flex items-center justify-between gap-2">
            <div className="text-xs font-semibold text-indigo-900">
              เลือกแล้ว {compareIds.length}/3 แห่ง
              {compareIds.length < 2 && (
                <span className="ml-1 font-normal text-indigo-700">(เลือกอีกอย่างน้อย 1 แห่ง)</span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={onClearCompare}
                className="rounded-lg p-1.5 text-indigo-600 hover:bg-indigo-100"
                title="ล้างรายการที่เลือก"
              >
                <X className="h-4 w-4" />
              </button>
              <button
                type="button"
                disabled={compareIds.length < 2}
                onClick={onOpenCompare}
                className={`inline-flex items-center gap-1.5 rounded-xl px-3.5 py-2 text-xs font-bold shadow-xs transition ${
                  compareIds.length >= 2
                    ? 'bg-indigo-600 text-white hover:bg-indigo-700'
                    : 'cursor-not-allowed bg-indigo-200 text-indigo-400'
                }`}
              >
                <Scale className="h-3.5 w-3.5" />
                เปรียบเทียบเลย
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
