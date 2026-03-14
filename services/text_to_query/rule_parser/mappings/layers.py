"""Layer keyword mapping — Thai keywords → (layer_name, layer_thai_label)."""

LAYER_KEYWORDS = {
    # pipe
    "ท่อประปา": ("pipe", "ท่อประปา"),
    "ท่อจ่ายน้ำ": ("pipe", "ท่อจ่ายน้ำ"),
    "ท่อส่งน้ำ": ("pipe", "ท่อส่งน้ำ"),
    "ท่อน้ำดิบ": ("pipe", "ท่อน้ำดิบ"),
    "ท่อ": ("pipe", "ท่อประปา"),
    # valve
    "ประตูน้ำ": ("valve", "ประตูน้ำ"),
    "วาล์ว": ("valve", "วาล์ว"),
    # firehydrant
    "หัวดับเพลิง": ("firehydrant", "หัวดับเพลิง"),
    "หัวจ่ายน้ำดับเพลิง": ("firehydrant", "หัวจ่ายน้ำดับเพลิง"),
    "ดับเพลิง": ("firehydrant", "หัวดับเพลิง"),
    # meter
    "มาตรวัดน้ำ": ("meter", "มาตรวัดน้ำ"),
    "มาตร": ("meter", "มาตรวัดน้ำ"),
    "มิเตอร์": ("meter", "มิเตอร์"),
    # bldg
    "อาคาร": ("bldg", "อาคาร"),
    "บ้าน": ("bldg", "อาคาร/บ้าน"),
    "สิ่งปลูกสร้าง": ("bldg", "อาคาร/สิ่งปลูกสร้าง"),
    # leakpoint
    "จุดแตกรั่ว": ("leakpoint", "จุดแตกรั่ว"),
    "จุดรั่ว": ("leakpoint", "จุดรั่ว"),
    "น้ำรั่ว": ("leakpoint", "จุดน้ำรั่ว"),
    "แตกรั่ว": ("leakpoint", "จุดแตกรั่ว"),
    "จุดซ่อมท่อ": ("leakpoint", "จุดซ่อมท่อ"),
    "ซ่อมท่อ": ("leakpoint", "จุดซ่อมท่อ"),
    # pwa_waterworks
    "สำนักงาน": ("pwa_waterworks", "สำนักงาน"),
    "ที่ตั้งกิจการ": ("pwa_waterworks", "ที่ตั้งกิจการประปา"),
    "สถานีผลิต": ("pwa_waterworks", "สถานีผลิตน้ำ"),
    "สถานีสูบ": ("pwa_waterworks", "สถานีสูบน้ำ"),
    "โรงกรองน้ำ": ("pwa_waterworks", "โรงกรองน้ำ"),
    "โรงกรอง": ("pwa_waterworks", "โรงกรองน้ำ"),
    # struct
    "สิ่งก่อสร้าง": ("struct", "สิ่งก่อสร้าง"),
    # pipe_serv
    "ท่อบริการ": ("pipe_serv", "ท่อบริการ"),
    "ท่อแยกเข้าบ้าน": ("pipe_serv", "ท่อบริการ"),
    # flow_meter
    "มาตรวัดอัตราการไหล": ("flow_meter", "มาตรวัดอัตราการไหล"),
    "โฟลว์มิเตอร์": ("flow_meter", "โฟลว์มิเตอร์"),
    # dma_boundary
    "ขอบเขต dma": ("dma_boundary", "ขอบเขต DMA"),
    "dma": ("dma_boundary", "DMA"),
}

# Sort keywords longest first so "ท่อประปา" matches before "ท่อ"
_SORTED_LAYER_KW = sorted(LAYER_KEYWORDS.keys(), key=len, reverse=True)
