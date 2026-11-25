#!/usr/bin/env python3
"""
Fetch Inside Airbnb Bangkok Open Data (CC0 1.0 Public Domain) and compute
station-level monthly rental benchmarks for Bangkok Commute Map.

Auto-discovers the latest Bangkok snapshot from https://insideairbnb.com/get-the-data/,
filters for residential Entire home/apt listings (prioritizing minimum_nights >= 28
monthly stays per Thai rental law), maps each listing to the nearest BTS/MRT/ARL/SRT
station in backend/data/stations.json, and writes backend/data/station_rent_benchmarks.json.
"""

from __future__ import annotations

import argparse
import csv
import gzip
import io
import json
import math
import re
import shutil
import ssl
import subprocess
import sys
import urllib.request
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

INSIDE_AIRBNB_INDEX = "https://insideairbnb.com/get-the-data/"
BKK_CSV_RE = re.compile(
    r"https://data\.insideairbnb\.com/thailand/central-thailand/bangkok/(\d{4}-\d{2}-\d{2})/(visualisations/listings\.csv|data/listings\.csv\.gz)"
)
USER_AGENT = "bangkok-commute-map-open-data-sync/1.0 (+https://github.com/sing198/bangkok-commute-map)"

MIN_MONTHLY_THB = 4_000
MAX_MONTHLY_THB = 250_000


def haversine_m(lat1: float, lng1: float, lat2: float, lng2: float) -> float:
    r = 6_371_000.0
    p1, p2 = math.radians(lat1), math.radians(lat2)
    dp = math.radians(lat2 - lat1)
    dl = math.radians(lng2 - lng1)
    a = math.sin(dp / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 2 * r * math.atan2(math.sqrt(a), math.sqrt(1 - a))


def percentile(sorted_vals: list[float], p: float) -> float:
    if not sorted_vals:
        raise ValueError("empty list")
    if len(sorted_vals) == 1:
        return float(sorted_vals[0])
    k = (len(sorted_vals) - 1) * (p / 100.0)
    f = math.floor(k)
    c = math.ceil(k)
    if f == c:
        return float(sorted_vals[int(k)])
    return float(sorted_vals[f] * (c - k) + sorted_vals[c] * (k - f))


def round_to_500(val: float) -> int:
    return max(3_000, int(round(val / 500.0) * 500))


def parse_price_thb(raw: str | None) -> float | None:
    if not raw:
        return None
    cleaned = re.sub(r"[^\d.]", "", raw.strip())
    if not cleaned:
        return None
    try:
        val = float(cleaned)
        return val if val > 0 else None
    except ValueError:
        return None


def fetch_bytes(url: str, timeout: int = 45) -> bytes:
    # Prefer certifi CA bundle if available; on python.org macOS builds without cert bundle,
    # fall back to system curl (which verifies TLS against macOS Keychain / system CAs).
    ctx = None
    try:
        import certifi  # type: ignore

        ctx = ssl.create_default_context(cafile=certifi.where())
    except ImportError:
        ctx = ssl.create_default_context()

    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    try:
        with urllib.request.urlopen(req, timeout=timeout, context=ctx) as resp:
            return resp.read()
    except urllib.error.URLError as exc:
        if isinstance(exc.reason, ssl.SSLCertVerificationError) and shutil.which("curl"):
            proc = subprocess.run(
                ["curl", "-fsSL", "--max-time", str(timeout), "-A", USER_AGENT, url],
                check=True,
                capture_output=True,
            )
            return proc.stdout
        raise


def discover_latest_bangkok_url() -> tuple[str, str]:
    html = fetch_bytes(INSIDE_AIRBNB_INDEX, timeout=30).decode("utf-8", errors="replace")
    matches = BKK_CSV_RE.findall(html)
    if not matches:
        raise RuntimeError(
            f"Could not locate Bangkok listings.csv link on {INSIDE_AIRBNB_INDEX}"
        )

    # Prefer latest snapshot date, and prefer lightweight visualisations/listings.csv over .gz
    by_date: dict[str, dict[str, str]] = defaultdict(dict)
    for snap_date, suffix in matches:
        full_url = f"https://data.insideairbnb.com/thailand/central-thailand/bangkok/{snap_date}/{suffix}"
        by_date[snap_date][suffix] = full_url

    latest_date = sorted(by_date.keys())[-1]
    candidates = by_date[latest_date]
    if "visualisations/listings.csv" in candidates:
        return candidates["visualisations/listings.csv"], latest_date
    return next(iter(candidates.values())), latest_date


def load_csv_rows(csv_file: Path | None, url: str | None) -> tuple[list[dict[str, str]], str, str]:
    if csv_file is not None:
        raw = csv_file.read_bytes()
        if csv_file.suffix == ".gz":
            text = gzip.decompress(raw).decode("utf-8", errors="replace")
        else:
            text = raw.decode("utf-8", errors="replace")
        m = re.search(r"(\d{4}-\d{2}-\d{2})", csv_file.name)
        snap_date = m.group(1) if m else datetime.now(timezone.utc).strftime("%Y-%m-%d")
        return list(csv.DictReader(io.StringIO(text))), str(csv_file), snap_date

    if url:
        m = re.search(r"(\d{4}-\d{2}-\d{2})", url)
        snap_date = m.group(1) if m else datetime.now(timezone.utc).strftime("%Y-%m-%d")
        target_url = url
    else:
        target_url, snap_date = discover_latest_bangkok_url()

    raw = fetch_bytes(target_url, timeout=60)
    if target_url.endswith(".gz"):
        text = gzip.decompress(raw).decode("utf-8", errors="replace")
    else:
        text = raw.decode("utf-8", errors="replace")
    return list(csv.DictReader(io.StringIO(text))), target_url, snap_date


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Build Bangkok rail station monthly rent benchmarks from Inside Airbnb Open Data."
    )
    parser.add_argument("--url", help="Explicit Inside Airbnb listings.csv(.gz) URL (auto-discovered if omitted)")
    parser.add_argument("--csv-file", type=Path, help="Local listings.csv(.gz) file for offline processing")
    parser.add_argument("--stations", type=Path, default=Path("backend/data/stations.json"))
    parser.add_argument("--output", type=Path, default=Path("backend/data/station_rent_benchmarks.json"))
    parser.add_argument("--max-dist-m", type=float, default=1500.0, help="Max radius (m) around a station")
    parser.add_argument("--min-samples", type=int, default=3, help="Min listings required to publish a station benchmark")
    args = parser.parse_args()

    stations: list[dict[str, Any]] = json.loads(args.stations.read_text(encoding="utf-8"))
    rows, source_url, snapshot_date = load_csv_rows(args.csv_file, args.url)

    # Bucket monthly rents (price * 30) per nearest station:
    # Strictly use Entire home/apt listings with minimum_nights >= 28 (monthly residential stays
    # under Thai law) so nightly tourist listings * 30 never inflate suburban station benchmarks.
    monthly28_by_station: dict[str, list[float]] = defaultdict(list)

    total_rows = len(rows)
    valid_entire = 0
    valid_monthly28 = 0

    for row in rows:
        room_type = (row.get("room_type") or "").strip()
        if room_type != "Entire home/apt":
            continue

        nightly = parse_price_thb(row.get("price"))
        if nightly is None:
            continue
        monthly = nightly * 30.0
        if not (MIN_MONTHLY_THB <= monthly <= MAX_MONTHLY_THB):
            continue

        try:
            lat = float(row.get("latitude") or "")
            lng = float(row.get("longitude") or "")
        except ValueError:
            continue

        try:
            min_nights = int(float(row.get("minimum_nights") or "1"))
        except ValueError:
            min_nights = 1

        # Find nearest transit station within max_dist_m
        best_sid = None
        best_dist = float("inf")
        for s in stations:
            d = haversine_m(lat, lng, s["lat"], s["lng"])
            if d < best_dist:
                best_dist = d
                best_sid = s["id"]
        if best_sid is None or best_dist > args.max_dist_m:
            continue

        valid_entire += 1
        if min_nights >= 28:
            valid_monthly28 += 1
            monthly28_by_station[best_sid].append(monthly)

    station_entries: list[dict[str, Any]] = []
    basis_counts: dict[str, int] = defaultdict(int)

    for s in sorted(stations, key=lambda x: x["id"]):
        sid = s["id"]
        vals_28 = sorted(monthly28_by_station.get(sid, []))
        if len(vals_28) < args.min_samples:
            continue
        chosen = vals_28
        basis = "monthly_28plus"

        basis_counts[basis] += 1
        med = round_to_500(percentile(chosen, 50))
        p25 = min(med, round_to_500(percentile(chosen, 25)))
        p75 = max(med, round_to_500(percentile(chosen, 75)))

        station_entries.append(
            {
                "station_id": sid,
                "name_th": s["name_th"],
                "sample_count": len(chosen),
                "p25_monthly_thb": p25,
                "median_monthly_thb": med,
                "p75_monthly_thb": p75,
                "basis": basis,
                "snapshot_date": snapshot_date,
            }
        )

    payload = {
        "meta": {
            "source": "Inside Airbnb (Bangkok)",
            "license": "CC0 1.0 Public Domain",
            "snapshot_date": snapshot_date,
            "source_url": source_url,
            "generated_at": datetime.now(timezone.utc).isoformat(),
            "max_station_radius_m": int(args.max_dist_m),
            "total_csv_rows": total_rows,
            "matched_entire_home_rows": valid_entire,
            "matched_monthly_28plus_rows": valid_monthly28,
            "covered_stations": len(station_entries),
            "total_stations": len(stations),
            "basis_breakdown": dict(basis_counts),
        },
        "stations": station_entries,
    }

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(
        json.dumps(payload, indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )

    print(
        f"Inside Airbnb Bangkok ({snapshot_date}): {total_rows:,} rows -> "
        f"{valid_monthly28:,} monthly (>=28 nights) / {valid_entire:,} entire-home matched -> "
        f"{len(station_entries)}/{len(stations)} stations covered"
    )
    print(f"Basis breakdown: {dict(basis_counts)}")
    for sid in ("BTS_E4", "BTS_CEN", "BTS_S2", "MRT_BL20", "BTS_N5", "BTS_N18"):
        st = next((x for x in station_entries if x["station_id"] == sid), None)
        if st:
            print(
                f"  {st['station_id']} ({st['name_th']}): n={st['sample_count']} [{st['basis']}] "
                f"median=฿{st['median_monthly_thb']:,} (P25 ฿{st['p25_monthly_thb']:,} - P75 ฿{st['p75_monthly_thb']:,})"
            )
    print(f"Wrote {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
