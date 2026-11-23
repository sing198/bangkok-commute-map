# LinkedIn Post Draft — Bangkok Commute Map

> แนบคลิปวิดีโอเดโม 25 วินาทีพร้อมโพสต์ได้เลยจากไฟล์: `docs/bangkok-commute-map-demo.mp4`

## Version 1: ภาษาไทย (แนะนำสำหรับโพสต์หางาน / โชว์ผลงานสาย Dev ในไทย)

เคยไหมครับ? หาหอ/คอนโดในกรุงเทพฯ ที่ค่าเช่าถูกกว่าหลายพันบาท แต่พออยู่จริงกลับเสียค่ารถไฟฟ้าไป-กลับ + ค่าวินมอเตอร์ไซค์เพิ่มเกือบสองพัน แถมเสียเวลาชีวิตบนรถไฟฟ้าเพิ่มอีกหลายสิบชั่วโมงต่อเดือน

ผมเลยลองสร้างโปรเจกต์ **Bangkok Commute Map** เว็บช่วยค้นหาและเปรียบเทียบทำเลที่พักใน กทม. ตาม **“เวลาเดินทางจริงด้วย BTS/MRT/ARL”** และ **“ค่าครองชีพรวมจริงต่อเดือน (ค่าเช่า + ค่าเดินทาง)”** ครับ (ในคลิป 25 วินาที ลองพิมพ์ค้นหาตึก G Tower พระราม 9 แล้วเทียบ 3 คอนโดจริง เห็นส่วนต่างชัดเจนถึง ฿9,708/เดือน vs 8.1 ชม./เดือน)

สิ่งที่ทำในโปรเจกต์นี้ (Full-Stack + Spatial DB):
- **PostGIS Spatial Indexing (`GEOGRAPHY` + `GIST`)**: ใช้ KNN (`<->`) หาสถานีรถไฟฟ้าใกล้ออฟฟิศ 3 แห่งแรก และ `ST_DWithin` กรองอาคารที่พักจริงรอบสถานีเป็นเมตรจริงบนพื้นโลก (พิกัดอาคารจริง 183 แห่งจาก OpenStreetMap + Curated Condos)
- **In-Memory Transit Graph (Go / Dijkstra)**: คำนวณเวลาเดินทางจริงบน 79 สถานี / 166 เส้นเชื่อม คิดเวลาเปลี่ยนสายในค่าย (+3 นาทีที่สยาม/เตาปูน) และข้ามค่าย (+6–7 นาที พร้อมแยกค่าโดยสารตามจริงและส่วนต่อขยาย BTS +15฿ cap 62฿)
- **Upstash Redis Caching + Region Co-location**: ปัดพิกัดออฟฟิศ ~100 เมตรเพื่อแชร์แคช พร้อมปักหมุด Vercel Functions ไว้ที่สิงคโปร์ (`sin1`) คู่กับ Supabase PostGIS และ Upstash Redis ลด latency จาก ~2.2 วิ เหลือ **~11–25 ms เมื่อ `X-Cache: HIT`** (โชว์สดบนหน้าเว็บ พร้อม graceful fallback)
- **Free OSM Place Search + Compare 3 Locations**: พิมพ์ค้นหาชื่อตึกออฟฟิศ/ห้างใน กทม. ฟรี (OpenStreetMap Photon + Nominatim) หรือลากหมุดเองได้ทันที พร้อมเทียบ 3 ทำเลข้างกันทั้งแบบเข้าออฟฟิศ 22 วัน และ Hybrid 12 วัน

ลองกดเล่นได้เลยครับ (ไม่ต้องล็อกอิน):
Live Demo: https://bangkok-commute-map.vercel.app/
Source Code & PostGIS CI Tests: https://github.com/sing198/bangkok-commute-map

หากพี่ๆ HR หรือ Tech Lead กำลังมองหา **Junior Full-Stack / Backend Developer (Go, React/TypeScript, PostgreSQL/PostGIS, Redis)** สามารถทักมาพูดคุยได้เลยนะครับ ยินดีรับฟีดแบ็กเต็มที่ครับ!

#Golang #PostgreSQL #PostGIS #React #TypeScript #Redis #FullStackDeveloper #SoftwareEngineering #Bangkok

---

## Version 2: English (for international tech recruiters / English feed)

In Bangkok, straight-line apartment search is misleading — a cheaper condo farther out can quietly add thousands of baht in BTS/MRT/ARL transfers and dozens of hours commuting every month.

I built **Bangkok Commute Map** to rank and compare 183 real Bangkok condos & apartments by **door-to-door BTS/MRT/ARL commute time** and **True Monthly Cost (Rent + Rail + Feeder)**. In the 25s clip below, searching "G Tower" and comparing 3 real condos surfaces a **THB 9,708/mo vs 8.1 hrs/mo** trade-off in seconds.

Engineering highlights:
- **PostgreSQL + PostGIS (`GEOGRAPHY(Point, 4326)` + `GIST`)**: KNN (`<->`) nearest-station lookup & `ST_DWithin` spatial filtering across 79 transit stations and 183 real Bangkok buildings (sourced from OpenStreetMap + curated landmarks).
- **Go / Gin In-Memory Dijkstra Engine**: Models same-operator transfers (+3 min), cross-operator transfers (+6–7 min with separate fares), BTS extension surcharges (+15 THB, 62 THB cap), and automatic walk-to-motorcycle-taxi feeder fallback.
- **Upstash Redis Cache + Region Co-location (`sin1`)**: Quantizes office coordinates to ~100m precision, exposes live `X-Cache: HIT/MISS` & `X-Query-Time-Ms` headers in the UI, and pinning Vercel Services to Singapore (`sin1`) beside Supabase PostGIS & Upstash Redis cut response times from ~2.2s cross-region down to **~11–25 ms on HIT** (~27–53 ms on PostGIS MISS).
- **Verified CI Pipeline**: GitHub Actions runs fare/transfer unit tests + real PostGIS & Redis integration tests with `-race` and idempotency checks.

Try it live (no sign-up): https://bangkok-commute-map.vercel.app/
GitHub Repo & CI: https://github.com/sing198/bangkok-commute-map

Open to Junior Full-Stack / Backend roles (Go, React/TypeScript, PostgreSQL, Redis). Feedback welcome!

#Go #PostGIS #PostgreSQL #Redis #React #TypeScript #FullStack #BackendEngineering
