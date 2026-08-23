# แผนพัฒนาหน้าแผนที่ภาพรวม PWA GIS Online Tracking

วันที่จัดทำ: 23 สิงหาคม 2569

เอกสารต้นทาง: `note/19_requirements_for_create_map_page_20260823.md`

## 1. ข้อสรุปการออกแบบ

สร้างหน้าแยกที่ `/pwa_gis_tracking/map` และเพิ่มเมนู **แผนที่** ใน navigation ของทุกหน้าหลัก ไม่รวมแผนที่เต็มรูปแบบไว้ใน `dashboard.html` เพราะ Dashboard มีตัวกรอง สถิติ กราฟ และตารางหนาแน่นอยู่แล้ว การแยกหน้าทำให้แผนที่ใช้พื้นที่เต็มจอและคงความเรียบง่ายได้

หน้าแผนที่ใช้ MapLibre GL JS และ layout แบบ full viewport โดยมี sidebar ด้านซ้ายที่ย่อ/ขยายได้ ประกอบด้วย:

1. Quick search: ชื่อสาขา รหัสสาขา และพิกัด `lat, lon`
2. Structured filters: เขต สาขา ช่วงวันที่ และชั้นข้อมูล โดย reuse vocabulary/ข้อมูลจาก Dashboard และ Detail
3. Layer control แบ่งเป็น แผนที่ฐาน, ชั้นข้อมูลของระบบ และแผนที่ 3 มิติ
4. ผู้ช่วย AI “น้องหนึ่งน้ำ” อยู่ใน sidebar เดียวกัน
5. Sidebar บนจอเล็กเป็น drawer และต้องปิดได้เพื่อไม่บดบังแผนที่

หน้า Dashboard และ Detail ยังคงหน้าที่เดิม ไม่ย้ายกราฟ ตาราง หรือระบบ export มาไว้บนหน้า Map

## 2. แผนที่ฐานและ 3 มิติ

### 2.1 แผนที่ฐาน

- OSM เป็นค่าเริ่มต้น
- Satellite ใช้ Esri World Imagery ร่วมกับ label จาก OSM เป็น fallback
- เตรียม Google Maps Tile provider แบบ config-driven แต่ปิดไว้จนกว่าจะกำหนด `GOOGLE_MAPS_TILE_API_KEY` และเปิด provider อย่างเป็นทางการ
- ไม่ใช้ endpoint `mt*.google.com/vt` แบบไม่เป็นทางการ
- ปิด MapLibre attribution control มุมขวาล่างตาม requirement แต่ย้ายเครดิตแหล่งข้อมูลที่จำเป็นมาแสดงแบบ minimal ใน sidebar

### 2.2 แผนที่ 3 มิติ

- Terrain ใช้ `raster-dem` TileJSON จาก `MAP_DEM_TILEJSON_URL`
- ค่าเริ่มต้นใช้ MapLibre demo terrain และเปลี่ยนเป็น DTM ภายในได้ผ่าน environment โดยไม่แก้ frontend
- อาคาร OSM ใช้ `fill-extrusion` และเริ่มแสดงที่ zoom 14
- ถ้ามี `render_height`/`render_min_height` ให้ใช้ค่าจริง; ถ้าไม่มีให้ fallback จากจำนวนชั้นอย่างระมัดระวัง

## 3. Progressive layer visibility

เปิด layer หลักไว้เป็นค่าเริ่มต้น แต่ใช้ minzoom เพื่อลดความรกและจำนวน feature ที่ render:

- zoom 5–9: สำนักงานและขอบเขต DMA
- zoom 10–13: เพิ่มท่อประปาและท่อบริการ
- zoom 14–16: เพิ่มอาคาร โครงสร้าง ประตูน้ำ หัวดับเพลิง จุดแตกรั่ว จุดทดสอบ มาตรวัดน้ำ และมาตรวัดอัตราการไหล
- zoom 17 ขึ้นไป: เพิ่ม label และรายละเอียดหนาแน่น

ผู้ใช้ยังเปิด/ปิดแต่ละ layer เองได้ Layer style ยึดจาก `G:\My Drive\web_projects\vector_style` และปรับให้สอดคล้องกับ theme ของโปรเจกต์

สีขอบเขต DMA ต้องอ่านจาก `pgweb_gis2.pwa_dma.dma_boundary` โดย join ด้วย composite key `(pwa_code, dma_id)`:

- fill: `sld_color_fill`
- stroke: `sld_color_stroke`
- ถ้าค่าว่างหรือไม่ใช่ CSS color ที่ปลอดภัย ให้ใช้สี fallback ของระบบ

## 4. Data architecture

MongoDB ยังคงเป็น source of truth สำหรับข้อมูล GIS และรายละเอียด feature ส่วน PostGIS เป็น read-optimized mirror สำหรับ vector tiles:

```text
MongoDB (source of truth)
  -> pwa-gis-map-sync (ทุก 15 นาที)
  -> PostGIS schema pwa_tracking_map
  -> pwa-gis-map-martin (127.0.0.1:5031)
  -> Go authenticated tile proxy
  -> MapLibre
```

### 4.1 PostGIS mirror

สร้าง migration ที่ rerunnable สำหรับ schema `pwa_tracking_map`:

- `features`: geometry, layer, pwa_code, source feature ID, properties JSONB, source timestamps และ sync timestamp
- spatial GiST index บน geometry
- indexes บน `(layer, pwa_code)`, source ID และ source update time
- `sync_state`: watermark/status/error ของแต่ละ Mongo collection
- `map_summary`: metadata/count ที่ sidebar ใช้
- SQL function สำหรับสร้าง MVT ด้วย `ST_AsMVT`/`ST_AsMVTGeom`

MVT ต้องส่งเฉพาะ properties ที่ frontend ใช้ รวม `_fid`, `_pwaCode`, `_layerName` และค่าสี DMA ไม่ส่ง properties ทั้งก้อนเพื่อลดขนาด tile

### 4.2 Sync worker

สร้าง executable แยก `pwa-gis-map-sync` สำหรับติดตั้งเป็น NSSM service:

- initial/full sync ทำเป็น batch และ upsert แบบ idempotent
- incremental sync ทุก 15 นาที อิง `_updatedAt`
- full reconciliation ทุก 24 ชั่วโมงเพื่อจัดการ record ที่ถูกลบหรือ collection เปลี่ยน
- ใช้ bounded concurrency, timeout และ structured logs
- failure ของหนึ่ง collectionต้องไม่ทำให้ state ของ collectionอื่นเสีย
- sync DMA จะ enrich สีจาก PostGIS ด้วย `(pwa_code, dma_id)`
- หลัง sync สำเร็จให้อัปเดต summary และเริ่ม cache warming

### 4.3 Cache warming

- warm metadata/count ทุก 15 นาที
- warm tile ภาพรวมประเทศไทย zoom 5–7
- warm tile รายเขต zoom 8–10
- zoom ลึกกว่านั้นสร้างเมื่อมี request และเก็บใน Martin memory cache
- ป้องกัน cache stampede และจำกัด concurrency/timeout

## 5. Martin และ nginx

สร้าง `martin/config.yaml` ตามรูปแบบ `potential-martin`:

- service name: `pwa-gis-map-martin`
- listen: `127.0.0.1:5031`
- PostgreSQL connection จาก `DATABASE_URL`; ห้าม commit credential
- auto-publish เฉพาะ schema/function ของ `pwa_tracking_map`
- memory cache ปรับผ่าน config

เพิ่ม location ใน `nginx.conf` ของโปรเจกต์โดยคง Martin เป็น loopback-only เส้นทางจาก browser ที่ใช้จริงต้องผ่าน Go tile proxy ซึ่งตรวจ session/RBAC ก่อน ห้ามเปิด Martin catalog/tile functions ให้ผู้ใช้ภายนอกเรียกข้าม authorization ได้โดยตรง

## 6. Authorization

ใช้ scope เดิม:

- `all`: ทุกเขต/ทุกสาขา
- `reg`: เฉพาะสาขาในเขตของผู้ใช้
- `branch`: เฉพาะสาขาของผู้ใช้

เพิ่ม `mapScopeOverrides` ใน `services/rbac.go` แยกจาก `explicitUserIDs` เพื่อขยายเฉพาะสิทธิ์หน้าแผนที่โดยไม่เปลี่ยนสิทธิ์ Detail หรือ download tier

Go tile proxy ต้อง resolve allowed PWA codes จาก session ฝั่ง server แล้วส่งต่อให้ Martin function ห้ามเชื่อ `pwaCode`, `scope` หรือรายชื่อสาขาที่ browser ส่งมาเอง

## 7. Interface และ route

### หน้าเว็บ

- `GET /pwa_gis_tracking/map`

### API ใหม่

- `GET /pwa_gis_tracking/api/map/config` — config ที่เปิดเผยต่อ browser ได้เท่านั้น
- `GET /pwa_gis_tracking/api/map/summary` — summary ตาม RBAC
- `GET /pwa_gis_tracking/api/map/tiles/:layer/:z/:x/:y` — authenticated tile proxy

ใช้ API เดิมสำหรับ zones, offices, layers, session info, feature properties และ chatbot เท่าที่ interface เดิมรองรับ

## 8. Frontend modules

เพิ่มไฟล์เฉพาะหน้าเพื่อไม่เพิ่ม coupling กับ `detail.js` ที่มีขนาดใหญ่:

- `templates/map.html`
- `static/css/map.css`
- `static/js/map.js`
- module ย่อยสำหรับ style/layer definitions หาก `map.js` เริ่มตื้นหรือใหญ่เกินไป

Map module เป็น deep module ที่ interface หลักมีเพียง init, apply filters, toggle layer/3D, locate result และ render AI GeoJSON ส่วน state/style switching/source restoration เก็บไว้ภายใน implementation

ทุก template เพิ่ม favicon `/pwa_gis_tracking/static/images/pwa-logo.jpg`

## 9. UI/UX และ accessibility

- ใช้ Noto Sans Thai, PWA blue/gold, surface/border tokens เดิม
- card โปร่งเล็กน้อย เงาเบา spacing ชัด ไม่ใช้ decoration เกินจำเป็น
- keyboard focus, ARIA label, status/error/empty state และ reduced motion
- sidebar ไม่บัง controls; map resize หลังย่อ/ขยาย sidebar
- แสดง loading แยก map/config/tile/sync status โดยไม่ freeze ทั้งหน้า
- ไม่แสดง secret, raw SQL/Mongo pipeline หรือ internal Martin URL ใน browser

ไม่มี skill `impeccable` ใน environment นี้ จึงใช้หลัก minimal hierarchy, progressive disclosure และ accessibility เป็น fallback

## 10. Test-driven implementation slices

ทำ RED -> GREEN ทีละ behavior ผ่าน public interface:

1. authenticated `/map` route แสดงหน้าได้ และ unauthenticated ถูก redirect
2. `mapScopeOverrides` ขยายเฉพาะ map scope ไม่กระทบ permission/download เดิม
3. tile request ปฏิเสธ layer/zoom/coordinate ที่ไม่ถูกต้องและ derive scope จาก session
4. tile proxy ส่ง allowed PWA codes ที่ถูกต้องสำหรับ all/reg/branch ไปยัง Martin adapter
5. sync transform แปลง Mongo feature เป็น mirror recordโดยคง geometry/identifier ที่จำเป็น
6. DMA enrichment join `(pwa_code, dma_id)` และ validate/fallback สี
7. watermark/upsert/reconciliation เป็น idempotent
8. summary และ warming profile ครอบ zoom 5–10 ตามที่กำหนด
9. frontend smoke/static contract: route มี element/control/script ที่จำเป็น และ JavaScript syntax ผ่าน

หลังแต่ละ slice รัน test เฉพาะ package/file, รัน `go test ./...` และ `go build ./...` ตอนท้าย พร้อม `node --check` สำหรับ JavaScript ใหม่

## 11. Deployment/rollback

- เพิ่ม runbook สำหรับ migration, build sync worker, ติดตั้ง NSSM สอง service, ตั้ง environment, ทดสอบ Martin catalog, nginx test/reload และ smoke test
- migration ต้องไม่แก้/ลบ schema งานเดิม
- rollback หน้าเว็บทำได้โดยถอด route/nav ใหม่
- rollback tile pipeline ทำได้โดยหยุด `pwa-gis-map-sync` และ `pwa-gis-map-martin`; schema `pwa_tracking_map` เก็บไว้เพื่อ forensic/restore จนกว่าจะอนุมัติให้ลบ

## 12. เกณฑ์ส่งมอบ

- หน้า `/map` ใช้งานบน desktop/mobile และไม่บดบังแผนที่
- filter/search/layer/AI อยู่ใน left sidebar
- layer ใช้ vector tilesผ่าน Martin และถูกจำกัดตาม RBAC
- progressive visibility ทำงาน โดย meter เริ่ม zoom 14
- DMA ใช้สีจาก `sld_color_fill`/`sld_color_stroke`
- OSM, satellite fallback, terrain และ OSM 3D buildings สลับได้
- MapLibre control มุมขวาล่างถูกซ่อนและ attribution ที่จำเป็นยังแสดงใน sidebar
- favicon เป็น PWA logo
- sync 15 นาที, daily reconciliation และ warming ทำงานแยก service
- test/build/review ผ่าน และไม่มี credential ใหม่ถูก commit
