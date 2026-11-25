import { useEffect, useState } from 'react';
import {
  Award,
  Clock,
  ExternalLink,
  MapPin,
  MessageSquarePlus,
  ShieldCheck,
  Sparkles,
  TrainFront,
  X,
  Zap,
} from 'lucide-react';
import {
  OPERATOR_LABEL,
  type CompareResponse,
  type ListingResult,
} from '../types/commute';
import { buildGoogleMapsUrl, buildRealPriceSearchUrl } from '../utils/externalLinks';

interface CompareModalProps {
  open: boolean;
  loading: boolean;
  error: string | null;
  data: CompareResponse | null;
  onClose: () => void;
  onSharePrice: (listing: ListingResult, rentTHB: number) => void;
}

export function CompareModal({
  open,
  loading,
  error,
  data,
  onClose,
  onSharePrice,
}: CompareModalProps) {
  // User-entered real rent per listing ID (pre-populated ONLY from verified crowdsourced prices)
  const [customRents, setCustomRents] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!data) return;
    setCustomRents((prev) => {
      const next: Record<string, string> = {};
      for (const item of data.items) {
        if (prev[item.id] !== undefined) {
          next[item.id] = prev[item.id];
        } else if (item.price_status === 'verified' && item.monthly_rent_thb > 0) {
          next[item.id] = String(item.monthly_rent_thb);
        } else {
          next[item.id] = '';
        }
      }
      return next;
    });
  }, [data]);

  if (!open) return null;

  const items = data?.items ?? [];

  // Compute live effective rent & true monthly cost per item
  const computed = items.map((item) => {
    const raw = (customRents[item.id] || '').replace(/[^0-9]/g, '');
    const parsedRent = raw ? parseInt(raw, 10) : 0;
    const hasRent = parsedRent > 0;
    const trueMonthly = hasRent ? parsedRent + item.cost.monthly_commute_thb : 0;
    const isCustomTyped =
      hasRent &&
      (item.price_status !== 'verified' || parsedRent !== item.monthly_rent_thb);
    return { item, rent: parsedRent, hasRent, trueMonthly, isCustomTyped };
  });

  const allHaveRent = computed.length > 0 && computed.every((c) => c.hasRent);
  const maxTrueCost = allHaveRent
    ? Math.max(...computed.map((c) => c.trueMonthly), 1)
    : 1;
  const minTrueCost = allHaveRent
    ? Math.min(...computed.map((c) => c.trueMonthly))
    : 0;
  const maxTrueSavings = allHaveRent ? maxTrueCost - minTrueCost : 0;

  const maxCommuteCost = computed.length > 0
    ? Math.max(...computed.map((c) => c.item.cost.monthly_commute_thb), 1)
    : 1;
  const minCommuteCost = computed.length > 0
    ? Math.min(...computed.map((c) => c.item.cost.monthly_commute_thb))
    : 0;
  const maxCommuteSavings = computed.length > 0
    ? Math.max(...computed.map((c) => c.item.cost.monthly_commute_thb)) - minCommuteCost
    : 0;

  const maxHours = computed.length > 0
    ? Math.max(...computed.map((c) => c.item.cost.monthly_commute_hours), 1)
    : 1;
  const minOneWayMinutes = computed.length > 0
    ? Math.min(...computed.map((c) => c.item.cost.one_way_minutes))
    : 0;

  const hasTrueCostDiff = allHaveRent && maxTrueSavings > 0;
  const hasCommuteCostDiff = !allHaveRent && maxCommuteSavings > 0;
  const hasTimeDiff = Boolean(data && data.summary.max_monthly_hours_saved > 0);

  return (
    <div
      className="fixed inset-0 z-[1000] flex items-center justify-center bg-slate-900/55 p-4 backdrop-blur-xs"
      onClick={onClose}
    >
      <div
        className="flex max-h-[92vh] w-full max-w-4xl flex-col overflow-hidden rounded-3xl bg-white shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Modal Header */}
        <div className="flex items-center justify-between border-b border-slate-200 px-6 py-4">
          <div>
            <h2 className="text-lg font-bold text-slate-900">
              เปรียบเทียบทำเลจากข้อมูลจริง (Commute &amp; True Monthly Cost)
            </h2>
            {data && (
              <p className="text-xs text-slate-500">
                คำนวณจากวันเข้าออฟฟิศ {data.workdays} วัน/เดือน • ไป-กลับ รวมค่ารถไฟฟ้าจริงและค่าวินมอเตอร์ไซค์ • กดลิงก์เช็กราคาห้องจริงแล้วกรอกเทียบได้เลย
              </p>
            )}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-full p-2 text-slate-500 hover:bg-slate-100 hover:text-slate-800"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Modal Body */}
        <div className="flex-1 overflow-y-auto p-6">
          {loading ? (
            <div className="py-16 text-center text-sm text-slate-500">
              กำลังคำนวณเปรียบเทียบค่าเดินทางและเวลาเดินทาง...
            </div>
          ) : error ? (
            <div className="rounded-xl bg-rose-50 p-4 text-sm text-rose-700">{error}</div>
          ) : data && computed.length > 0 ? (
            <div className="space-y-5">
              {/* Insight Banner */}
              <div className="flex flex-wrap items-center justify-between gap-4 rounded-2xl border border-indigo-200 bg-gradient-to-r from-indigo-50 to-emerald-50 p-4">
                <div className="flex items-start gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-indigo-600 text-white">
                    <Sparkles className="h-5 w-5" />
                  </div>
                  <div>
                    <div className="text-xs font-semibold uppercase tracking-wider text-indigo-700">
                      สรุป Trade-off จากข้อมูลจริง
                    </div>
                    <p className="mt-0.5 text-sm text-slate-800">
                      {allHaveRent ? (
                        hasTrueCostDiff && hasTimeDiff ? (
                          <>
                            ส่วนต่างค่าครองชีพรวมจริงสูงสุด{' '}
                            <strong className="text-emerald-700">
                              ฿{maxTrueSavings.toLocaleString()}/เดือน
                            </strong>{' '}
                            และส่วนต่างเวลาชีวิตบนถนนสูงสุด{' '}
                            <strong className="text-indigo-700">
                              {data.summary.max_monthly_hours_saved} ชั่วโมง/เดือน
                            </strong>
                          </>
                        ) : !hasTrueCostDiff && hasTimeDiff ? (
                          <>
                            ทุกทำเลมีค่าครองชีพรวมจริงเท่ากันที่{' '}
                            <strong className="text-slate-900">
                              ฿{minTrueCost.toLocaleString()}/เดือน
                            </strong>{' '}
                            — ประหยัดเวลาชีวิตบนถนนได้สูงสุด{' '}
                            <strong className="text-indigo-700">
                              {data.summary.max_monthly_hours_saved} ชั่วโมง/เดือน
                            </strong>
                          </>
                        ) : hasTrueCostDiff && !hasTimeDiff ? (
                          <>
                            ทุกทำเลใช้เวลาเดินทางเท่ากันที่{' '}
                            <strong className="text-slate-900">
                              {computed[0].item.cost.monthly_commute_hours} ชม./เดือน
                            </strong>{' '}
                            — ประหยัดค่าครองชีพรวมได้สูงสุด{' '}
                            <strong className="text-emerald-700">
                              ฿{maxTrueSavings.toLocaleString()}/เดือน
                            </strong>
                          </>
                        ) : (
                          <>
                            ทุกทำเลมีค่าครองชีพรวมและเวลาเดินทางเท่ากัน (฿
                            {minTrueCost.toLocaleString()}/เดือน •{' '}
                            {computed[0].item.cost.monthly_commute_hours} ชม./เดือน)
                          </>
                        )
                      ) : (
                        <>
                          ส่วนต่างค่าเดินทางจริงสูงสุด{' '}
                          <strong className="text-emerald-700">
                            ฿{maxCommuteSavings.toLocaleString()}/เดือน
                          </strong>{' '}
                          และส่วนต่างเวลาชีวิตบนถนนสูงสุด{' '}
                          <strong className="text-indigo-700">
                            {data.summary.max_monthly_hours_saved} ชั่วโมง/เดือน
                          </strong>{' '}
                          <span className="text-xs text-slate-500">
                            (กรอกค่าเช่าจริงด้านล่างให้ครบเพื่อคำนวณค่าครองชีพรวมจริง)
                          </span>
                        </>
                      )}
                    </p>
                  </div>
                </div>
              </div>

              {/* Side-by-Side Cards */}
              <div
                className={`grid gap-4 ${
                  computed.length === 2 ? 'md:grid-cols-2' : 'md:grid-cols-3'
                }`}
              >
                {computed.map(({ item, rent, hasRent, trueMonthly, isCustomTyped }) => {
                  const isCheapestTotal = hasTrueCostDiff && trueMonthly === minTrueCost;
                  const isCheapestCommute =
                    hasCommuteCostDiff &&
                    item.cost.monthly_commute_thb === minCommuteCost;
                  const isFastest =
                    hasTimeDiff && item.cost.one_way_minutes === minOneWayMinutes;
                  const isDirect = item.route.station_ids.length <= 1;

                  const highlightEmerald = isCheapestTotal || isCheapestCommute;
                  const highlightIndigo =
                    !highlightEmerald && !hasTrueCostDiff && !hasCommuteCostDiff && isFastest;

                  const rentPct = allHaveRent
                    ? Math.round((rent / maxTrueCost) * 100)
                    : 0;
                  const commutePct = allHaveRent
                    ? Math.round((item.cost.monthly_commute_thb / maxTrueCost) * 100)
                    : Math.round((item.cost.monthly_commute_thb / maxCommuteCost) * 100);
                  const hoursPct = Math.round(
                    (item.cost.monthly_commute_hours / maxHours) * 100
                  );

                  return (
                    <div
                      key={item.id}
                      className={`flex flex-col rounded-2xl border p-4 ${
                        highlightEmerald
                          ? 'border-emerald-400 bg-emerald-50/30 ring-1 ring-emerald-400/40'
                          : highlightIndigo
                            ? 'border-indigo-400 bg-indigo-50/30 ring-1 ring-indigo-400/40'
                            : 'border-slate-200 bg-white'
                      }`}
                    >
                      {/* Winner Badges */}
                      <div className="mb-2 flex min-h-[24px] flex-wrap gap-1.5">
                        {isCheapestTotal && (
                          <span className="inline-flex items-center gap-1 rounded-full bg-emerald-600 px-2.5 py-0.5 text-[11px] font-bold text-white">
                            <Award className="h-3 w-3" />
                            รวมจริงถูกสุด
                          </span>
                        )}
                        {isCheapestCommute && (
                          <span className="inline-flex items-center gap-1 rounded-full bg-emerald-600 px-2.5 py-0.5 text-[11px] font-bold text-white">
                            <Award className="h-3 w-3" />
                            ค่าเดินทางถูกสุด
                          </span>
                        )}
                        {isFastest && (
                          <span className="inline-flex items-center gap-1 rounded-full bg-indigo-600 px-2.5 py-0.5 text-[11px] font-bold text-white">
                            <Zap className="h-3 w-3" />
                            เดินทางเร็วสุด
                          </span>
                        )}
                      </div>

                      <h3 className="text-base font-bold text-slate-900">{item.title_th}</h3>
                      <p className="text-xs text-slate-500">
                        {isDirect
                          ? `ใกล้จุดทำงาน (~${item.home_access.distance_m} ม.)`
                          : `${OPERATOR_LABEL[item.origin_station.operator] || item.origin_station.operator} ${item.origin_station.name_th}`}
                      </p>

                      {/* 1-Click Links to Real Room Prices */}
                      <div className="mt-2 flex flex-wrap gap-1.5">
                        {item.latest_source_url && (
                          <a
                            href={item.latest_source_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 rounded-lg border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[11px] font-bold text-emerald-800 hover:bg-emerald-100"
                          >
                            <ExternalLink className="h-3 w-3" />
                            เว็บหอ
                          </a>
                        )}
                        <a
                          href={buildRealPriceSearchUrl(item)}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="inline-flex items-center gap-1 rounded-lg border border-indigo-200 bg-indigo-50 px-2 py-0.5 text-[11px] font-semibold text-indigo-700 hover:bg-indigo-100"
                        >
                          <ExternalLink className="h-3 w-3" />
                          เช็กราคาจริง
                        </a>
                        <a
                          href={buildGoogleMapsUrl(item)}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-white px-2 py-0.5 text-[11px] text-slate-600 hover:bg-slate-50"
                        >
                          <MapPin className="h-3 w-3" />
                          Maps
                        </a>
                      </div>

                      {/* Interactive Real Rent Input */}
                      <div className="mt-3">
                        <div className="flex items-center justify-between text-[11px]">
                          <label className="font-semibold text-slate-700">
                            ค่าเช่าจริง (฿/เดือน)
                          </label>
                          {item.price_status === 'verified' && !isCustomTyped && (
                            <span className="inline-flex items-center gap-0.5 font-semibold text-emerald-700">
                              <ShieldCheck className="h-3 w-3" />
                              จากผู้เช่า ({item.verified_report_count})
                            </span>
                          )}
                        </div>
                        <input
                          type="number"
                          min={0}
                          max={500000}
                          step={500}
                          value={customRents[item.id] ?? ''}
                          onChange={(e) =>
                            setCustomRents((prev) => ({
                              ...prev,
                              [item.id]: e.target.value,
                            }))
                          }
                          placeholder="เช็กลิงก์แล้วกรอก เช่น 12500"
                          className="mt-1 w-full rounded-xl border border-slate-300 bg-white px-3 py-1.5 text-sm font-bold text-slate-900 placeholder:text-xs placeholder:font-normal placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                        />
                        {isCustomTyped && rent >= 1500 && (
                          <button
                            type="button"
                            onClick={() => onSharePrice(item, rent)}
                            className="mt-1 inline-flex items-center gap-1 text-[11px] font-semibold text-emerald-700 hover:underline"
                          >
                            <MessageSquarePlus className="h-3 w-3" />
                            แชร์ราคา ฿{rent.toLocaleString()} ลงระบบให้คนอื่น
                          </button>
                        )}
                      </div>

                      {/* Big Metric Box: True Monthly Cost (if rent entered) OR Monthly Commute Cost */}
                      <div className="mt-3 rounded-xl bg-slate-900 px-4 py-3 text-white">
                        {hasRent ? (
                          <>
                            <div className="text-[11px] text-slate-300">
                              ค่าครองชีพรวมจริง / เดือน
                            </div>
                            <div className="text-2xl font-extrabold">
                              ฿{trueMonthly.toLocaleString()}
                            </div>
                          </>
                        ) : (
                          <>
                            <div className="text-[11px] text-slate-300">
                              ค่าเดินทางจริง / เดือน
                            </div>
                            <div className="text-2xl font-extrabold">
                              ฿{item.cost.monthly_commute_thb.toLocaleString()}
                            </div>
                          </>
                        )}
                      </div>

                      {/* Cost Bar */}
                      <div className="mt-3.5 space-y-1.5">
                        {allHaveRent ? (
                          <>
                            <div className="flex justify-between text-xs">
                              <span className="text-slate-600">ค่าเช่าจริง + ค่าเดินทาง</span>
                              <span className="font-semibold text-slate-900">
                                ฿{rent.toLocaleString()} + ฿
                                {item.cost.monthly_commute_thb.toLocaleString()}
                              </span>
                            </div>
                            <div className="flex h-3 w-full overflow-hidden rounded-full bg-slate-100">
                              <div
                                style={{ width: `${rentPct}%` }}
                                className="bg-indigo-500"
                              />
                              <div
                                style={{ width: `${commutePct}%` }}
                                className="bg-amber-400"
                              />
                            </div>
                          </>
                        ) : (
                          <>
                            <div className="flex justify-between text-xs">
                              <span className="text-slate-600">ค่าเดินทางจริง / เดือน</span>
                              <span className="font-semibold text-amber-700">
                                ฿{item.cost.monthly_commute_thb.toLocaleString()}
                              </span>
                            </div>
                            <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-100">
                              <div
                                style={{ width: `${commutePct}%` }}
                                className="h-full rounded-full bg-amber-400"
                              />
                            </div>
                          </>
                        )}
                      </div>

                      {/* Monthly Commute Hours Bar */}
                      <div className="mt-3.5 space-y-1.5">
                        <div className="flex justify-between text-xs">
                          <span className="flex items-center gap-1 text-slate-600">
                            <Clock className="h-3.5 w-3.5" />
                            เวลาเดินทางรวม/เดือน
                          </span>
                          <span className="font-bold text-slate-900">
                            {item.cost.monthly_commute_hours} ชม.
                          </span>
                        </div>
                        <div className="h-2.5 w-full overflow-hidden rounded-full bg-slate-100">
                          <div
                            style={{ width: `${hoursPct}%` }}
                            className="h-full rounded-full bg-rose-500"
                          />
                        </div>
                      </div>

                      {/* Detail rows */}
                      <dl className="mt-3.5 space-y-1.5 border-t border-slate-200 pt-3 text-xs text-slate-600">
                        <div className="flex justify-between">
                          <dt>เวลาเดินทางขาเดียว</dt>
                          <dd className="font-semibold text-slate-900">
                            {item.cost.one_way_minutes} นาที
                          </dd>
                        </div>
                        <div className="flex justify-between">
                          <dt>ค่ารถไฟฟ้า/เที่ยว</dt>
                          <dd className="font-semibold text-slate-900">
                            ฿{item.cost.one_way_fare_thb}
                          </dd>
                        </div>
                        <div className="flex justify-between">
                          <dt>{isDirect ? 'ระยะตรงถึงออฟฟิศ' : 'เข้าสถานี (ถนนจริง)'}</dt>
                          <dd className="font-semibold text-slate-900">
                            {item.home_access.mode === 'walk'
                              ? `เดิน ${item.home_access.distance_m} ม.`
                              : `วินฯ ${item.home_access.distance_m} ม. (+฿${item.home_access.one_way_cost_thb})`}
                          </dd>
                        </div>
                        {!isDirect && item.office_access.one_way_cost_thb > 0 && (
                          <div className="flex justify-between">
                            <dt>ต่อถึงออฟฟิศ (ปลายทาง)</dt>
                            <dd className="font-semibold text-amber-700">
                              วินฯ ~{item.office_access.distance_m} ม. (+฿
                              {item.office_access.one_way_cost_thb})
                            </dd>
                          </div>
                        )}
                        <div className="flex justify-between">
                          <dt className="flex items-center gap-1">
                            <TrainFront className="h-3.5 w-3.5" />
                            เส้นทาง
                          </dt>
                          <dd className="font-semibold text-slate-900">
                            {item.route.legs && item.route.legs.length > 0
                              ? item.route.legs
                                  .map(
                                    (l) =>
                                      `${OPERATOR_LABEL[l.operator] || l.operator} (฿${l.fare_thb})`
                                  )
                                  .join(' → ')
                              : item.home_access.mode === 'walk'
                                ? 'เดินถึงโดยตรง'
                                : 'วินฯ ถึงโดยตรง'}
                          </dd>
                        </div>
                      </dl>
                    </div>
                  );
                })}
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
