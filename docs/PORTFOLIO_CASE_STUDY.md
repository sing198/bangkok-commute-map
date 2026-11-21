# Portfolio Case Study Draft — Bangkok Commute Map

พร้อมนำไปใส่ใน `projectDetails.js` ของ `portfolio-git` ได้ทันที

---

## Project Card / Case Study Content

- **Title:** Bangkok Commute Map — True-Cost Transit Apartment Finder
- **Tech Stack:** Go (Gin), PostgreSQL 16 + PostGIS, Redis 7, React 19, TypeScript, Tailwind CSS, Leaflet, Docker, GitHub Actions
- **Challenge:**
  การค้นหาที่พักในกรุงเทพฯ ด้วยรัศมีวงกลม (Straight-line distance) ไม่สะท้อนเวลาและค่าใช้จ่ายจริง เพราะการเดินทางขึ้นอยู่กับโครงข่าย BTS/MRT/ARL จุดเปลี่ยนสาย ค่าโดยสารแยกค่าย และส่วนต่อขยาย ทำให้ผู้เช่ามักเลือกห้องที่ค่าเช่าถูกกว่าแต่เสียค่าเดินทางและเวลาชีวิตต่อเดือนมากกว่า
- **Key Engineering Decisions:**
  1. **PostGIS Spatial Indexing (`GEOGRAPHY` + `GIST`) + In-Memory Transit Graph (Go):** ใช้ PostGIS KNN (`<->`) หาสถานีใกล้ออฟฟิศ 3 แห่งแรก และ `ST_DWithin` กรองที่พักเป็นเมตรจริงบนพื้นโลก ผสานกับ Dijkstra ใน Go ที่คำนวณเวลาเปลี่ยนสาย (+3/+6 นาที) และกฎค่าโดยสารแยกค่าย BTS (รวมส่วนต่อขยาย & cap 62฿), MRT, ARL
  2. **True Monthly Cost & Trade-off Comparison:** เปลี่ยนแกนการจัดอันดับจาก “ค่าเช่าต่ำสุด” เป็น “ค่าครองชีพรวมจริงต่อเดือน (ค่าเช่า + ค่ารถไฟฟ้า + ค่าวินมอเตอร์ไซค์)” พร้อมหน้าเปรียบเทียบ 3 ทำเลข้างกันให้เห็นส่วนต่างเงินและชั่วโมงเดินทางต่อเดือน
  3. **Redis Coordinate-Quantized Caching with Graceful Degradation:** ปัดพิกัดออฟฟิศ ~100 เมตรเพื่อเพิ่ม Cache Hit Rate พร้อมแสดง `X-Cache: HIT/MISS` และ `X-Query-Time-Ms` บน UI และทำงานต่อผ่าน PostGIS ได้แม้ Redis ล่ม
- **Verifiable Evidence:**
  - GitHub Actions CI รัน Unit Tests สูตรค่าโดยสาร/เส้นทาง + Integration Tests บน `postgis/postgis:16-3.4` และ `redis:7-alpine` จริง พร้อม `-race` และตรวจสอบความ idempotent ของ Seed CLI
  - โค้ดตัวอย่างที่ชี้ให้ดูได้ตรงๆ:
    - Dijkstra + Thai Fare Rules: `backend/internal/transit/graph.go`
    - PostGIS KNN & `ST_DWithin`: `backend/internal/commute/repository.go`
    - Redis Quantization Cache: `backend/internal/commute/service.go`

---

## สคริปต์อัดคลิปเดโม 25 วินาที (สำหรับแปะหน้า Portfolio / LinkedIn)

1. **[0:00–0:05]** เปิดหน้าเว็บ (โหลดโซน **อโศก** ขึ้นทันที) → ชี้วงสีเขียว/เหลือง/ส้มรอบสถานี BTS/MRT/ARL
2. **[0:05–0:10]** กดสลับ Preset ไป **สีลม** หรือ **พระราม 9** แล้วคลิกย้ายหมุด **“งาน”** บนแผนที่ → ให้เห็นป้าย `Redis Cache: MISS → HIT` พร้อม latency หลัก ms
3. **[0:10–0:15]** คลิกการ์ดที่พัก 1 แห่งให้เส้นทางรถไฟฟ้าขึ้นบนแผนที่ พร้อมปรับวันทำงานจาก `22 วัน` เป็น `Hybrid 12 วัน` เพื่อโชว์ค่าครองชีพรวมเปลี่ยนทันที
4. **[0:15–0:25]** กด **“เทียบทำเล”** 3 แห่ง (เช่น ใกล้ CBD vs อ่อนนุช vs ส่วนต่อขยาย) แล้วเปิด Modal เปรียบเทียบให้เห็นกราฟ **ค่าเช่า + ค่าเดินทาง/เดือน** และ **ชั่วโมงเดินทางที่ประหยัดได้**
