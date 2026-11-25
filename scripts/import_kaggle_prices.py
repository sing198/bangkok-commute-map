#!/usr/bin/env python3
"""
Kaggle / Open CSV Bangkok Condo Rental Importer for Bangkok Commute Map.

Reads local Bangkok condo/apartment rental CSV datasets (e.g. downloaded from Kaggle
such as "Condominium Data in Bangkok" or "Thailand Housing" into data/kaggle/),
auto-detects common column names, filters strictly for monthly rent observations,
matches existing buildings in backend/data/listings.json using strict geographic +
name similarity rules (zero cross-branch false positives), and writes
backend/data/kaggle_price_benchmarks.json.

Optional: pass --kaggle-dataset <owner/dataset> to download directly via the
`kaggle` CLI when ~/.kaggle/kaggle.json is configured.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import math
import re
import shutil
import subprocess
import sys
from collections import defaultdict
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable

MIN_RENT_THB = 2_000
MAX_RENT_THB = 250_000

NAME_COLS = (
    "project_name", "project", "condo_name", "condo", "building_name", "building",
    "property_name", "title", "name", "listing_name", "name_en", "name_th",
)
RENT_COLS = (
    "monthly_rent_thb", "monthly_rent", "rent_price", "rent_thb", "rent",
    "price_rent", "price_per_month", "rental_price", "price_thb", "price",
)
TYPE_COLS = ("listing_type", "intent", "deal_type", "offer_type", "transaction_type", "type")
AREA_COLS = ("floor_area", "room_size_sqm", "room_size", "area_sqm", "usable_area", "size_sqm", "area", "size", "sqm")
LAT_COLS = ("lat", "latitude")
LNG_COLS = ("lng", "lon", "long", "longitude")
STATION_COLS = ("station_id", "nearest_station", "station", "bts", "mrt", "bts_station", "mrt_station")


@dataclass
class CsvObs:
    project_name: str
    rent_thb: int
    area_sqm: float | None
    lat: float | None
    lng: float | None
    station_hint: str
    source_file: str


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
    return max(2_500, int(round(val / 500.0) * 500))


def to_float(raw: Any) -> float | None:
    if raw is None:
        return None
    s = re.sub(r"[^\d.\-]", "", str(raw).strip())
    if not s or s in (".", "-", "-."):
        return None
    try:
        return float(s)
    except ValueError:
        return None


def pick_column(headers: list[str], candidates: tuple[str, ...]) -> str | None:
    lower_map = {h.strip().lower(): h for h in headers}
    for cand in candidates:
        if cand in lower_map:
            return lower_map[cand]
    for cand in candidates:
        for low, orig in lower_map.items():
            if cand in low:
                return orig
    return None


def normalize_building_name(name: str) -> str:
    s = (name or "").lower()
    s = re.sub(r",\s*(กรุงเทพ|bangkok).*$", "", s)
    s = re.sub(r"[^a-z0-9\u0e00-\u0e7f]+", " ", s)
    stop = {
        "condo", "condominium", "คอนโด", "คอนโดมิเนียม", "residence", "apartment",
        "อพาร์ทเม้นท์", "อพาร์ตเมนต์", "the", "at", "bangkok", "กรุงเทพ", "for", "rent", "ให้เช่า",
    }
    return " ".join(t for t in s.split() if t and t not in stop)


def building_matches_project(row: dict[str, Any], proj: dict[str, Any]) -> bool:
    plat, plng = proj.get("lat"), proj.get("lng")
    rlat, rlng = row.get("lat"), row.get("lng")
    dist_m: float | None = None
    if plat is not None and plng is not None and rlat is not None and rlng is not None:
        dist_m = haversine_m(rlat, rlng, plat, plng)
        if dist_m > 500.0:
            return False

    proj_parts = [p.strip() for p in re.split(r"[:|/\-–—]", proj.get("project_name", "")) if p.strip()]
    proj_norms = [normalize_building_name(proj.get("project_name", ""))] + [
        normalize_building_name(p) for p in proj_parts
    ]
    proj_norms = [n for n in proj_norms if len(n) >= 4]

    row_norms = [
        normalize_building_name(row.get("title_th", "")),
        normalize_building_name(row.get("title_en", "")),
    ]
    row_norms = [n for n in row_norms if len(n) >= 4]

    for rn in row_norms:
        for pn in proj_norms:
            if rn == pn:
                return True
            r_toks, p_toks = set(rn.split()), set(pn.split())
            if len(r_toks) >= 2 and len(p_toks) >= 2:
                jaccard = len(r_toks & p_toks) / len(r_toks | p_toks)
                if jaccard >= 0.75:
                    return True

    if dist_m is not None and dist_m <= 90.0:
        r_all = set(" ".join(row_norms).split())
        p_all = set(" ".join(proj_norms).split())
        if r_all & p_all:
            return True

    return False


def download_kaggle_dataset(slug: str, dest_dir: Path) -> None:
    if not shutil.which("kaggle"):
        raise RuntimeError(
            "The `kaggle` CLI is not installed. Install via `pip install kaggle` and place your API token at ~/.kaggle/kaggle.json, "
            "or manually download CSV files into data/kaggle/."
        )
    dest_dir.mkdir(parents=True, exist_ok=True)
    cmd = ["kaggle", "datasets", "download", "-d", slug, "-p", str(dest_dir), "--unzip"]
    subprocess.run(cmd, check=True)


def collect_csv_files(inputs: Iterable[str]) -> list[Path]:
    found: list[Path] = []
    for raw in inputs:
        p = Path(raw)
        if p.is_file() and p.suffix.lower() == ".csv":
            found.append(p)
        elif p.is_dir():
            found.extend(sorted(p.rglob("*.csv")))
    seen: set[Path] = set()
    out: list[Path] = []
    for p in found:
        rp = p.resolve()
        if rp not in seen:
            seen.add(rp)
            out.append(p)
    return out


def parse_csv_file(csv_path: Path) -> tuple[list[CsvObs], dict[str, Any]]:
    raw_bytes = csv_path.read_bytes()
    sha256 = hashlib.sha256(raw_bytes).hexdigest()
    text = raw_bytes.decode("utf-8-sig", errors="replace")
    reader = csv.DictReader(text.splitlines())
    headers = reader.fieldnames or []

    name_col = pick_column(headers, NAME_COLS)
    rent_col = pick_column(headers, RENT_COLS)
    type_col = pick_column(headers, TYPE_COLS)
    area_col = pick_column(headers, AREA_COLS)
    lat_col = pick_column(headers, LAT_COLS)
    lng_col = pick_column(headers, LNG_COLS)
    station_col = pick_column(headers, STATION_COLS)

    if not name_col or not rent_col:
        raise ValueError(
            f"{csv_path.name}: could not detect project name ({name_col}) or rent ({rent_col}) columns from {headers}"
        )

    obs_list: list[CsvObs] = []
    total_rows = 0
    for row in reader:
        total_rows += 1
        if type_col:
            tval = (row.get(type_col) or "").strip().lower()
            if tval and any(k in tval for k in ("sale", "sell", "buy", "ขาย")) and not any(
                k in tval for k in ("rent", "เช่า")
            ):
                continue

        name = (row.get(name_col) or "").strip()
        rent_f = to_float(row.get(rent_col))
        if not name or rent_f is None:
            continue
        rent = int(round(rent_f))
        if not (MIN_RENT_THB <= rent <= MAX_RENT_THB):
            continue

        area = to_float(row.get(area_col)) if area_col else None
        if area is not None and not (10.0 <= area <= 600.0):
            area = None
        lat = to_float(row.get(lat_col)) if lat_col else None
        lng = to_float(row.get(lng_col)) if lng_col else None
        if lat is not None and not (13.3 <= lat <= 14.3):
            lat, lng = None, None
        if lng is not None and not (100.2 <= lng <= 101.0):
            lat, lng = None, None

        obs_list.append(
            CsvObs(
                project_name=name,
                rent_thb=rent,
                area_sqm=area,
                lat=lat,
                lng=lng,
                station_hint=(row.get(station_col) or "").strip() if station_col else "",
                source_file=csv_path.name,
            )
        )

    meta = {
        "file": csv_path.name,
        "sha256": sha256,
        "rows": total_rows,
        "valid_rent_rows": len(obs_list),
        "detected_columns": {
            "name": name_col,
            "rent": rent_col,
            "type": type_col,
            "area": area_col,
            "lat": lat_col,
            "lng": lng_col,
            "station": station_col,
        },
    }
    return obs_list, meta


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Import Bangkok condo monthly rent CSV datasets (e.g. from Kaggle) into Bangkok Commute Map."
    )
    parser.add_argument("--input", action="append", help="CSV file or folder (default: data/kaggle)")
    parser.add_argument("--kaggle-dataset", help="Optional Kaggle dataset slug to download via `kaggle` CLI into data/kaggle")
    parser.add_argument("--listings", type=Path, default=Path("backend/data/listings.json"))
    parser.add_argument("--output", type=Path, default=Path("backend/data/kaggle_price_benchmarks.json"))
    parser.add_argument("--min-samples", type=int, default=1)
    parser.add_argument("--preview-listings-diff", action="store_true")
    parser.add_argument("--apply-to-listings", action="store_true")
    args = parser.parse_args()

    if args.kaggle_dataset:
        download_kaggle_dataset(args.kaggle_dataset, Path("data/kaggle"))

    inputs = args.input or ["data/kaggle"]
    csv_files = collect_csv_files(inputs)
    if not csv_files:
        print(
            f"No CSV files found in {inputs}. Download a Bangkok condo rent CSV from Kaggle into data/kaggle/ or pass --input <file.csv>.",
            file=sys.stderr,
        )
        return 1

    all_obs: list[CsvObs] = []
    file_metas: list[dict[str, Any]] = []
    for cf in csv_files:
        obs, meta = parse_csv_file(cf)
        all_obs.extend(obs)
        file_metas.append(meta)

    by_project: dict[str, list[CsvObs]] = defaultdict(list)
    for o in all_obs:
        key = normalize_building_name(o.project_name) or o.project_name.lower()
        by_project[key].append(o)

    project_summaries: list[dict[str, Any]] = []
    for key in sorted(by_project.keys()):
        items = by_project[key]
        rents = sorted(float(x.rent_thb) for x in items)
        psqms = sorted(x.rent_thb / x.area_sqm for x in items if x.area_sqm)
        lat = next((x.lat for x in items if x.lat is not None), None)
        lng = next((x.lng for x in items if x.lng is not None), None)
        project_summaries.append(
            {
                "project_key": key,
                "project_name": items[0].project_name,
                "sample_count": len(items),
                "min_rent_thb": int(rents[0]),
                "p25_rent_thb": int(round(percentile(rents, 25))),
                "median_rent_thb": int(round(percentile(rents, 50))),
                "p75_rent_thb": int(round(percentile(rents, 75))),
                "max_rent_thb": int(rents[-1]),
                "median_price_per_sqm": round(percentile(psqms, 50), 2) if psqms else None,
                "lat": lat,
                "lng": lng,
            }
        )

    now_utc = datetime.now(timezone.utc)
    month_label = now_utc.strftime("%Y-%m")
    report = {
        "meta": {
            "generated_at": now_utc.isoformat(),
            "month": month_label,
            "source_files": file_metas,
            "valid_rent_rows": len(all_obs),
            "unique_projects": len(project_summaries),
        },
        "projects": project_summaries,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

    print(
        f"Processed {len(csv_files)} CSV file(s): {len(all_obs)} valid rent rows -> "
        f"{len(project_summaries)} unique project(s). Wrote {args.output}"
    )

    if args.preview_listings_diff or args.apply_to_listings:
        dry_run = not args.apply_to_listings
        listings: list[dict[str, Any]] = json.loads(args.listings.read_text(encoding="utf-8"))
        eligible = [p for p in project_summaries if p["sample_count"] >= args.min_samples]
        changes = []
        for row in listings:
            # Do not overwrite newer HAR building-level entries with older Kaggle snapshots
            if (row.get("price_source") or "").startswith("ราคาอ้างอิงจาก HAR"):
                continue
            matched = next((p for p in eligible if building_matches_project(row, p)), None)
            if not matched:
                continue
            new_med = round_to_500(matched["median_rent_thb"])
            new_min = min(new_med, round_to_500(matched["p25_rent_thb"]))
            new_max = max(new_med, round_to_500(matched["p75_rent_thb"]))
            changes.append(
                {
                    "id": row["id"],
                    "title": row.get("title_th") or row.get("title_en"),
                    "matched_project": matched["project_name"],
                    "samples": matched["sample_count"],
                    "old_rent": row.get("monthly_rent_thb"),
                    "new_rent": new_med,
                    "range": [new_min, new_max],
                }
            )
            if not dry_run:
                row["monthly_rent_thb"] = new_med
                row["estimated_rent_min_thb"] = new_min
                row["estimated_rent_max_thb"] = new_max
                row["price_source"] = (
                    f"ฐานข้อมูลตลาด (Kaggle CSV · {matched['sample_count']} ประกาศ · {month_label})"
                )

        if not dry_run and changes:
            args.listings.write_text(
                json.dumps(listings, indent=2, ensure_ascii=False) + "\n", encoding="utf-8"
            )
        mode = "DRY-RUN" if dry_run else "APPLIED"
        print(f"[{mode}] Matched {len(changes)} listing(s) in {args.listings}:")
        for c in changes[:20]:
            print(
                f"  {c['id']} {c['title']} <- {c['matched_project']} (n={c['samples']}): "
                f"฿{c['old_rent']:,} -> ฿{c['new_rent']:,} (฿{c['range'][0]:,}-฿{c['range'][1]:,})"
            )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
