import { useEffect, useRef } from 'react';
import L from 'leaflet';
import type {
  ListingResult,
  OfficePoint,
  ReachableStation,
  ZoneColor,
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
  const officeMarkerRef = useRef<L.Marker | null>(null);
  const zonesLayerRef = useRef<L.LayerGroup | null>(null);
  const listingsLayerRef = useRef<L.LayerGroup | null>(null);
  const routeLayerRef = useRef<L.LayerGroup | null>(null);
  const onMoveOfficeRef = useRef(onMoveOffice);

  onMoveOfficeRef.current = onMoveOffice;

  // Initialize Leaflet map once
  useEffect(() => {
    if (!containerRef.current || mapRef.current) return;

    const map = L.map(containerRef.current, {
      center: [office.lat, office.lng],
      zoom: 12,
      zoomControl: false,
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
    if (!layer) return;
    layer.clearLayers();

    for (const rs of reachableStations) {
      const color = ZONE_HEX[rs.zone_color] || '#64748b';
      const circle = L.circle([rs.station.lat, rs.station.lng], {
        radius: 580,
        color,
        weight: 1.5,
        opacity: 0.55,
        fillColor: color,
        fillOpacity: 0.16,
      });

      circle.bindTooltip(
        `<div style="font-size:12px;line-height:1.4">
          <strong>${rs.station.operator} ${rs.station.name_th}</strong> (${rs.station.name_en})<br/>
          ถึงออฟฟิศ ~<strong>${rs.commute_minutes} นาที</strong> • ค่าโดยสาร ฿${rs.one_way_fare_thb}
          ${rs.transfers > 0 ? `• เปลี่ยนสาย ${rs.transfers} ครั้ง` : '• ต่อเดียว'}
        </div>`,
        { direction: 'top', sticky: true }
      );
      circle.addTo(layer);

      // Small station center dot
      L.circleMarker([rs.station.lat, rs.station.lng], {
        radius: 4,
        color: '#ffffff',
        weight: 1.5,
        fillColor: color,
        fillOpacity: 0.95,
      }).addTo(layer);
    }
  }, [reachableStations]);

  // Render listing pins
  useEffect(() => {
    const layer = listingsLayerRef.current;
    if (!layer) return;
    layer.clearLayers();

    for (const item of listings) {
      const isSelected = item.id === selectedListingId;
      const isCompared = compareIds.includes(item.id);
      const color = ZONE_HEX[item.zone_color] || '#475569';

      const marker = L.circleMarker([item.lat, item.lng], {
        radius: isSelected ? 9 : isCompared ? 8 : 6,
        color: isSelected ? '#1e1b4b' : isCompared ? '#4f46e5' : '#ffffff',
        weight: isSelected || isCompared ? 3 : 2,
        fillColor: color,
        fillOpacity: 0.95,
      });

      marker.bindTooltip(
        `<div style="font-size:12px">
          <div style="font-weight:600">${item.title_th}</div>
          <div>รวมจริง <strong>฿${item.cost.true_monthly_cost_thb.toLocaleString()}/ด.</strong> (เช่า ฿${item.monthly_rent_thb.toLocaleString()})</div>
          <div>${item.cost.one_way_minutes} นาที/เที่ยว • ${item.origin_station.operator} ${item.origin_station.name_th}</div>
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

    // Dashed home -> origin station line
    L.polyline(
      [
        [selected.lat, selected.lng],
        [selected.origin_station.lat, selected.origin_station.lng],
      ],
      { color: '#4f46e5', weight: 3, dashArray: '5, 6', opacity: 0.85 }
    ).addTo(layer);

    // Solid rail route polyline
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

    // Dashed dest station -> office line
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
        คลิกจุดใดก็ได้บนแผนที่ หรือลากหมุด <strong className="text-indigo-700">“งาน”</strong> เพื่อเปลี่ยนที่ตั้งออฟฟิศ
      </div>
    </div>
  );
}
