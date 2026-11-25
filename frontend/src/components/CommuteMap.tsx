import { useEffect, useRef } from 'react';
import L from 'leaflet';
import {
  OPERATOR_LABEL,
  type ListingResult,
  type OfficePoint,
  type ReachableStation,
  type ZoneColor,
} from '../types/commute';

interface CommuteMapProps {
  office: OfficePoint;
  onMoveOffice: (next: OfficePoint) => void;
  reachableStations: ReachableStation[];
  listings: ListingResult[];
  selectedListingId: string | null;
  onSelectListing: (listing: ListingResult) => void;
  compareIds: string[];
}

const ZONE_HEX: Record<ZoneColor, string> = {
  green: '#10b981',
  yellow: '#f59e0b',
  orange: '#f97316',
  red: '#ef4444',
};

export function CommuteMap({
  office,
  onMoveOffice,
  reachableStations,
  listings,
  selectedListingId,
  onSelectListing,
  compareIds,
}: CommuteMapProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<L.Map | null>(null);
  const canvasRendererRef = useRef<L.Canvas | null>(null);
  const officeMarkerRef = useRef<L.Marker | null>(null);
  const zonesLayerRef = useRef<L.LayerGroup | null>(null);
  const listingsLayerRef = useRef<L.LayerGroup | null>(null);
  const routeLayerRef = useRef<L.LayerGroup | null>(null);
  const onMoveOfficeRef = useRef(onMoveOffice);

  onMoveOfficeRef.current = onMoveOffice;

  // Initialize Leaflet map once with high-performance Canvas renderer for 1,000+ markers
  useEffect(() => {
    if (!containerRef.current || mapRef.current) return;

    const canvasRenderer = L.canvas({ padding: 0.5 });
    canvasRendererRef.current = canvasRenderer;

    const map = L.map(containerRef.current, {
      center: [office.lat, office.lng],
      zoom: 12,
      zoomControl: false,
      preferCanvas: true,
    });

    L.control.zoom({ position: 'topright' }).addTo(map);

    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '&copy; OpenStreetMap contributors',
      maxZoom: 19,
    }).addTo(map);

    zonesLayerRef.current = L.layerGroup().addTo(map);
    routeLayerRef.current = L.layerGroup().addTo(map);
    listingsLayerRef.current = L.layerGroup().addTo(map);

    const officeIcon = L.divIcon({
      className: '',
      html: `<div class="office-pin-pulse" title="ลากเพื่อย้ายจุดออฟฟิศ">งาน</div>`,
      iconSize: [34, 34],
      iconAnchor: [17, 17],
    });

    const marker = L.marker([office.lat, office.lng], {
      icon: officeIcon,
      draggable: true,
      zIndexOffset: 1000,
    }).addTo(map);

    marker.on('dragend', () => {
      const pos = marker.getLatLng();
      onMoveOfficeRef.current({ lat: pos.lat, lng: pos.lng });
    });

    map.on('click', (e: L.LeafletMouseEvent) => {
      onMoveOfficeRef.current({ lat: e.latlng.lat, lng: e.latlng.lng });
    });

    officeMarkerRef.current = marker;
    mapRef.current = map;

    return () => {
      map.remove();
      mapRef.current = null;
      canvasRendererRef.current = null;
    };
  }, []);

  // Update office marker & pan smoothly when office changes
  useEffect(() => {
    if (!mapRef.current || !officeMarkerRef.current) return;
    officeMarkerRef.current.setLatLng([office.lat, office.lng]);
    mapRef.current.panTo([office.lat, office.lng], { animate: true, duration: 0.4 });
  }, [office.lat, office.lng]);

  // Render color-coded reachable station zones
  useEffect(() => {
    const layer = zonesLayerRef.current;
    const renderer = canvasRendererRef.current;
    if (!layer || !renderer) return;
    layer.clearLayers();

    for (const rs of reachableStations) {
      const color = ZONE_HEX[rs.zone_color] || '#64748b';
      const opLabel = OPERATOR_LABEL[rs.station.operator] || rs.station.operator;
      const circle = L.circle([rs.station.lat, rs.station.lng], {
        renderer,
        radius: 550,
        color,
        weight: 1.25,
        opacity: 0.5,
        fillColor: color,
        fillOpacity: 0.14,
      });

      circle.bindTooltip(
        `<div style="font-size:12px;line-height:1.4">
          <strong>${opLabel} ${rs.station.name_th}</strong> (${rs.station.name_en})<br/>
          ถึงออฟฟิศ ~<strong>${rs.commute_minutes} นาที</strong> • ค่าโดยสาร ฿${rs.one_way_fare_thb}
          ${rs.transfers > 0 ? `• เปลี่ยนสาย ${rs.transfers} ครั้ง` : '• ต่อเดียว'}
        </div>`,
        { direction: 'top', sticky: true }
      );
      circle.addTo(layer);

      L.circleMarker([rs.station.lat, rs.station.lng], {
        renderer,
        radius: 3.5,
        color: '#ffffff',
        weight: 1.5,
        fillColor: color,
        fillOpacity: 0.95,
      }).addTo(layer);
    }
  }, [reachableStations]);

  // Render listing pins via Canvas for smooth 60fps interaction across hundreds of buildings
  useEffect(() => {
    const layer = listingsLayerRef.current;
    const renderer = canvasRendererRef.current;
    if (!layer || !renderer) return;
    layer.clearLayers();

    for (const item of listings) {
      const isSelected = item.id === selectedListingId;
      const isCompared = compareIds.includes(item.id);
      const isVerified = item.price_status === 'verified';
      const estRent = item.estimated_rent_thb ?? 0;
      const isHarCalibrated = item.price_tier === 'market_har';
      const color = ZONE_HEX[item.zone_color] || '#475569';
      const opLabel = OPERATOR_LABEL[item.origin_station.operator] || item.origin_station.operator;
      const isDirect = item.route.station_ids.length <= 1;
      const walkLabel = isDirect
        ? `ตรงถึงออฟฟิศ ~${item.home_access.distance_m} ม.`
        : item.home_access.distance_source === 'osrm_foot'
          ? `เดินตามถนน ${item.home_access.distance_m} ม.`
          : `~${item.home_access.distance_m} ม.`;
      const estTotal = estRent + item.cost.monthly_commute_thb;
      const priceLine = isVerified
        ? `<div>รวมจริง <strong>฿${item.cost.true_monthly_cost_thb.toLocaleString()}/ด.</strong> <span style="color:#047857;font-weight:700">(เช่าจริง ฿${item.monthly_rent_thb.toLocaleString()})</span></div>`
        : estRent > 0
          ? isHarCalibrated
            ? `<div>เช่าอ้างอิง HAR <strong style="color:#0369a1">~฿${estRent.toLocaleString()}/ด.</strong> <span style="color:#4338ca">(รวม ~฿${estTotal.toLocaleString()})</span></div>`
            : `<div>เช่าประเมิน <strong>~฿${estRent.toLocaleString()}/ด.</strong> <span style="color:#475569">(รวม ~฿${estTotal.toLocaleString()})</span></div>`
          : `<div>ค่าเดินทางจริง <strong>฿${item.cost.monthly_commute_thb.toLocaleString()}/ด.</strong> <span style="color:#64748b">(กดการ์ดเพื่อเช็กราคาห้องจริง ↗)</span></div>`;

      const marker = L.circleMarker([item.lat, item.lng], {
        renderer,
        radius: isSelected ? 9 : isCompared ? 8 : isVerified ? 6.5 : isHarCalibrated ? 6 : 5,
        color: isSelected
          ? '#1e1b4b'
          : isCompared
            ? '#4f46e5'
            : isVerified
              ? '#059669'
              : isHarCalibrated
                ? '#0284c7'
                : '#ffffff',
        weight: isSelected || isCompared ? 3 : 1.75,
        fillColor: color,
        fillOpacity: isSelected || isCompared ? 1 : 0.88,
      });

      marker.bindTooltip(
        `<div style="font-size:12px;line-height:1.4">
          <div style="font-weight:700">${item.title_th}</div>
          ${priceLine}
          <div style="font-size:11px;color:#334155">${item.cost.one_way_minutes} นาที/เที่ยว • ${isDirect ? walkLabel : `${opLabel} ${item.origin_station.name_th} (${walkLabel})`}</div>
        </div>`,
        { direction: 'top', offset: [0, -6] }
      );

      marker.on('click', (e) => {
        L.DomEvent.stopPropagation(e);
        onSelectListing(item);
      });

      marker.addTo(layer);
    }
  }, [listings, selectedListingId, compareIds, onSelectListing]);

  // Draw highlighted transit route for the currently selected listing
  useEffect(() => {
    const layer = routeLayerRef.current;
    if (!layer) return;
    layer.clearLayers();

    if (!selectedListingId) return;
    const selected = listings.find((l) => l.id === selectedListingId);
    if (!selected) return;

    const stationCoordsById = new Map<string, [number, number]>();
    for (const rs of reachableStations) {
      stationCoordsById.set(rs.station.id, [rs.station.lat, rs.station.lng]);
    }
    stationCoordsById.set(selected.origin_station.id, [
      selected.origin_station.lat,
      selected.origin_station.lng,
    ]);
    stationCoordsById.set(selected.dest_station.id, [
      selected.dest_station.lat,
      selected.dest_station.lng,
    ]);

    // Direct door-to-door line when no rail ride is needed (same nearest station)
    if (selected.route.station_ids.length <= 1) {
      L.polyline(
        [
          [selected.lat, selected.lng],
          [office.lat, office.lng],
        ],
        { color: '#4f46e5', weight: 3.5, dashArray: '6, 6', opacity: 0.9 }
      ).addTo(layer);
      return;
    }

    L.polyline(
      [
        [selected.lat, selected.lng],
        [selected.origin_station.lat, selected.origin_station.lng],
      ],
      { color: '#4f46e5', weight: 3, dashArray: '5, 6', opacity: 0.85 }
    ).addTo(layer);

    const railPoints: [number, number][] = [];
    for (const sid of selected.route.station_ids) {
      const coord = stationCoordsById.get(sid);
      if (coord) railPoints.push(coord);
    }
    if (railPoints.length >= 2) {
      L.polyline(railPoints, {
        color: '#4f46e5',
        weight: 4.5,
        opacity: 0.9,
      }).addTo(layer);
    }

    L.polyline(
      [
        [selected.dest_station.lat, selected.dest_station.lng],
        [office.lat, office.lng],
      ],
      { color: '#4f46e5', weight: 3, dashArray: '5, 6', opacity: 0.85 }
    ).addTo(layer);
  }, [selectedListingId, listings, reachableStations, office.lat, office.lng]);

  return (
    <div className="relative h-full w-full">
      <div ref={containerRef} className="h-full w-full" />
      <div className="pointer-events-none absolute bottom-4 left-4 z-[400] hidden rounded-xl border border-slate-200/80 bg-white/95 px-3.5 py-2 text-xs text-slate-600 shadow-md backdrop-blur sm:block">
        คลิกจุดใดก็ได้บนแผนที่ หรือลากหมุด <strong className="text-indigo-700">“งาน”</strong> • ครอบคลุม BTS / MRT ทุกสาย / ARL / SRT แดง (192 สถานี)
      </div>
    </div>
  );
}
