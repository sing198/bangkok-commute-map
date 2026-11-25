#!/usr/bin/env bash
# Monthly / Folder-Watch Offline Price Sync Runner for Bangkok Commute Map
# Triggered automatically by macOS launchd (WatchPaths on data/har/ or monthly calendar schedule).
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HAR_DIR="${REPO_DIR}/data/har"
LOG_DIR="${REPO_DIR}/data/logs"
mkdir -p "${HAR_DIR}" "${LOG_DIR}"

LOG_FILE="${LOG_DIR}/har-price-sync.log"
TIMESTAMP="$(date '+%Y-%m-%d %H:%M:%S')"

echo "[${TIMESTAMP}] Starting offline HAR price sync..." >> "${LOG_FILE}"

# Check whether any .har file exists in data/har/
shopt -s nullglob
HAR_FILES=("${HAR_DIR}"/*.har)
if [ ${#HAR_FILES[@]} -eq 0 ]; then
  echo "[${TIMESTAMP}] No .har files in ${HAR_DIR}. Sending reminder." >> "${LOG_FILE}"
  if command -v osascript >/dev/null 2>&1; then
    osascript -e 'display notification "ยังไม่มีไฟล์ .har ใหม่ใน data/har/ สำหรับรอบอัปเดตราคาเดือนนี้" with title "Bangkok Commute Map"' || true
  fi
  exit 0
fi

# Check if newest .har is older than 30 days (monthly freshness reminder)
NEWEST_AGE_DAYS="$(python3 -c '
import os, glob, time
files = glob.glob("'"${HAR_DIR}"'/*.har")
newest = max(os.path.getmtime(f) for f in files)
print(int((time.time() - newest) / 86400))
')"

cd "${REPO_DIR}"
python3 scripts/import_har_prices.py \
  --input "${HAR_DIR}" \
  --output backend/data/har_price_benchmarks.json \
  --preview-listings-diff >> "${LOG_FILE}" 2>&1

if [ "${APPLY_TO_LISTINGS:-0}" = "1" ]; then
  python3 scripts/import_har_prices.py \
    --input "${HAR_DIR}" \
    --output backend/data/har_price_benchmarks.json \
    --apply-to-listings >> "${LOG_FILE}" 2>&1
  (cd backend && go run ./cmd/seed -validate-only) >> "${LOG_FILE}" 2>&1
  if [ "${RESEED_DB:-0}" = "1" ]; then
    (cd backend && go run ./cmd/seed) >> "${LOG_FILE}" 2>&1
  fi
fi

if [ "${NEWEST_AGE_DAYS}" -ge 30 ] && command -v osascript >/dev/null 2>&1; then
  osascript -e "display notification \"ไฟล์ HAR ล่าสุดเก่ากว่า ${NEWEST_AGE_DAYS} วันแล้ว ถึงรอบเก็บไฟล์ราคาใหม่ครับ\" with title \"Bangkok Commute Map\"" || true
elif command -v osascript >/dev/null 2>&1; then
  osascript -e 'display notification "อัปเดตสถิติราคาจาก HAR เสร็จเรียบร้อย (backend/data/har_price_benchmarks.json)" with title "Bangkok Commute Map"' || true
fi

echo "[${TIMESTAMP}] Done (newest HAR age: ${NEWEST_AGE_DAYS}d)." >> "${LOG_FILE}"
