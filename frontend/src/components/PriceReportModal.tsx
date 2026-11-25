import { useEffect, useState, type FormEvent } from 'react';
import { CheckCircle2, ExternalLink, Link2, MapPin, ShieldCheck, X } from 'lucide-react';
import { fetchPriceReports, submitPriceReport } from '../api/client';
import {
  OPERATOR_LABEL,
  type ListPriceReportsResponse,
  type ListingResult,
  type RoomType,
} from '../types/commute';
import { buildGoogleMapsUrl, getRealPriceLinks } from '../utils/externalLinks';

interface PriceReportModalProps {
  listing: ListingResult | null;
  initialRent?: number | null;
  onClose: () => void;
  onSubmitted: () => void;
}

const ROOM_TYPE_LABELS: Record<RoomType, string> = {
  studio: 'สตูดิโอ (Studio)',
  '1br': '1 ห้องนอน (1BR)',
  '2br': '2 ห้องนอน (2BR)',
  dorm: 'หอพัก / ห้องเช่า',
};

function currentYearMonth(): string {
  const now = new Date();
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, '0');
  return `${y}-${m}`;
}

export function PriceReportModal({
  listing,
  initialRent,
  onClose,
  onSubmitted,
}: PriceReportModalProps) {
  const [rent, setRent] = useState('');
  const [roomSize, setRoomSize] = useState('');
  const [roomType, setRoomType] = useState<RoomType>('1br');
  const [reportedMonth, setReportedMonth] = useState(currentYearMonth());
  const [sourceUrl, setSourceUrl] = useState('');
  const [note, setNote] = useState('');

  const [existing, setExisting] = useState<ListPriceReportsResponse | null>(null);
  const [loadingExisting, setLoadingExisting] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!listing) return;
    if (initialRent && initialRent > 0) {
      setRent(String(initialRent));
    } else if (listing.price_status === 'verified' && listing.monthly_rent_thb > 0) {
      setRent(String(listing.monthly_rent_thb));
    } else {
      setRent('');
    }
    setRoomSize(String(listing.room_size_sqm || 28));
    setRoomType(listing.property_type === 'apartment' ? 'dorm' : '1br');
    setReportedMonth(currentYearMonth());
    setSourceUrl(listing.latest_source_url || listing.listing_url || '');
    setNote('');
    setError(null);
    setSuccessMsg(null);

    let cancelled = false;
    setLoadingExisting(true);
    fetchPriceReports(listing.id)
      .then((res) => {
        if (!cancelled) setExisting(res);
      })
      .catch(() => {
        if (!cancelled) setExisting(null);
      })
      .finally(() => {
        if (!cancelled) setLoadingExisting(false);
      });

    return () => {
      cancelled = true;
    };
  }, [listing, initialRent]);

  if (!listing) return null;

  const opLabel = OPERATOR_LABEL[listing.origin_station.operator] || listing.origin_station.operator;

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccessMsg(null);

    const parsedRent = parseInt(rent, 10);
    const parsedSize = parseFloat(roomSize);
    if (Number.isNaN(parsedRent) || parsedRent < 1500 || parsedRent > 500000) {
      setError('กรุณากรอกค่าเช่าจริงระหว่าง ฿1,500 – ฿500,000 ต่อเดือน');
      return;
    }
    if (Number.isNaN(parsedSize) || parsedSize < 10 || parsedSize > 500) {
      setError('กรุณากรอกขนาดห้องระหว่าง 10 – 500 ตร.ม.');
      return;
    }

    const trimmedUrl = sourceUrl.trim();
    if (trimmedUrl && !/^https?:\/\/.+/i.test(trimmedUrl)) {
      setError('ลิงก์อ้างอิงต้องขึ้นต้นด้วย https:// หรือ http://');
      return;
    }

    setSubmitting(true);
    try {
      const res = await submitPriceReport(listing.id, {
        monthly_rent_thb: parsedRent,
        room_size_sqm: parsedSize,
        room_type: roomType,
        reported_month: reportedMonth,
        source_url: trimmedUrl,
        note: note.trim(),
      });
      setSuccessMsg(res.message);
      const refreshed = await fetchPriceReports(listing.id).catch(() => null);
      if (refreshed) setExisting(refreshed);
      onSubmitted();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'ส่งรายงานไม่สำเร็จ');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-[1100] flex items-center justify-center bg-slate-900/55 p-4 backdrop-blur-xs"
      onClick={onClose}
    >
      <div
        className="flex max-h-[90vh] w-full max-w-lg flex-col overflow-hidden rounded-3xl bg-white shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-start justify-between border-b border-slate-200 px-6 py-4">
          <div className="min-w-0 pr-3">
            <div className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-2.5 py-0.5 text-[11px] font-semibold text-emerald-700">
              <ShieldCheck className="h-3.5 w-3.5" />
              ข้อมูลราคาจริงจากผู้เช่า / เว็บหอ
            </div>
            <h2 className="mt-1 truncate text-base font-bold text-slate-900">
              {listing.title_th}
            </h2>
            <p className="text-xs text-slate-500">
              {opLabel} {listing.origin_station.name_th} • ถนน {listing.home_access.distance_m} ม.
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-full p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 space-y-4 overflow-y-auto p-6">
          {/* Quick Links to Check Real Price First */}
          {(() => {
            const priceLinks = getRealPriceLinks(listing);
            return (
              <div className="rounded-2xl border border-indigo-100 bg-indigo-50/50 p-3.5">
                <div className="text-xs font-semibold text-slate-700">
                  เช็กราคาห้องจริงจากเว็บประกาศเช่าก่อนบันทึก:
                </div>
                <div className="mt-2 flex flex-wrap gap-2">
                  <a
                    href={priceLinks.primary.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className={`inline-flex items-center gap-1.5 rounded-xl px-3 py-1.5 text-xs font-bold text-white transition ${
                      priceLinks.primary.tone === 'emerald'
                        ? 'bg-emerald-600 hover:bg-emerald-700'
                        : 'bg-indigo-600 hover:bg-indigo-700'
                    }`}
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                    {priceLinks.primary.label}
                  </a>
                  {priceLinks.secondary && (
                    <a
                      href={priceLinks.secondary.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1.5 rounded-xl border border-indigo-200 bg-white px-3 py-1.5 text-xs font-semibold text-indigo-700 transition hover:bg-indigo-50"
                    >
                      <ExternalLink className="h-3.5 w-3.5" />
                      {priceLinks.secondary.label}
                    </a>
                  )}
                  <a
                    href={buildGoogleMapsUrl(listing)}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1.5 rounded-xl border border-slate-200 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 transition hover:bg-slate-50"
                  >
                    <MapPin className="h-3.5 w-3.5" />
                    ดูรูปตึก / รีวิวบน Maps
                  </a>
                </div>
                {existing && existing.stats.report_count > 0 && (
                  <div className="mt-2.5 flex items-center justify-between border-t border-indigo-100 pt-2 text-xs text-emerald-800">
                    <span className="font-semibold">
                      ราคามัธยฐานจากผู้เช่า ({existing.stats.report_count} รายงาน):
                    </span>
                    <strong className="text-sm">
                      ฿{existing.stats.median_rent_thb.toLocaleString()}/ด.
                    </strong>
                  </div>
                )}
              </div>
            );
          })()}

          {/* Report Form */}
          <form onSubmit={handleSubmit} className="space-y-3.5">
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-semibold text-slate-700">
                  ค่าเช่าจริง (บาท/เดือน) *
                </label>
                <input
                  type="number"
                  required
                  min={1500}
                  max={500000}
                  step={100}
                  value={rent}
                  onChange={(e) => setRent(e.target.value)}
                  className="mt-1 w-full rounded-xl border border-slate-200 px-3 py-2 text-sm font-semibold text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                  placeholder="เช่น 14500 หรือ 80000"
                />
              </div>
              <div>
                <label className="block text-xs font-semibold text-slate-700">
                  ขนาดห้อง (ตร.ม.) *
                </label>
                <input
                  type="number"
                  required
                  min={10}
                  max={500}
                  step={0.5}
                  value={roomSize}
                  onChange={(e) => setRoomSize(e.target.value)}
                  className="mt-1 w-full rounded-xl border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                  placeholder="เช่น 30"
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-semibold text-slate-700">
                  ประเภทห้อง *
                </label>
                <select
                  value={roomType}
                  onChange={(e) => setRoomType(e.target.value as RoomType)}
                  className="mt-1 w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                >
                  {(Object.keys(ROOM_TYPE_LABELS) as RoomType[]).map((rt) => (
                    <option key={rt} value={rt}>
                      {ROOM_TYPE_LABELS[rt]}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs font-semibold text-slate-700">
                  เดือนที่เช่า/เช็กราคา *
                </label>
                <input
                  type="month"
                  required
                  value={reportedMonth}
                  max={currentYearMonth()}
                  onChange={(e) => setReportedMonth(e.target.value)}
                  className="mt-1 w-full rounded-xl border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                />
              </div>
            </div>

            <div>
              <label className="flex items-center gap-1 text-xs font-semibold text-slate-700">
                <Link2 className="h-3.5 w-3.5 text-indigo-600" />
                แนบลิงก์เว็บหอ / ประกาศเช่าจริง (ไม่บังคับ)
              </label>
              <input
                type="url"
                maxLength={500}
                value={sourceUrl}
                onChange={(e) => setSourceUrl(e.target.value)}
                placeholder="https://... (เช่น เว็บไซต์หอพัก, Renthub, DDproperty)"
                className="mt-1 w-full rounded-xl border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
              />
            </div>

            <div>
              <label className="block text-xs font-semibold text-slate-700">
                รายละเอียดเพิ่มเติม (ไม่บังคับ)
              </label>
              <input
                type="text"
                maxLength={200}
                value={note}
                onChange={(e) => setNote(e.target.value)}
                placeholder="เช่น สัญญา 1 ปี เฟอร์ครบ ค่าน้ำไฟตามมิเตอร์"
                className="mt-1 w-full rounded-xl border border-slate-200 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
              />
            </div>

            {error && (
              <div className="rounded-xl bg-rose-50 px-3.5 py-2.5 text-xs font-medium text-rose-700">
                {error}
              </div>
            )}

            {successMsg && (
              <div className="flex items-start gap-2 rounded-xl bg-emerald-50 px-3.5 py-2.5 text-xs font-medium text-emerald-800">
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-600" />
                <span>{successMsg}</span>
              </div>
            )}

            <button
              type="submit"
              disabled={submitting}
              className="w-full rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-bold text-white shadow-xs transition hover:bg-indigo-700 disabled:opacity-60"
            >
              {submitting ? 'กำลังบันทึก...' : 'บันทึกราคาเช่าจริง'}
            </button>

            <p className="text-[11px] text-slate-400">
              ไม่เก็บข้อมูลส่วนตัว • จำกัด 1 รายงาน/ที่พัก/30 วัน เพื่อป้องกันสแปม
            </p>
          </form>

          {/* Recent Verified Reports */}
          <div className="border-t border-slate-100 pt-3">
            <h3 className="text-xs font-bold text-slate-700">
              รายงานราคาจริงจากผู้ใช้งานล่าสุด
            </h3>
            {loadingExisting ? (
              <p className="mt-2 text-xs text-slate-400">กำลังโหลด...</p>
            ) : existing && existing.reports.length > 0 ? (
              <ul className="mt-2 space-y-1.5">
                {existing.reports.slice(0, 6).map((r) => (
                  <li
                    key={r.id}
                    className="flex items-start justify-between gap-2 rounded-xl border border-slate-100 bg-slate-50 px-3 py-2 text-xs"
                  >
                    <div className="min-w-0">
                      <span className="font-bold text-emerald-700">
                        ฿{r.monthly_rent_thb.toLocaleString()}/ด.
                      </span>{' '}
                      <span className="text-slate-600">
                        • {ROOM_TYPE_LABELS[r.room_type]} {r.room_size_sqm} ตร.ม.
                      </span>
                      {r.note && (
                        <div className="text-[11px] text-slate-500">{r.note}</div>
                      )}
                      {r.source_url && (
                        <a
                          href={r.source_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="mt-0.5 inline-flex items-center gap-1 text-[11px] font-semibold text-indigo-600 hover:underline"
                        >
                          <ExternalLink className="h-3 w-3" />
                          เปิดลิงก์อ้างอิงราคา
                        </a>
                      )}
                    </div>
                    <span className="shrink-0 font-mono text-[11px] text-slate-400">
                      {r.reported_month}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-1.5 text-xs text-slate-400">
                ยังไม่มีผู้รายงานราคาจริงของตึกนี้ — กดปุ่มด้านบนเพื่อเช็กราคาจริงแล้วบันทึกแบ่งปันได้เลยครับ
              </p>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
