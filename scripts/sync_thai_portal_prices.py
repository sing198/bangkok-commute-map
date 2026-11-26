#!/usr/bin/env python3
"""
Sync real Thai monthly rental prices for Bangkok Commute Map from RentHub (apartments/mansions)
and PropertyHub (condos) via their public server-rendered __NEXT_DATA__ pages (robots.txt Allow: /).

Produces backend/data/portal_prices.json (embedded in the Go API binary) containing:
  1. `buildings`: exact building-level real monthly rental ranges (min / median / max)
     - Direct RentHub building pages (76+ linked apartments)
     - Direct PropertyHub project pages (402+ linked condos)
     - High-confidence same-station name matches discovered on RentHub/PropertyHub station pages
  2. `stations`: real Thai rental benchmarks per station, strictly separated by property_type:
     - `condo`: P25 / median / P75 from PropertyHub station pages
     - `apartment`: P25 / median / P75 from RentHub station pages

Polite fetching: honest User-Agent, bounded concurrency, per-request delay, clean stop on 429/403.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import math
import re
import subprocess
import sys
import threading
import time
import urllib.parse
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

USER_AGENT = "bangkok-commute-map-price-sync/1.0 (+https://github.com/sing198/bangkok-commute-map)"
NEXT_DATA_RE = re.compile(
    r'<script[^>]*id="__NEXT_DATA__"[^>]*>(.*?)</script>', re.DOTALL | re.IGNORECASE
)

MIN_APT_RENT = 1_500
MAX_APT_RENT = 120_000
MIN_CONDO_RENT = 3_500
MAX_CONDO_RENT = 400_000


def encode_url(raw_url: str) -> str:
    p = urllib.parse.urlsplit(raw_url.strip())
    path = urllib.parse.quote(urllib.parse.unquote(p.path), safe="/:@!$&'()*+,;=-._~")
    return urllib.parse.urlunsplit((p.scheme, p.netloc, path, p.query, p.fragment))


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


def round_to_100(val: float) -> int:
    return int(round(val / 100.0) * 100)


def round_to_500(val: float) -> int:
    return int(round(val / 500.0) * 500)


def normalize_name(name: str) -> str:
    s = (name or "").lower()
    s = re.sub(r"\([^)]*\)", " ", s)
    s = re.sub(r"[^a-z0-9\u0e00-\u0e7f]+", " ", s)
    stop = {
        "condo", "condominium", "คอนโด", "คอนโดมิเนียม", "apartment", "apartments",
        "อพาร์ทเม้นท์", "อพาร์ตเมนต์", "อพาร์ทเมนท์", "mansion", "แมนชั่น", "แมนชัน",
        "residence", "เรสซิเดนซ์", "place", "เพลส", "house", "เฮาส์", "หอพัก", "ห้องพัก",
        "ให้เช่า", "ใกล้", "bts", "mrt", "arl", "the", "at", "bangkok", "กรุงเทพ",
    }
    return " ".join(t for t in s.split() if t and t not in stop)


def names_match_strict(a_th: str, a_en: str, b_names: list[str]) -> bool:
    a_norms = [normalize_name(a_th), normalize_name(a_en)]
    a_norms = [n for n in a_norms if len(n) >= 4]
    if not a_norms:
        return False

    b_norms: list[str] = []
    for bn in b_names:
        if not bn:
            continue
        # Also split by separators/spaces prefix for noisy RentHub titles ("สิริมา แมนชั่น ห้วยขวาง - ...")
        parts = [bn] + re.split(r"[-–—|/:,]", bn)
        for p in parts[:3]:
            nn = normalize_name(p)
            if len(nn) >= 4:
                b_norms.append(nn)

    for an in a_norms:
        a_toks = set(an.split())
        for bn in b_norms:
            if an == bn:
                return True
            b_toks = set(bn.split())
            if len(a_toks) >= 2 and len(b_toks) >= 2:
                jaccard = len(a_toks & b_toks) / len(a_toks | b_toks)
                if jaccard >= 0.72:
                    return True
            # Exact Thai/EN distinctive name appearing as leading phrase of RentHub title
            if len(an) >= 7 and bn.startswith(an + " "):
                return True
    return False


class PoliteFetcher:
    def __init__(self, delay_s: float = 0.12, timeout_s: int = 18):
        self.delay_s = delay_s
        self.timeout_s = timeout_s
        self.rate_limited = threading.Event()
        self._lock = threading.Lock()
        self.ok_count = 0
        self.err_count = 0
        self.not_found_count = 0

    def fetch_next_data(self, raw_url: str) -> dict[str, Any] | None:
        if self.rate_limited.is_set() or not raw_url:
            return None
        if self.delay_s > 0:
            time.sleep(self.delay_s)

        url = encode_url(raw_url)
        for attempt in range(2):
            try:
                proc = subprocess.run(
                    [
                        "curl", "-sS", "-L",
                        "--max-time", str(self.timeout_s),
                        "-A", USER_AGENT,
                        "-w", "\n__HTTP_STATUS__:%{http_code}",
                        url,
                    ],
                    capture_output=True,
                    text=True,
                )
            except Exception:
                if attempt == 0:
                    time.sleep(0.5)
                    continue
                with self._lock:
                    self.err_count += 1
                return None

            out = proc.stdout
            marker = "\n__HTTP_STATUS__:"
            status = 0
            body = out
            if marker in out:
                body, st_str = out.rsplit(marker, 1)
                try:
                    status = int(st_str.strip())
                except ValueError:
                    status = 0

            if status == 0:
                if proc.returncode == 6:  # Could not resolve host (offline/sandbox)
                    self.rate_limited.set()
                    with self._lock:
                        self.err_count += 1
                    return None
            if status in (429, 403):
                self.rate_limited.set()
                with self._lock:
                    self.err_count += 1
                return None
            if status == 404:
                with self._lock:
                    self.not_found_count += 1
                return None
            if status != 200 or not body:
                if attempt == 0:
                    time.sleep(0.5)
                    continue
                with self._lock:
                    self.err_count += 1
                return None

            m = NEXT_DATA_RE.search(body)
            if not m:
                with self._lock:
                    self.err_count += 1
                return None
            try:
                nd = json.loads(m.group(1))
                with self._lock:
                    self.ok_count += 1
                return nd.get("props", {}).get("pageProps", {})
            except json.JSONDecodeError:
                with self._lock:
                    self.err_count += 1
                return None
        return None


def parse_renthub_monthly(price_obj: Any) -> tuple[int, int] | None:
    if not isinstance(price_obj, dict):
        return None
    monthly = price_obj.get("monthly")
    if not isinstance(monthly, dict):
        return None
    mn = monthly.get("minPrice") or 0
    mx = monthly.get("maxPrice") or 0
    try:
        mn_i = int(round(float(mn)))
        mx_i = int(round(float(mx)))
    except (TypeError, ValueError):
        return None
    if mn_i <= 0 and mx_i > 0:
        mn_i = mx_i
    if mx_i <= 0 and mn_i > 0:
        mx_i = mn_i
    if mn_i <= 0:
        return None
    if mx_i < mn_i:
        mn_i, mx_i = mx_i, mn_i
    if not (MIN_APT_RENT <= mn_i <= MAX_APT_RENT and MIN_APT_RENT <= mx_i <= MAX_APT_RENT):
        return None
    return mn_i, mx_i


def parse_propertyhub_monthly_prices(listings_container: Any) -> list[int]:
    items = []
    if isinstance(listings_container, dict):
        items = listings_container.get("listings") or []
    elif isinstance(listings_container, list):
        items = listings_container

    prices: list[int] = []
    for it in items:
        if not isinstance(it, dict):
            continue
        p = (
            (it.get("price") or {})
            .get("forRent", {})
            .get("monthly", {})
            .get("price")
        )
        if p is None:
            continue
        try:
            pi = int(round(float(p)))
        except (TypeError, ValueError):
            continue
        if MIN_CONDO_RENT <= pi <= MAX_CONDO_RENT:
            prices.append(pi)
    return prices


def summarize_condo_prices(prices: list[int]) -> dict[str, int] | None:
    if not prices:
        return None
    s = sorted(float(p) for p in prices)
    med = round_to_500(percentile(s, 50))
    p25 = min(med, round_to_500(percentile(s, 25)))
    p75 = max(med, round_to_500(percentile(s, 75)))
    return {
        "min_rent_thb": p25,
        "median_rent_thb": med,
        "max_rent_thb": p75,
        "sample_count": len(prices),
    }


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Sync real building & station monthly rent prices from RentHub and PropertyHub."
    )
    parser.add_argument("--stations", type=Path, default=Path("backend/data/stations.json"))
    parser.add_argument("--listings", type=Path, default=Path("backend/data/listings.json"))
    parser.add_argument("--output", type=Path, default=Path("backend/data/portal_prices.json"))
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--delay", type=float, default=0.10)
    args = parser.parse_args()

    stations: list[dict[str, Any]] = json.loads(args.stations.read_text(encoding="utf-8"))
    listings: list[dict[str, Any]] = json.loads(args.listings.read_text(encoding="utf-8"))
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")

    fetcher = PoliteFetcher(delay_s=args.delay)

    buildings_out: dict[str, dict[str, Any]] = {}
    stations_out: dict[str, dict[str, Any]] = {}

    # Harvested catalogs keyed by station_id for Phase 3 exact-name matching of unlinked buildings
    rh_by_station: dict[str, list[dict[str, Any]]] = defaultdict(list)
    ph_projects_by_station: dict[str, dict[str, dict[str, Any]]] = defaultdict(dict)

    # ------------------------------------------------------------------
    # Phase 1: Direct building pages (478 verified URLs in listings.json)
    # ------------------------------------------------------------------
    direct_jobs = [
        l for l in listings
        if "renthub.in.th" in (l.get("listing_url") or "")
        or "propertyhub.in.th" in (l.get("listing_url") or "")
    ]
    print(f"Phase 1: Fetching {len(direct_jobs)} direct building pages (RentHub + PropertyHub)...")
    t0 = time.time()

    def process_direct_listing(row: dict[str, Any]) -> tuple[str, dict[str, Any] | None, list[dict[str, Any]]]:
        lid = row["id"]
        url = row.get("listing_url") or ""
        pp = fetcher.fetch_next_data(url)
        if not pp:
            return lid, None, []

        if "renthub.in.th" in url:
            lst = pp.get("listing") or {}
            rng = parse_renthub_monthly(lst.get("price"))
            nearby_harvest = []
            for nb in pp.get("nearbyListings") or []:
                nb_rng = parse_renthub_monthly(nb.get("price"))
                if nb_rng and nb.get("name"):
                    nearby_harvest.append({
                        "name": nb["name"],
                        "slug": nb.get("slug") or "",
                        "min": nb_rng[0],
                        "max": nb_rng[1],
                    })
            if not rng:
                return lid, None, nearby_harvest
            mn, mx = rng
            med = round_to_100((mn + mx) / 2.0)
            return lid, {
                "min_rent_thb": mn,
                "median_rent_thb": med,
                "max_rent_thb": mx,
                "sample_count": 1,
                "source": "RentHub",
                "source_url": url,
                "match_type": "direct_url",
                "updated_at": today,
            }, nearby_harvest

        # PropertyHub project page
        prices = parse_propertyhub_monthly_prices(pp.get("listings"))
        stats = summarize_condo_prices(prices)
        if not stats:
            return lid, None, []
        return lid, {
            **stats,
            "source": "PropertyHub",
            "source_url": url,
            "match_type": "direct_url",
            "updated_at": today,
        }, []

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        future_map = {pool.submit(process_direct_listing, row): row for row in direct_jobs}
        for fut in concurrent.futures.as_completed(future_map):
            row = future_map[fut]
            lid, entry, nearby = fut.result()
            if entry:
                buildings_out[lid] = entry
            sid = row.get("nearest_station_id")
            if sid and nearby:
                rh_by_station[sid].extend(nearby)

    print(
        f"Phase 1 done in {time.time() - t0:.1f}s: "
        f"{len(buildings_out)}/{len(direct_jobs)} direct buildings priced "
        f"(ok={fetcher.ok_count}, 404={fetcher.not_found_count}, err={fetcher.err_count}, rate_limited={fetcher.rate_limited.is_set()})"
    )

    # ------------------------------------------------------------------
    # Phase 2: Station portal pages (Condos via PropertyHub, Apartments via RentHub)
    # ------------------------------------------------------------------
    station_jobs: list[tuple[str, str, str]] = []
    for s in stations:
        sid = s["id"]
        if s.get("portal_condo_url"):
            station_jobs.append((sid, "condo", s["portal_condo_url"]))
        if s.get("portal_apt_url"):
            station_jobs.append((sid, "apartment", s["portal_apt_url"]))

    print(f"Phase 2: Fetching {len(station_jobs)} station portal pages (condo + apartment)...")
    t1 = time.time()

    def process_station_page(job: tuple[str, str, str]) -> tuple[str, str, dict[str, Any] | None, list[Any]]:
        sid, kind, url = job
        pp = fetcher.fetch_next_data(url)
        if not pp:
            return sid, kind, None, []

        if kind == "apartment":
            items = pp.get("listings") or []
            mids: list[float] = []
            harvested = []
            for it in items:
                if not isinstance(it, dict):
                    continue
                rng = parse_renthub_monthly(it.get("price"))
                if not rng:
                    continue
                mn, mx = rng
                mids.append((mn + mx) / 2.0)
                if it.get("name"):
                    harvested.append({
                        "name": it["name"],
                        "slug": it.get("slug") or "",
                        "min": mn,
                        "max": mx,
                    })
            if len(mids) < 2:
                return sid, kind, None, harvested
            mids.sort()
            med = round_to_100(percentile(mids, 50))
            p25 = min(med, round_to_100(percentile(mids, 25)))
            p75 = max(med, round_to_100(percentile(mids, 75)))
            return sid, kind, {
                "p25_monthly_thb": p25,
                "median_monthly_thb": med,
                "p75_monthly_thb": p75,
                "sample_count": len(mids),
                "source": "RentHub",
                "source_url": url,
                "updated_at": today,
            }, harvested

        # PropertyHub condo station page
        raw_items = (pp.get("listings") or {}).get("listings") or []
        prices = parse_propertyhub_monthly_prices(raw_items)
        proj_harvest = []
        for it in raw_items:
            if not isinstance(it, dict):
                continue
            proj = it.get("project") or {}
            p = (it.get("price") or {}).get("forRent", {}).get("monthly", {}).get("price")
            if not proj or p is None:
                continue
            try:
                pi = int(round(float(p)))
            except (TypeError, ValueError):
                continue
            if MIN_CONDO_RENT <= pi <= MAX_CONDO_RENT and proj.get("slug"):
                proj_harvest.append({
                    "slug": proj["slug"],
                    "name": proj.get("name") or "",
                    "name_en": proj.get("nameEnglish") or "",
                    "price": pi,
                })
        stats = summarize_condo_prices(prices)
        if not stats or stats["sample_count"] < 2:
            return sid, kind, None, proj_harvest
        return sid, kind, {
            "p25_monthly_thb": stats["min_rent_thb"],
            "median_monthly_thb": stats["median_rent_thb"],
            "p75_monthly_thb": stats["max_rent_thb"],
            "sample_count": stats["sample_count"],
            "source": "PropertyHub",
            "source_url": url,
            "updated_at": today,
        }, proj_harvest

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        for sid, kind, summary, harvested in pool.map(process_station_page, station_jobs):
            if summary:
                stations_out.setdefault(sid, {})[kind] = summary
            if kind == "apartment" and harvested:
                rh_by_station[sid].extend(harvested)
            elif kind == "condo" and harvested:
                for ph in harvested:
                    slug = ph["slug"]
                    bucket = ph_projects_by_station[sid].setdefault(
                        slug, {"slug": slug, "name": ph["name"], "name_en": ph["name_en"], "prices": []}
                    )
                    bucket["prices"].append(ph["price"])

    condo_st_count = sum(1 for v in stations_out.values() if "condo" in v)
    apt_st_count = sum(1 for v in stations_out.values() if "apartment" in v)
    print(
        f"Phase 2 done in {time.time() - t1:.1f}s: "
        f"{len(stations_out)} stations priced (condo={condo_st_count}, apartment={apt_st_count})"
    )

    # ------------------------------------------------------------------
    # Phase 3: Match remaining unlinked listings strictly within same station
    # ------------------------------------------------------------------
    matched_extra = 0
    for row in listings:
        lid = row["id"]
        if lid in buildings_out:
            continue
        sid = row.get("nearest_station_id")
        if not sid:
            continue
        ptype = row.get("property_type")
        t_th = row.get("title_th") or ""
        t_en = row.get("title_en") or ""

        if ptype == "apartment":
            cands = rh_by_station.get(sid) or []
            for c in cands:
                if names_match_strict(t_th, t_en, [c["name"]]):
                    mn, mx = c["min"], c["max"]
                    buildings_out[lid] = {
                        "min_rent_thb": mn,
                        "median_rent_thb": round_to_100((mn + mx) / 2.0),
                        "max_rent_thb": mx,
                        "sample_count": 1,
                        "source": "RentHub",
                        "source_url": f"https://www.renthub.in.th/{c['slug']}" if c.get("slug") else "",
                        "match_type": "station_name_match",
                        "updated_at": today,
                    }
                    matched_extra += 1
                    break
        elif ptype == "condo":
            cands_map = ph_projects_by_station.get(sid) or {}
            for slug, pinfo in cands_map.items():
                if names_match_strict(t_th, t_en, [pinfo["name"], pinfo["name_en"]]):
                    stats = summarize_condo_prices(pinfo["prices"])
                    if stats:
                        buildings_out[lid] = {
                            **stats,
                            "source": "PropertyHub",
                            "source_url": f"https://propertyhub.in.th/เช่าคอนโด/โครงการ-{slug}",
                            "match_type": "station_name_match",
                            "updated_at": today,
                        }
                        matched_extra += 1
                        break

    print(
        f"Phase 3 done: +{matched_extra} extra buildings matched by same-station exact name "
        f"-> total exact buildings priced: {len(buildings_out)}/{len(listings)}"
    )

    payload = {
        "meta": {
            "generated_at": datetime.now(timezone.utc).isoformat(),
            "updated_date": today,
            "sources": ["RentHub.in.th", "PropertyHub.in.th"],
            "total_listings": len(listings),
            "priced_buildings": len(buildings_out),
            "direct_url_buildings": sum(1 for v in buildings_out.values() if v["match_type"] == "direct_url"),
            "name_matched_buildings": matched_extra,
            "priced_stations_total": len(stations_out),
            "priced_stations_condo": condo_st_count,
            "priced_stations_apartment": apt_st_count,
            "rate_limited": fetcher.rate_limited.is_set(),
        },
        "buildings": dict(sorted(buildings_out.items())),
        "stations": dict(sorted(stations_out.items())),
    }

    if not buildings_out and not stations_out:
        print("Error: fetched 0 buildings and 0 stations; refusing to overwrite existing output.", file=sys.stderr)
        return 1

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(
        json.dumps(payload, indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )

    # Print sanity checks for well-known buildings mentioned by user
    print("\n=== Spot-Check Samples ===")
    for check_id, label in [("LST_0729", "สิริมา แมนชั่น"), ("LST_0119", "คิว วัน สุขุมวิท"), ("LST_0138", "MSI III Garden")]:
        b = buildings_out.get(check_id)
        print(f"  {check_id} ({label}): {b}")
    for check_sid in ("BTS_E4", "MRT_BL18", "MRT_BL17", "BTS_N5", "BTS_N18"):
        print(f"  Station {check_sid}: {stations_out.get(check_sid)}")
    print(f"\nWrote {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
