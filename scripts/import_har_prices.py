#!/usr/bin/env python3
"""
Offline HAR -> Station/Project Rental Price Benchmark Pipeline for Bangkok Commute Map.

Reads local browser-exported HAR files (e.g. saved manually from DDproperty),
extracts listing rental prices without touching cookies/auth headers, deduplicates
by listing ID, maps listings to the nearest Bangkok rail station from
backend/data/stations.json, computes robust statistics (median, P25, P75,
THB/sqm), and optionally recalibrates matching station estimates in
backend/data/listings.json.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import math
import re
import sys
import urllib.parse
from collections import defaultdict
from dataclasses import dataclass, asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable

NEXT_DATA_RE = re.compile(
    r'<script[^>]*id="__NEXT_DATA__"[^>]*>(.*?)</script>', re.DOTALL | re.IGNORECASE
)

MIN_RENT_THB = 2_000
MAX_RENT_THB = 300_000


@dataclass
class ListingObs:
    listing_id: str
    intent: str
    price_thb: int
    floor_area_sqm: float | None
    bedrooms: int | None
    bathrooms: int | None
    property_type: str
    project_id: str | None
    project_name: str
    lat: float | None
    lng: float | None
    station_code_hint: str | None
    referer_station_code: str | None
    source_kind: str
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


def decode_har_content(content: dict[str, Any]) -> str:
    text = content.get("text") or ""
    if not text:
        return ""
    if content.get("encoding") == "base64":
        try:
            return base64.b64decode(text).decode("utf-8", errors="replace")
        except Exception:
            return ""
    return text


def normalize_property_type(raw: str | None) -> str:
    if not raw:
        return "condo"
    s = raw.strip().lower()
    if any(k in s for k in ("condo", "คอนโด")):
        return "condo"
    if any(k in s for k in ("apart", "อพาร์ท", "หอพัก", "mansion", "แมนชั่น")):
        return "apartment"
    if any(k in s for k in ("house", "บ้าน", "town", "ทาวน์", "villa")):
        return "house"
    return s


def normalize_intent(raw: str | None) -> str:
    if not raw:
        return ""
    s = raw.strip().upper()
    if "RENT" in s or "เช่า" in s:
        return "RENT"
    if "SALE" in s or "BUY" in s or "ขาย" in s:
        return "SALE"
    return s


def to_int(val: Any) -> int | None:
    if val is None or isinstance(val, bool):
        return None
    if isinstance(val, (int, float)):
        return int(round(val))
    if isinstance(val, str):
        digits = re.sub(r"[^\d.]", "", val)
        if digits:
            try:
                return int(round(float(digits)))
            except ValueError:
                return None
    return None


def to_float(val: Any) -> float | None:
    if val is None or isinstance(val, bool):
        return None
    if isinstance(val, (int, float)):
        return float(val)
    if isinstance(val, str):
        digits = re.sub(r"[^\d.]", "", val)
        if digits:
            try:
                return float(digits)
            except ValueError:
                return None
    return None


def extract_mrt_code_from_url(url: str) -> str | None:
    if not url:
        return None
    parsed = urllib.parse.urlparse(url)
    qs = urllib.parse.parse_qs(parsed.query)
    for key in ("mrtStations", "btsStations", "station", "stationCode"):
        vals = qs.get(key)
        if vals and vals[0]:
            return vals[0].split(",")[0].strip().upper()
    return None


def get_referer_header(entry: dict[str, Any]) -> str:
    for h in entry.get("request", {}).get("headers", []):
        if (h.get("name") or "").lower() == "referer":
            return h.get("value") or ""
    return ""


def extract_from_card_item(
    item: dict[str, Any],
    source_kind: str,
    source_file: str,
    referer_station_code: str | None,
) -> ListingObs | None:
    seg = (
        item.get("context", {})
        .get("segmentData", {})
        .get("listingData", {})
    )
    if not isinstance(seg, dict) or not seg:
        return None

    lid = seg.get("listingId") or item.get("id")
    price = to_int(seg.get("price"))
    if not lid or not price or price <= 0:
        return None

    intent = normalize_intent(
        seg.get("intentType") or item.get("context", {}).get("data", {}).get("variant")
    )
    proj_id = seg.get("projectId")
    proj_name = (
        seg.get("listingTitle")
        or item.get("title")
        or ""
    )
    return ListingObs(
        listing_id=str(lid),
        intent=intent,
        price_thb=price,
        floor_area_sqm=to_float(seg.get("floorArea")),
        bedrooms=to_int(seg.get("bedroom")),
        bathrooms=to_int(seg.get("bathroom")),
        property_type=normalize_property_type(seg.get("propertyType")),
        project_id=str(proj_id) if proj_id is not None else None,
        project_name=str(proj_name).strip(),
        lat=None,
        lng=None,
        station_code_hint=None,
        referer_station_code=referer_station_code,
        source_kind=source_kind,
        source_file=source_file,
    )


def extract_srp_listings_recursive(
    obj: Any,
    source_file: str,
    referer_station_code: str | None,
    out: list[ListingObs],
    depth: int = 0,
) -> None:
    """Best-effort extractor for DDproperty Search Results Pages (SRP) __NEXT_DATA__."""
    if depth > 8:
        return
    if isinstance(obj, list):
        for elem in obj:
            if isinstance(elem, dict):
                # SRP cards may have listingData nested or flat fields
                ld = elem.get("listingData") if isinstance(elem.get("listingData"), dict) else elem
                lid = ld.get("id") or ld.get("listingId")
                price_raw = ld.get("price")
                if isinstance(price_raw, dict):
                    price_raw = price_raw.get("value") or price_raw.get("amount")
                price = to_int(price_raw)
                if lid and price and price > 0 and (
                    "floorArea" in ld or "bedrooms" in ld or "propertyType" in ld or "listingType" in ld
                ):
                    loc = ld.get("location") if isinstance(ld.get("location"), dict) else {}
                    lat = to_float(ld.get("latitude") or ld.get("lat") or loc.get("latitude") or loc.get("lat"))
                    lng = to_float(ld.get("longitude") or ld.get("lng") or loc.get("longitude") or loc.get("lng"))
                    intent = normalize_intent(
                        ld.get("listingType") or ld.get("intentType") or ld.get("listingTypeText")
                    )
                    proj_id = ld.get("projectId") or ld.get("propertyLegacyId")
                    out.append(
                        ListingObs(
                            listing_id=str(lid),
                            intent=intent,
                            price_thb=price,
                            floor_area_sqm=to_float(ld.get("floorArea")),
                            bedrooms=to_int(ld.get("bedrooms") if "bedrooms" in ld else ld.get("bedroom")),
                            bathrooms=to_int(ld.get("bathrooms") if "bathrooms" in ld else ld.get("bathroom")),
                            property_type=normalize_property_type(
                                ld.get("propertyTypeCode") or ld.get("propertyType")
                            ),
                            project_id=str(proj_id) if proj_id is not None else None,
                            project_name=str(ld.get("propertyName") or ld.get("localizedTitle") or ld.get("title") or "").strip(),
                            lat=lat,
                            lng=lng,
                            station_code_hint=None,
                            referer_station_code=referer_station_code,
                            source_kind="srp_next_data",
                            source_file=source_file,
                        )
                    )
                else:
                    extract_srp_listings_recursive(elem, source_file, referer_station_code, out, depth + 1)
    elif isinstance(obj, dict):
        for k, v in obj.items():
            if k in ("trackingConfig", "globalConfig", "pgBasePageLayoutData", "seoInternalLinksData"):
                continue
            extract_srp_listings_recursive(v, source_file, referer_station_code, out, depth + 1)


def extract_from_har(har_path: Path) -> tuple[list[ListingObs], dict[str, Any]]:
    raw_bytes = har_path.read_bytes()
    sha256 = hashlib.sha256(raw_bytes).hexdigest()
    har = json.loads(raw_bytes.decode("utf-8", errors="replace"))
    entries = har.get("log", {}).get("entries", [])

    observations: list[ListingObs] = []
    project_geo: dict[str, tuple[float | None, float | None, str | None]] = {}

    for entry in entries:
        req = entry.get("request", {})
        res = entry.get("response", {})
        url = req.get("url", "")
        parsed = urllib.parse.urlparse(url)
        if "ddproperty" not in parsed.netloc.lower():
            continue

        content = res.get("content", {})
        mime = (content.get("mimeType") or "").split(";")[0].strip().lower()
        body = decode_har_content(content)
        if not body:
            continue

        referer = get_referer_header(entry)
        referer_station = extract_mrt_code_from_url(url) or extract_mrt_code_from_url(referer)

        # 1) HTML SSR pages (__NEXT_DATA__)
        if "html" in mime:
            m = NEXT_DATA_RE.search(body)
            if not m:
                continue
            try:
                nd = json.loads(m.group(1))
            except json.JSONDecodeError:
                continue
            page = nd.get("page", "")
            page_data = (
                nd.get("props", {})
                .get("pageProps", {})
                .get("pageData", {})
                .get("data", {})
            )
            if not isinstance(page_data, dict):
                continue

            # LDP primary listing
            ld = page_data.get("listingData")
            if isinstance(ld, dict) and ld.get("listingId") and ld.get("price"):
                loc_data = page_data.get("listingLocationData", {}).get("data", {})
                center = loc_data.get("center") or {}
                lat = to_float(center.get("lat"))
                lng = to_float(center.get("lng"))
                nearest_mrts = loc_data.get("nearestMRTs") or []
                mrt_code = (
                    str(nearest_mrts[0].get("id")).strip().upper()
                    if nearest_mrts and isinstance(nearest_mrts[0], dict) and nearest_mrts[0].get("id")
                    else None
                )
                proj_id = ld.get("propertyLegacyId")
                if proj_id is not None:
                    project_geo[str(proj_id)] = (lat, lng, mrt_code)

                price = to_int(ld.get("price"))
                if price and price > 0:
                    observations.append(
                        ListingObs(
                            listing_id=str(ld["listingId"]),
                            intent=normalize_intent(ld.get("listingType") or ld.get("listingTypeText")),
                            price_thb=price,
                            floor_area_sqm=to_float(ld.get("floorArea")),
                            bedrooms=to_int(ld.get("bedrooms")),
                            bathrooms=to_int(ld.get("bathrooms")),
                            property_type=normalize_property_type(
                                ld.get("propertyTypeCode") or ld.get("propertyType")
                            ),
                            project_id=str(proj_id) if proj_id is not None else None,
                            project_name=str(ld.get("propertyName") or ld.get("localizedTitle") or "").strip(),
                            lat=lat,
                            lng=lng,
                            station_code_hint=mrt_code,
                            referer_station_code=referer_station,
                            source_kind="ldp_next_data",
                            source_file=har_path.name,
                        )
                    )
            elif "/srp/" in page or "listings" in str(page_data.keys()):
                extract_srp_listings_recursive(
                    page_data, har_path.name, referer_station, observations
                )

        # 2) JSON XHR API responses
        elif "json" in mime:
            try:
                payload = json.loads(body)
            except json.JSONDecodeError:
                continue

            if "/api/consumer/listings/other" in parsed.path and isinstance(payload, dict):
                for item in payload.get("items") or []:
                    if isinstance(item, dict):
                        obs = extract_from_card_item(
                            item, "api_listings_other", har_path.name, referer_station
                        )
                        if obs:
                            observations.append(obs)
            elif "/api/consumer/recommendation" in parsed.path and isinstance(payload, dict):
                for item in payload.get("data") or []:
                    if isinstance(item, dict):
                        obs = extract_from_card_item(
                            item, "api_recommendation", har_path.name, referer_station
                        )
                        if obs:
                            observations.append(obs)
            elif "/api/consumer/listings/map-listings" in parsed.path:
                extract_srp_listings_recursive(
                    payload, har_path.name, referer_station, observations
                )

    # Backfill project coordinates/station hints from LDP onto same-project API cards
    for obs in observations:
        if obs.project_id and obs.project_id in project_geo:
            plat, plng, pmrt = project_geo[obs.project_id]
            if obs.lat is None and plat is not None:
                obs.lat = plat
            if obs.lng is None and plng is not None:
                obs.lng = plng
            if obs.station_code_hint is None and pmrt is not None:
                obs.station_code_hint = pmrt

    meta = {
        "file": har_path.name,
        "sha256": sha256,
        "har_entries": len(entries),
        "raw_observations": len(observations),
    }
    return observations, meta


class StationIndex:
    def __init__(self, stations_path: Path):
        self.stations: list[dict[str, Any]] = json.loads(stations_path.read_text(encoding="utf-8"))
        self.by_id: dict[str, dict[str, Any]] = {s["id"]: s for s in self.stations}
        self.by_short_code: dict[str, dict[str, Any]] = {}
        for s in self.stations:
            parts = s["id"].split("_", 1)
            if len(parts) == 2:
                self.by_short_code[parts[1].upper()] = s

    def resolve(
        self, obs: ListingObs, max_dist_m: float
    ) -> tuple[str | None, str, int | None]:
        # 1. Coordinate-based nearest station
        if obs.lat is not None and obs.lng is not None:
            best_station = None
            best_dist = float("inf")
            for s in self.stations:
                d = haversine_m(obs.lat, obs.lng, s["lat"], s["lng"])
                if d < best_dist:
                    best_dist = d
                    best_station = s
            if best_station is not None and best_dist <= max_dist_m:
                return best_station["id"], "coordinates", int(round(best_dist))

        # 2. Explicit station code from nearestMRTs
        if obs.station_code_hint:
            st = self.by_short_code.get(obs.station_code_hint.upper())
            if st:
                return st["id"], "nearest_mrt_code", None

        # 3. Fallback: mrtStations param in URL/referer
        if obs.referer_station_code:
            st = self.by_short_code.get(obs.referer_station_code.upper())
            if st:
                return st["id"], "referer_mrt_param", None

        return None, "unmatched", None


def deduplicate(observations: list[ListingObs], target_intent: str) -> list[ListingObs]:
    best: dict[tuple[str, str], ListingObs] = {}

    def richness(o: ListingObs) -> tuple[int, int, int, int]:
        return (
            1 if (o.lat is not None and o.lng is not None) else 0,
            1 if o.station_code_hint else 0,
            1 if (o.floor_area_sqm and o.floor_area_sqm > 0) else 0,
            1 if o.source_kind == "ldp_next_data" else 0,
        )

    for obs in observations:
        intent = obs.intent or target_intent
        if target_intent and intent != target_intent:
            continue
        if intent == "RENT" and not (MIN_RENT_THB <= obs.price_thb <= MAX_RENT_THB):
            continue
        key = (obs.listing_id, intent)
        prev = best.get(key)
        if prev is None or richness(obs) > richness(prev):
            best[key] = obs
    return list(best.values())


def summarize_group(items: list[ListingObs]) -> dict[str, Any]:
    prices = sorted(float(x.price_thb) for x in items)
    psqms = sorted(
        x.price_thb / x.floor_area_sqm
        for x in items
        if x.floor_area_sqm and x.floor_area_sqm >= 10.0
    )
    areas = sorted(
        x.floor_area_sqm for x in items if x.floor_area_sqm and x.floor_area_sqm >= 10.0
    )
    return {
        "sample_count": len(items),
        "min_rent_thb": int(prices[0]),
        "p25_rent_thb": int(round(percentile(prices, 25))),
        "median_rent_thb": int(round(percentile(prices, 50))),
        "p75_rent_thb": int(round(percentile(prices, 75))),
        "max_rent_thb": int(prices[-1]),
        "median_price_per_sqm": round(percentile(psqms, 50), 2) if psqms else None,
        "median_floor_area_sqm": round(percentile(areas, 50), 2) if areas else None,
        "listing_ids": sorted(x.listing_id for x in items),
    }


def collect_har_files(inputs: Iterable[str]) -> list[Path]:
    found: list[Path] = []
    for raw in inputs:
        p = Path(raw)
        if p.is_file() and p.suffix.lower() == ".har":
            found.append(p)
        elif p.is_dir():
            found.extend(sorted(p.rglob("*.har")))
    # Deduplicate resolved paths
    seen: set[Path] = set()
    unique: list[Path] = []
    for p in found:
        rp = p.resolve()
        if rp not in seen:
            seen.add(rp)
            unique.append(p)
    return unique


def apply_benchmarks_to_listings(
    listings_path: Path,
    station_summaries: dict[str, dict[str, Any]],
    min_samples: int,
    month_label: str,
    dry_run: bool,
) -> list[dict[str, Any]]:
    listings: list[dict[str, Any]] = json.loads(listings_path.read_text(encoding="utf-8"))
    changes: list[dict[str, Any]] = []

    for row in listings:
        sid = row.get("nearest_station_id")
        ptype = normalize_property_type(row.get("property_type"))
        if not sid or sid not in station_summaries:
            continue
        bucket = station_summaries[sid].get("by_property_type", {}).get(ptype)
        if not bucket or bucket["sample_count"] < min_samples:
            continue

        med_psqm = bucket.get("median_price_per_sqm")
        room_sqm = to_float(row.get("room_size_sqm"))
        if med_psqm and room_sqm and room_sqm >= 12.0:
            new_med = round_to_500(med_psqm * room_sqm)
            spread_low = bucket["p25_rent_thb"] / max(1, bucket["median_rent_thb"])
            spread_high = bucket["p75_rent_thb"] / max(1, bucket["median_rent_thb"])
            new_min = min(new_med, round_to_500(new_med * min(spread_low, 0.90)))
            new_max = max(new_med, round_to_500(new_med * max(spread_high, 1.15)))
        else:
            new_med = round_to_500(bucket["median_rent_thb"])
            new_min = min(new_med, round_to_500(bucket["p25_rent_thb"]))
            new_max = max(new_med, round_to_500(bucket["p75_rent_thb"]))

        old_med = row.get("monthly_rent_thb")
        new_source = (
            f"ปรับเทียบจาก HAR สถานี {sid} ({bucket['sample_count']} ประกาศ · {month_label})"
        )
        changes.append(
            {
                "id": row["id"],
                "station_id": sid,
                "title": row.get("title_th") or row.get("title_en"),
                "old_rent": old_med,
                "new_rent": new_med,
                "new_range": [new_min, new_max],
            }
        )
        if not dry_run:
            row["monthly_rent_thb"] = new_med
            row["estimated_rent_min_thb"] = new_min
            row["estimated_rent_max_thb"] = new_max
            row["price_source"] = new_source

    if not dry_run and changes:
        listings_path.write_text(
            json.dumps(listings, indent=2, ensure_ascii=False) + "\n",
            encoding="utf-8",
        )
    return changes


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Extract and aggregate DDproperty HAR rental prices by Bangkok rail station."
    )
    parser.add_argument(
        "--input",
        action="append",
        help="Path to a .har file or directory of .har files (repeatable). Default: data/har",
    )
    parser.add_argument(
        "--stations",
        default="backend/data/stations.json",
        help="Path to stations.json",
    )
    parser.add_argument(
        "--listings",
        default="backend/data/listings.json",
        help="Path to listings.json (used with --apply-to-listings or --preview-listings-diff)",
    )
    parser.add_argument(
        "--output",
        default="backend/data/har_price_benchmarks.json",
        help="Output benchmark JSON path",
    )
    parser.add_argument(
        "--intent",
        default="RENT",
        choices=["RENT", "SALE"],
        help="Listing intent to include (default: RENT)",
    )
    parser.add_argument(
        "--max-station-dist-m",
        type=float,
        default=2500.0,
        help="Max Haversine distance (m) to associate a coordinate with a station",
    )
    parser.add_argument(
        "--min-samples",
        type=int,
        default=3,
        help="Minimum unique listings per station+type required to recalibrate listings.json",
    )
    parser.add_argument(
        "--preview-listings-diff",
        action="store_true",
        help="Preview how listings.json would be recalibrated without modifying it",
    )
    parser.add_argument(
        "--apply-to-listings",
        action="store_true",
        help="Apply station benchmarks to backend/data/listings.json",
    )
    args = parser.parse_args()

    inputs = args.input or ["data/har"]
    har_files = collect_har_files(inputs)
    if not har_files:
        print(
            f"No .har files found in {inputs}. Place exported .har files in data/har/ or pass --input <file.har>.",
            file=sys.stderr,
        )
        return 1

    station_index = StationIndex(Path(args.stations))
    all_obs: list[ListingObs] = []
    file_metas: list[dict[str, Any]] = []
    for hf in har_files:
        obs_list, meta = extract_from_har(hf)
        all_obs.extend(obs_list)
        file_metas.append(meta)

    unique_obs = deduplicate(all_obs, target_intent=args.intent)

    by_station: dict[str, list[ListingObs]] = defaultdict(list)
    by_station_type: dict[str, dict[str, list[ListingObs]]] = defaultdict(lambda: defaultdict(list))
    by_project: dict[str, list[ListingObs]] = defaultdict(list)
    match_methods: dict[str, int] = defaultdict(int)
    unmatched: list[dict[str, Any]] = []

    for obs in unique_obs:
        sid, method, dist_m = station_index.resolve(obs, args.max_station_dist_m)
        match_methods[method] += 1
        if sid:
            by_station[sid].append(obs)
            by_station_type[sid][obs.property_type].append(obs)
        else:
            unmatched.append(
                {
                    "listing_id": obs.listing_id,
                    "project_name": obs.project_name,
                    "price_thb": obs.price_thb,
                    "floor_area_sqm": obs.floor_area_sqm,
                }
            )
        if obs.project_id:
            by_project[obs.project_id].append(obs)

    station_summaries: dict[str, dict[str, Any]] = {}
    for sid in sorted(by_station.keys()):
        st = station_index.by_id[sid]
        entry = {
            "station_id": sid,
            "name_th": st["name_th"],
            "name_en": st["name_en"],
            "line_code": st["line_code"],
            **summarize_group(by_station[sid]),
            "by_property_type": {
                pt: summarize_group(items)
                for pt, items in sorted(by_station_type[sid].items())
            },
        }
        station_summaries[sid] = entry

    project_summaries = []
    for pid, items in sorted(by_project.items()):
        project_summaries.append(
            {
                "project_id": pid,
                "project_name": items[0].project_name,
                **summarize_group(items),
            }
        )

    now_utc = datetime.now(timezone.utc)
    month_label = now_utc.strftime("%Y-%m")
    output_payload = {
        "meta": {
            "generated_at": now_utc.isoformat(),
            "month": month_label,
            "intent": args.intent,
            "source_files": file_metas,
            "total_raw_observations": len(all_obs),
            "unique_listings": len(unique_obs),
            "matched_stations": len(station_summaries),
            "match_methods": dict(match_methods),
            "unmatched_count": len(unmatched),
        },
        "stations": list(station_summaries.values()),
        "projects": project_summaries,
        "unmatched": unmatched,
    }

    out_path = Path(args.output)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(
        json.dumps(output_payload, indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )

    print(
        f"Processed {len(har_files)} HAR file(s): "
        f"{len(all_obs)} raw -> {len(unique_obs)} unique {args.intent} listings -> "
        f"{len(station_summaries)} station(s)"
    )
    for st in output_payload["stations"]:
        print(
            f"  {st['station_id']} ({st['name_th']}): n={st['sample_count']} | "
            f"median=฿{st['median_rent_thb']:,} (P25=฿{st['p25_rent_thb']:,} - P75=฿{st['p75_rent_thb']:,}) | "
            f"฿{st['median_price_per_sqm']}/sqm"
        )
    print(f"Wrote benchmark report to {out_path}")

    if args.preview_listings_diff or args.apply_to_listings:
        dry_run = not args.apply_to_listings
        changes = apply_benchmarks_to_listings(
            listings_path=Path(args.listings),
            station_summaries=station_summaries,
            min_samples=args.min_samples,
            month_label=month_label,
            dry_run=dry_run,
        )
        mode = "DRY-RUN" if dry_run else "APPLIED"
        print(f"\n[{mode}] Recalibration changes for {args.listings} ({len(changes)} listing(s)):")
        for c in changes:
            print(
                f"  {c['id']} ({c['station_id']}) {c['title']}: "
                f"฿{c['old_rent']:,} -> ฿{c['new_rent']:,} (range ฿{c['new_range'][0]:,}-฿{c['new_range'][1]:,})"
            )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
