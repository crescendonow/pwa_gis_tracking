# Plan: ปรับสิทธิ์ดาวน์โหลดและเพิ่มชั้นข้อมูล DMA Boundary, Step Test, Flow Meter

อ้างอิง: `note/15_new_requirements_for_editlogin_addnew_layer_20260819.md`

## เป้าหมาย

1. ให้พนักงานที่ผ่านฐานข้อมูลยืนยันตัวตนเข้าใช้งานระบบได้ทุกคนตาม flow เดิม
2. ให้ผู้สังกัด `งานน้ำสูญเสีย` ใช้ขอบเขตข้อมูลระดับ `reg` และดาวน์โหลดได้ทุก format
3. ให้พนักงานรหัส `10432` ดาวน์โหลดได้ทุก format โดยไม่เปลี่ยนกติกาขอบเขตข้อมูลเดิม
4. เพิ่ม `dma_boundary`, `step_test` และ `flow_meter` ให้ใช้งานได้สม่ำเสมอในทุกส่วนที่แสดง เลือก นับ ดูแผนที่ ค้นหา และ export ชั้นข้อมูล

## แผนการแก้ไข

### Authentication และ Download Authorization

- ใช้ `ResolvePermission` เป็น interface กลางสำหรับคำนวณขอบเขตข้อมูลและสิทธิ์ดาวน์โหลด
- เพิ่ม `งานน้ำสูญเสีย` ในกติกาขอบเขตระดับ `reg`
- เพิ่มกติกา full-download แบบ exact match สำหรับ `งานน้ำสูญเสีย` เพื่อไม่ให้ `งานบริการและควบคุมน้ำสูญเสีย` ถูกยกระดับสิทธิ์โดยไม่ตั้งใจ
- เพิ่มพนักงาน `10432` เป็น full-download override โดยไม่บังคับเปลี่ยนขอบเขตจากกติกาเดิม
- คงค่าเริ่มต้นของพนักงานทั่วไปเป็น `branch/basic` ตาม implementation สำหรับการเข้าใช้งานของพนักงานทุกคน

### Layer Registry และการแสดงผล

1. ลงทะเบียนทั้งสามชั้นข้อมูลใน `services.LayerConfigs`
2. เพิ่มชื่อใน `GetAllLayerNames` เพื่อให้ `/api/layers`, dashboard, detail, Excel export และ validation ของ export ใช้รายการเดียวกัน
3. กำหนดชื่อภาษาไทยมาตรฐาน
   - `dma_boundary` — ขอบเขต DMA
   - `step_test` — จุดทดสอบ Step Test
   - `flow_meter` — มาตรวัดอัตราการไหล
4. กำหนดรูปแบบแผนที่ detail: DMA Boundary เป็น polygon; Step Test และ Flow Meter เป็น point
5. เพิ่ม label และสีใน Advanced Query, dashboard และ detail
6. เพิ่มการรองรับใน Text-to-Query: allow-list, JSON schema, prompt, keyword mapping และชื่อ/หน่วยของผลลัพธ์
7. คงพฤติกรรมเดิมเมื่อบางสาขายังไม่มี collection โดยระบบนับเป็น 0

## สมมติฐานข้อมูล

- ทั้งสาม collection ใช้ alias `b{pwaCode}_{layerName}`
- ใช้ `globalId` หรือ `_id` เป็น identifier สำหรับการนับ
- ใช้ `recordDate` สำหรับ date filtering และกลไกเดิม fallback ไป `properties._createdAt`
- ยังไม่เพิ่ม filter field เฉพาะ layer จนกว่าจะมี data dictionary จริง

## การทดสอบ

1. RBAC: `10432` ต้องได้ full download; `งานน้ำสูญเสีย` ต้องได้ `reg/full` แม้ตรงกฎขอบเขตอื่น; `งานบริการและควบคุมน้ำสูญเสีย` ต้องยังเป็น `branch/basic`
2. Layer registry: ทั้งสามชื่ออยู่ใน `LayerConfigs`, layer list และมีชื่อภาษาไทย
3. Rule parser: คำถาม DMA, Step Test และ Flow Meter ต้อง resolve เป็น layer ที่ถูกต้อง
4. ตรวจ JavaScript/Python syntax, Go tests, Python tests และ Go build
