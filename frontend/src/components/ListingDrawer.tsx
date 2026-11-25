import { useEffect, useMemo, useState } from 'react';
import {
  ArrowUpDown,
  Bike,
  CheckSquare,
  Clock,
  ExternalLink,
  Footprints,
  MapPin,
  MessageSquarePlus,
  Scale,
  Search,
  ShieldCheck,
  Square,
  TrainFront,
  X,
} from 'lucide-react';
import {
  OPERATOR_LABEL,
  OPERATOR_PILL_STYLE,
  type ListingResult,
  type SortMode,
  type ZoneColor,
} from '../types/commute';
import { buildGoogleMapsUrl, buildRealPriceSearchUrl } from '../utils/externalLinks';

interface ListingDrawerProps {
  listings: ListingResult[];
  totalListingsCount: number;
  sortMode: SortMode;
  onChangeSortMode: (mode: SortMode) => void;
  selectedListingId: string | null;
  onSelectListing: (listing: ListingResult) => void;
  compareIds: string[];
  onToggleCompare: (id: string) => void;
  onClearCompare: () => void;
  onOpenCompare: () => void;
  onOpenReportPrice: (listing: ListingResult) => void;
  loading: boolean;
}

const ZONE_BADGE: Record<ZoneColor, string> = {
  green: 'bg-emerald-50 text-emerald-700 border-emerald-200',
  yellow: 'bg-amber-50 text-amber-700 border-amber-200',
  orange: 'bg-orange-50 text-orange-700 border-orange-200',
  red: 'bg-rose-50 text-rose-700 border-rose-200',
};

const PAGE_SIZE = 40;

export function ListingDrawer({
  listings,
  totalListingsCount,
  sortMode,
  onChangeSortMode,
  selectedListingId,
  onSelectListing,
  compareIds,
  onToggleCompare,
  onClearCompare,
  onOpenCompare,
  onOpenReportPrice,
  loading,
}: ListingDrawerProps) {
  const [filterText, setFilterText] = useState('');
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE);

  useEffect(() => {
    setVisibleCount(PAGE_SIZE);
  }, [listings, sortMode, filterText]);

  const filtered = useMemo(() => {
    const q = filterText.trim().toLowerCase();
    const base = q
      ? listings.filter(
          (l) =>
            l.title_th.toLowerCase().includes(q) ||
            l.title_en.toLowerCase().includes(q) ||
            l.origin_station.name_th.toLowerCase().includes(q) ||
            l.origin_station.name_en.toLowerCase().includes(q) ||
            l.district.toLowerCase().includes(q)
        )
      : listings;

    return [...base].sort((a, b) => {
      if (sortMode === 'commute_cost') {
        if (a.cost.monthly_commute_thb !== b.cost.monthly_commute_thb) {
          return a.cost.monthly_commute_thb - b.cost.monthly_commute_thb;
        }
        return a.cost.one_way_minutes - b.cost.one_way_minutes;
      }
      if (a.cost.one_way_minutes !== b.cost.one_way_minutes) {
        return a.cost.one_way_minutes - b.cost.one_way_minutes;
      }
      return a.cost.monthly_commute_thb - b.cost.monthly_commute_thb;
    });
  }, [listings, sortMode, filterText]);

  const visibleItems = filtered.slice(0, visibleCount);
  const displayTotal = filterText.trim()
    ? filtered.length
    : Math.max(totalListingsCount, listings.length);

  return (
    <aside className="flex h-full w-full flex-col border-l border-slate-200 bg-slate-50/70 lg:w-[410px] lg:shrink-0">
      {/* Header & Sort */}
      <div className="border-b border-slate-200 bg-white px-4 py-3">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-bold text-slate-900">
            ที่พักที่ถึงทันเวลา ({displayTotal.toLocaleString()})
          </h2>
          <div className="flex items-center gap-1 text-xs text-slate-500">
            <ArrowUpDown className="h-3.5 w-3.5" />
            <span>เรียงตาม</span>
          </div>
        </div>

        <div className="mt-2 grid grid-cols-2 gap-1.5 rounded-xl bg-slate-100 p-1">
          <button
            type="button"
            onClick={() => onChangeSortMode('time')}
            className={`rounded-lg py-1.5 text-xs font-semibold transition ${
              sortMode === 'time'
                ? 'bg-white text-indigo-700 shadow-xs'
                : 'text-slate-600 hover:text-slate-900'
            }`}
          >
            เวลาเดินทางเร็วสุด
          </button>
          <button
            type="button"
            onClick={() => onChangeSortMode('commute_cost')}
            className={`rounded-lg py-1.5 text-xs font-semibold transition ${
              sortMode === 'commute_cost'
                ? 'bg-white text-indigo-700 shadow-xs'
                : 'text-slate-600 hover:text-slate-900'
            }`}
          >
            ค่าเดินทาง/เดือนต่ำสุด
          </button>
        </div>

        {/* Quick Search Filter inside Drawer */}
        <div className="relative mt-2">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-slate-400" />
          <input
            type="text"
            value={filterText}
            onChange={(e) => setFilterText(e.target.value)}
            placeholder="กรองชื่อคอนโด / หอพัก / สถานี..."
            className="w-full rounded-lg border border-slate-200 bg-slate-50 py-1.5 pl-8 pr-7 text-xs text-slate-800 placeholder:text-slate-400 focus:border-indigo-400 focus:bg-white focus:outline-none"
          />
          {filterText && (
            <button
              type="button"
              onClick={() => setFilterText('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>

        <p className="mt-1.5 text-[10px] leading-snug text-slate-400">
          อาคารจริงจาก OSM • ระยะเดินถนนจริง (OSRM) • ไม่เดาราคาห้อง กดลิงก์เช็กราคาจริงได้ทุกแห่ง
        </p>
      </div>

      {/* Scrollable Listing Cards */}
      <div className="flex-1 space-y-2.5 overflow-y-auto p-3">
        {loading && listings.length === 0 ? (
          <div className="space-y-3">
            {[1, 2, 3, 4].map((n) => (
              <div
                key={n}
                className="h-36 animate-pulse rounded-2xl border border-slate-200 bg-white p-4"
              />
            ))}
          </div>
        ) : visibleItems.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-slate-300 bg-white p-6 text-center">
            <p className="text-sm font-semibold text-slate-700">
              ไม่พบที่พักที่ตรงเงื่อนไขนี้
            </p>
            <p className="mt-1 text-xs text-slate-500">
              ลองเพิ่มเวลาเดินทางสูงสุด ปรับงบค่าเดินทาง หรือปิดตัวกรองเฉพาะราคาจริงดูนะครับ
            </p>
          </div>
        ) : (
          <>
            {visibleItems.map((item) => {
              const isSelected = item.id === selectedListingId;
              const isCompared = compareIds.includes(item.id);
              const canAddMore = compareIds.length < 3 || isCompared;
              const isVerified = item.price_status === 'verified';
              const isDirect = item.route.station_ids.length <= 1;
              const opLabel =
                OPERATOR_LABEL[item.origin_station.operator] ||
                item.origin_station.operator;
              const opStyle =
                OPERATOR_PILL_STYLE[item.origin_station.operator] ||
                'bg-slate-100 text-slate-700';

              return (
                <article
                  key={item.id}
                  onClick={() => onSelectListing(item)}
                  className={`cursor-pointer rounded-2xl border bg-white p-3.5 transition ${
                    isSelected
                      ? 'border-indigo-500 shadow-sm ring-2 ring-indigo-500/20'
                      : 'border-slate-200 hover:border-slate-300 hover:shadow-xs'
                  }`}
                >
                  {/* Top row: Title & Commute Time Badge */}
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-slate-600">
                          {item.property_type === 'condo' ? 'คอนโด' : 'อพาร์ตเมนต์'}
                        </span>
                        {isVerified ? (
                          <span className="inline-flex items-center gap-0.5 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[10px] font-bold text-emerald-800">
                            <ShieldCheck className="h-3 w-3" />
                            ราคาจริงจากผู้เช่า ({item.verified_report_count})
                          </span>
                        ) : (
                          <span className="rounded-full bg-slate-100 px-1.5 py-0.5 text-[10px] font-medium text-slate-500">
                            เช็กราคาห้องจากลิงก์ด้านล่าง
                          </span>
                        )}
                      </div>
                      <h3 className="mt-1 truncate text-sm font-bold text-slate-900">
                        {item.title_th}
                      </h3>
                      {item.title_en !== item.title_th && (
                        <p className="truncate text-[11px] text-slate-500">
                          {item.title_en}
                        </p>
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

                  {/* Factual Cost Box (Zero Guessed Rent!) */}
                  <div className="mt-2.5 flex items-baseline justify-between rounded-xl bg-slate-50 px-3 py-2">
                    {isVerified ? (
                      <>
                        <div>
                          <div className="text-[10px] font-medium text-emerald-700">
                            ค่าครองชีพรวมจริง / เดือน
                          </div>
                          <div className="text-lg font-extrabold tracking-tight text-indigo-700">
                            ฿{item.cost.true_monthly_cost_thb.toLocaleString()}
                          </div>
                        </div>
                        <div className="text-right text-[11px] text-slate-600">
                          <div>
                            เช่าจริง{' '}
                            <strong className="text-emerald-700">
                              ฿{item.monthly_rent_thb.toLocaleString()}
                            </strong>
                          </div>
                          <div className="text-amber-700">
                            + เดินทาง{' '}
                            <strong>฿{item.cost.monthly_commute_thb.toLocaleString()}</strong>
                          </div>
                        </div>
                      </>
                    ) : (
                      <>
                        <div>
                          <div className="text-[10px] font-medium text-slate-500">
                            ค่าเดินทางจริง / เดือน
                          </div>
                          <div className="text-lg font-extrabold tracking-tight text-indigo-700">
                            {item.cost.monthly_commute_thb === 0
                              ? '฿0 (เดินถึง)'
                              : `฿${item.cost.monthly_commute_thb.toLocaleString()}`}
                          </div>
                        </div>
                        <div className="text-right text-[11px] text-slate-600">
                          {item.cost.monthly_rail_thb > 0 && (
                            <div>
                              รถไฟฟ้า{' '}
                              <strong>฿{item.cost.monthly_rail_thb.toLocaleString()}</strong>
                            </div>
                          )}
                          {item.cost.monthly_feeder_thb > 0 ? (
                            <div className="text-amber-700">
                              วินฯ{' '}
                              <strong>฿{item.cost.monthly_feeder_thb.toLocaleString()}</strong>
                            </div>
                          ) : (
                            <div className="text-emerald-700">เดินเข้าสถานีฟรี</div>
                          )}
                        </div>
                      </>
                    )}
                  </div>

                  {/* Transit Route & Real OSRM Access Mode */}
                  <div className="mt-2.5 flex flex-wrap items-center gap-1.5 text-[11px]">
                    {isDirect ? (
                      <span className="inline-flex items-center gap-1 rounded-md bg-emerald-50 px-2 py-0.5 font-semibold text-emerald-800">
                        {item.home_access.mode === 'walk' ? (
                          <Footprints className="h-3 w-3" />
                        ) : (
                          <Bike className="h-3 w-3 text-orange-600" />
                        )}
                        ตรงถึงออฟฟิศ ~{item.home_access.distance_m} ม. (
                        {item.home_access.mode === 'walk'
                          ? `เดิน ${item.home_access.minutes} น.`
                          : `วิน ฿${item.home_access.one_way_cost_thb}`}
                        )
                      </span>
                    ) : (
                      <>
                        <span
                          className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 font-semibold ${opStyle}`}
                        >
                          <TrainFront className="h-3 w-3" />
                          {opLabel} {item.origin_station.name_th}
                        </span>

                        <span
                          title={
                            item.home_access.distance_source === 'osrm_foot'
                              ? 'ระยะเดินตามถนนและซอยจริง (คำนวณจาก OpenStreetMap + OSRM)'
                              : 'ระยะทางประเมินด้วยตัวคูณถนน กทม. (1.40x)'
                          }
                          className="inline-flex items-center gap-1 rounded-md bg-slate-100 px-2 py-0.5 text-slate-600"
                        >
                          {item.home_access.mode === 'walk' ? (
                            <Footprints className="h-3 w-3" />
                          ) : (
                            <Bike className="h-3 w-3 text-orange-600" />
                          )}
                          {item.home_access.distance_source === 'osrm_foot'
                            ? `ถนน ${item.home_access.distance_m} ม.`
                            : `~${item.home_access.distance_m} ม.`}{' '}
                          ({item.home_access.minutes} น.
                          {item.home_access.one_way_cost_thb > 0
                            ? ` • วิน ฿${item.home_access.one_way_cost_thb}`
                            : ''}
                          )
                        </span>

                        {item.route.legs && item.route.legs.length > 0 && (
                          <span className="rounded-md bg-slate-100 px-2 py-0.5 text-slate-600">
                            {item.route.legs
                              .map(
                                (leg) =>
                                  `${OPERATOR_LABEL[leg.operator] || leg.operator} ฿${leg.fare_thb}`
                              )
                              .join(' + ')}
                          </span>
                        )}

                        {item.office_access.one_way_cost_thb > 0 && (
                          <span className="inline-flex items-center gap-1 rounded-md bg-amber-50 px-2 py-0.5 text-amber-800">
                            <Bike className="h-3 w-3 text-amber-600" />
                            ต่อวินถึงออฟฟิศ ฿{item.office_access.one_way_cost_thb}
                          </span>
                        )}
                      </>
                    )}
                  </div>

                  {/* Direct External Links to Real Room Prices & Building Reviews */}
                  <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
                    {item.latest_source_url && (
                      <a
                        href={item.latest_source_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        onClick={(e) => e.stopPropagation()}
                        className="inline-flex items-center gap-1 rounded-lg border border-emerald-200 bg-emerald-50 px-2 py-1 text-[11px] font-bold text-emerald-800 transition hover:bg-emerald-100"
                      >
                        <ExternalLink className="h-3 w-3" />
                        เว็บหอ / แหล่งราคาจริง
                      </a>
                    )}
                    <a
                      href={buildRealPriceSearchUrl(item)}
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={(e) => e.stopPropagation()}
                      className="inline-flex items-center gap-1 rounded-lg border border-indigo-200 bg-indigo-50/70 px-2 py-1 text-[11px] font-semibold text-indigo-700 transition hover:bg-indigo-100"
                    >
                      <ExternalLink className="h-3 w-3" />
                      เช็กราคาเช่าจริง
                    </a>
                    <a
                      href={buildGoogleMapsUrl(item)}
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={(e) => e.stopPropagation()}
                      className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-white px-2 py-1 text-[11px] font-medium text-slate-600 transition hover:bg-slate-50 hover:text-slate-900"
                    >
                      <MapPin className="h-3 w-3" />
                      แผนที่ / รีวิว
                    </a>
                  </div>

                  {/* Footer Actions: Report Real Price + Compare */}
                  <div className="mt-2.5 flex items-center justify-between gap-2 border-t border-slate-100 pt-2">
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        onOpenReportPrice(item);
                      }}
                      className="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] font-semibold text-emerald-700 transition hover:bg-emerald-50"
                    >
                      <MessageSquarePlus className="h-3.5 w-3.5" />
                      บันทึกราคาจริง + แนบลิงก์
                    </button>

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
            })}

            {visibleCount < filtered.length && (
              <button
                type="button"
                onClick={() => setVisibleCount((prev) => prev + PAGE_SIZE)}
                className="w-full rounded-xl border border-slate-200 bg-white py-2.5 text-xs font-bold text-indigo-700 transition hover:bg-indigo-50"
              >
                แสดงเพิ่มอีก {Math.min(PAGE_SIZE, filtered.length - visibleCount)} แห่ง (เหลือ{' '}
                {(filtered.length - visibleCount).toLocaleString()} แห่ง)
              </button>
            )}
          </>
        )}
      </div>

      {/* Sticky Compare Bar */}
      {compareIds.length > 0 && (
        <div className="border-t border-indigo-100 bg-white px-4 py-3 shadow-[0_-4px_12px_rgba(0,0,0,0.05)]">
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="text-xs font-bold text-slate-900">
                เลือกแล้ว {compareIds.length}/3 แห่ง
              </div>
              <div className="text-[11px] text-slate-500">
                {compareIds.length < 2
                  ? 'เลือกอีกอย่างน้อย 1 แห่งเพื่อเปรียบเทียบ'
                  : 'เทียบค่าเดินทาง + กรอกค่าเช่าจริงเทียบได้ทันที'}
              </div>
            </div>
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={onClearCompare}
                className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-600"
                title="ล้างรายการที่เลือก"
              >
                <X className="h-4 w-4" />
              </button>
              <button
                type="button"
                disabled={compareIds.length < 2}
                onClick={onOpenCompare}
                className="inline-flex items-center gap-1.5 rounded-xl bg-indigo-600 px-3.5 py-2 text-xs font-bold text-white shadow-xs transition hover:bg-indigo-700 disabled:cursor-not-allowed disabled:bg-slate-300"
              >
                <Scale className="h-3.5 w-3.5" />
                เปรียบเทียบเลย
              </button>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}
