import { Award, Clock, Sparkles, TrainFront, X, Zap } from 'lucide-react';
import type { CompareResponse } from '../types/commute';

interface CompareModalProps {
  open: boolean;
  loading: boolean;
  error: string | null;
  data: CompareResponse | null;
  onClose: () => void;
}

export function CompareModal({
  open,
  loading,
  error,
  data,
  onClose,
}: CompareModalProps) {
  if (!open) return null;

  const maxTrueCost = data
    ? Math.max(...data.items.map((i) => i.cost.true_monthly_cost_thb), 1)
    : 1;
  const minTrueCost = data && data.items.length > 0
    ? Math.min(...data.items.map((i) => i.cost.true_monthly_cost_thb))
    : 0;
  const maxHours = data
    ? Math.max(...data.items.map((i) => i.cost.monthly_commute_hours), 1)
    : 1;
  const minOneWayMinutes = data && data.items.length > 0
    ? Math.min(...data.items.map((i) => i.cost.one_way_minutes))
    : 0;

  const hasCostDiff = Boolean(data && data.summary.max_monthly_savings_thb > 0);
  const hasTimeDiff = Boolean(data && data.summary.max_monthly_hours_saved > 0);

  return (
    <div
      className="fixed inset-0 z-[1000] flex items-center justify-center bg-slate-900/55 p-4 backdrop-blur-xs"
      onClick={onClose}
    >
      <div
        className="flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden rounded-3xl bg-white shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Modal Header */}
        <div className="flex items-center justify-between border-b border-slate-200 px-6 py-4">
          <div>
            <h2 className="text-lg font-bold text-slate-900">
              เปรียบเทียบทำเลแบบเห็นภาพจริง (True Monthly Cost & Time)
            </h2>
            {data && (
              <p className="text-xs text-slate-500">
                คำนวณจากวันเข้าออฟฟิศ {data.workdays} วัน/เดือน • ไป-กลับ รวมค่ารถไฟฟ้าและค่าวินมอเตอร์ไซค์
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
              กำลังคำนวณเปรียบเทียบค่าครองชีพและเวลาเดินทาง...
            </div>
          ) : error ? (
            <div className="rounded-xl bg-rose-50 p-4 text-sm text-rose-700">{error}</div>
          ) : data && data.items.length > 0 ? (
            <div className="space-y-6">
              {/* Insight Banner */}
              <div className="flex flex-wrap items-center justify-between gap-4 rounded-2xl border border-indigo-200 bg-gradient-to-r from-indigo-50 to-emerald-50 p-4">
                <div className="flex items-start gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-indigo-600 text-white">
                    <Sparkles className="h-5 w-5" />
                  </div>
                  <div>
                    <div className="text-xs font-semibold uppercase tracking-wider text-indigo-700">
                      สรุป Trade-off สำคัญ
                    </div>
                    <p className="mt-0.5 text-sm text-slate-800">
                      {hasCostDiff && hasTimeDiff ? (
                        <>
                          ส่วนต่างค่าครองชีพรวมสูงสุด{' '}
                          <strong className="text-emerald-700">
                            ฿{data.summary.max_monthly_savings_thb.toLocaleString()}/เดือน
                          </strong>{' '}
                          และส่วนต่างเวลาชีวิตบนถนนสูงสุด{' '}
                          <strong className="text-indigo-700">
                            {data.summary.max_monthly_hours_saved} ชั่วโมง/เดือน
                          </strong>
                        </>
                      ) : !hasCostDiff && hasTimeDiff ? (
                        <>
                          ทุกทำเลมีค่าครองชีพรวมเท่ากันที่{' '}
                          <strong className="text-slate-900">
                            ฿{minTrueCost.toLocaleString()}/เดือน
                          </strong>{' '}
                          — ประหยัดเวลาชีวิตบนถนนได้สูงสุด{' '}
                          <strong className="text-indigo-700">
                            {data.summary.max_monthly_hours_saved} ชั่วโมง/เดือน
                          </strong>
                        </>
                      ) : hasCostDiff && !hasTimeDiff ? (
                        <>
                          ทุกทำเลใช้เวลาเดินทางเท่ากันที่{' '}
                          <strong className="text-slate-900">
                            {data.items[0].cost.monthly_commute_hours} ชม./เดือน
                          </strong>{' '}
                          — ประหยัดค่าครองชีพรวมได้สูงสุด{' '}
                          <strong className="text-emerald-700">
                            ฿{data.summary.max_monthly_savings_thb.toLocaleString()}/เดือน
                          </strong>
                        </>
                      ) : (
                        <>
                          ทุกทำเลที่เลือกมีค่าครองชีพรวมและเวลาเดินทางเท่ากันที่{' '}
                          <strong className="text-slate-900">
                            ฿{minTrueCost.toLocaleString()}/เดือน •{' '}
                            {data.items[0].cost.monthly_commute_hours} ชม./เดือน
                          </strong>
                        </>
                      )}
                    </p>
                  </div>
                </div>
              </div>

              {/* Side-by-Side Cards */}
              <div
                className={`grid gap-4 ${
                  data.items.length === 2 ? 'md:grid-cols-2' : 'md:grid-cols-3'
                }`}
              >
                {data.items.map((item) => {
                  const isCheapest =
                    hasCostDiff && item.cost.true_monthly_cost_thb === minTrueCost;
                  const isFastest =
                    hasTimeDiff && item.cost.one_way_minutes === minOneWayMinutes;
                  const highlightEmerald = isCheapest;
                  const highlightIndigo = !hasCostDiff && isFastest;
                  const rentPct = Math.round(
                    (item.monthly_rent_thb / maxTrueCost) * 100
                  );
                  const commutePct = Math.round(
                    (item.cost.monthly_commute_thb / maxTrueCost) * 100
                  );
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
                        {isCheapest && (
                          <span className="inline-flex items-center gap-1 rounded-full bg-emerald-600 px-2.5 py-0.5 text-[11px] font-bold text-white">
                            <Award className="h-3 w-3" />
                            รวมจริงถูกสุด
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
                        {item.origin_station.operator} {item.origin_station.name_th} •{' '}
                        {item.room_size_sqm} ตร.ม.
                      </p>

                      {/* Big True Monthly Cost */}
                      <div className="mt-4 rounded-xl bg-slate-900 px-4 py-3 text-white">
                        <div className="text-[11px] text-slate-300">ค่าครองชีพรวมจริง / เดือน</div>
                        <div className="text-2xl font-extrabold">
                          ฿{item.cost.true_monthly_cost_thb.toLocaleString()}
                        </div>
                      </div>

                      {/* Stacked Bar Chart: Rent vs Commute */}
                      <div className="mt-4 space-y-1.5">
                        <div className="flex justify-between text-xs">
                          <span className="text-slate-600">สัดส่วนค่าเช่า + ค่าเดินทาง</span>
                          <span className="font-semibold text-slate-900">
                            ฿{item.monthly_rent_thb.toLocaleString()} + ฿
                            {item.cost.monthly_commute_thb.toLocaleString()}
                          </span>
                        </div>
                        <div className="flex h-3.5 w-full overflow-hidden rounded-full bg-slate-100">
                          <div
                            style={{ width: `${rentPct}%` }}
                            className="bg-indigo-500"
                            title={`ค่าเช่า ฿${item.monthly_rent_thb.toLocaleString()}`}
                          />
                          <div
                            style={{ width: `${commutePct}%` }}
                            className="bg-amber-400"
                            title={`ค่าเดินทาง ฿${item.cost.monthly_commute_thb.toLocaleString()}`}
                          />
                        </div>
                        <div className="flex gap-3 text-[11px] text-slate-500">
                          <span className="flex items-center gap-1">
                            <span className="h-2 w-2 rounded-full bg-indigo-500" />
                            ค่าเช่า
                          </span>
                          <span className="flex items-center gap-1">
                            <span className="h-2 w-2 rounded-full bg-amber-400" />
                            ค่าเดินทาง/ด.
                          </span>
                        </div>
                      </div>

                      {/* Monthly Commute Hours Bar */}
                      <div className="mt-4 space-y-1.5">
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
                      <dl className="mt-4 space-y-1.5 border-t border-slate-200 pt-3 text-xs text-slate-600">
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
                          <dt>เข้าสถานี (ต้นทาง)</dt>
                          <dd className="font-semibold text-slate-900">
                            {item.home_access.mode === 'walk'
                              ? `เดิน ${item.home_access.distance_m} ม.`
                              : `วินฯ ${item.home_access.distance_m} ม. (+฿${item.home_access.one_way_cost_thb})`}
                          </dd>
                        </div>
                        {item.office_access.one_way_cost_thb > 0 && (
                          <div className="flex justify-between">
                            <dt>ต่อถึงออฟฟิศ (ปลายทาง)</dt>
                            <dd className="font-semibold text-amber-700">
                              วินฯ {item.office_access.distance_m} ม. (+฿
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
                            {item.route.legs
                              ?.map((l) => `${l.operator} (฿${l.fare_thb})`)
                              .join(' → ') || 'เดินถึง'}
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
