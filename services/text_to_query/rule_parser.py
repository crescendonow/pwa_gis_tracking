"""
Rule-Based Parser — fast path for common GIS queries.
Matches ~90% of typical user questions via regex, responds < 100ms.
Falls back to LLM for unmatched patterns.
"""

import re
import logging
from datetime import datetime

log = logging.getLogger("text_to_query")

# ── Layer keyword mapping ──────────────────────────────────
# Maps Thai keywords → (layer_name, layer_thai_label)
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

# ── Pipe type keyword mapping ─────────────────────────────
# Maps Thai/English keywords → typeId value in MongoDB
PIPE_TYPES = {
    "pvc": "PVC", "พีวีซี": "PVC",
    "ac": "AC", "ซีเมนต์ใยหิน": "AC", "เอซี": "AC",
    "hdpe": "HDPE", "เอชดีพีอี": "HDPE",
    "di": "DI", "เหล็กหล่อเหนียว": "DI",
    "ci": "CI", "เหล็กหล่อ": "CI",
    "gs": "GS", "เหล็กอาบสังกะสี": "GS",
    "st": "ST", "เหล็ก": "ST",
    "pb": "PB",
    "grp": "GRP",
    "pvc-o": "PVC_O", "pvc_o": "PVC_O", "พีวีซีโอ": "PVC_O",
}
_SORTED_PIPE_TYPE_KW = sorted(PIPE_TYPES.keys(), key=len, reverse=True)

# ── Pipe function mapping ─────────────────────────────────
PIPE_FUNCTIONS = {
    "ท่อส่งน้ำระหว่าง": ("4", "ท่อส่งน้ำระหว่างสถานี"),
    "ท่อส่งน้ำ": ("1", "ท่อส่งน้ำ"),
    "ท่อจ่ายน้ำ": ("2", "ท่อจ่ายน้ำ"),
    "ท่อน้ำดิบ": ("5", "ท่อน้ำดิบ"),
    "ท่อปลอก": ("6", "ท่อปลอก"),
}
_SORTED_PIPE_FUNC_KW = sorted(PIPE_FUNCTIONS.keys(), key=len, reverse=True)


def _detect_layer(text):
    """Detect which GIS layer the user is asking about."""
    lower = text.lower()
    for kw in _SORTED_LAYER_KW:
        if kw in lower:
            return LAYER_KEYWORDS[kw]
    return None, None


def _detect_pipe_type(text):
    """Detect pipe type (AC, HDPE, PVC, etc.)."""
    lower = text.lower()
    for kw in _SORTED_PIPE_TYPE_KW:
        if kw in lower:
            return PIPE_TYPES[kw]
    return None


def _detect_pipe_function(text):
    """Detect pipe function (ท่อส่งน้ำ, ท่อจ่ายน้ำ, etc.)."""
    for kw in _SORTED_PIPE_FUNC_KW:
        if kw in text:
            return PIPE_FUNCTIONS[kw]
    return None, None


def _detect_size(text):
    """Extract pipe/meter size from text (e.g. '100 มม.', 'ขนาด 200')."""
    m = re.search(r"ขนาด\s*(\d+)\s*(?:มม\.?|มิลลิเมตร)?", text)
    if m:
        return m.group(1)
    m = re.search(r"(\d+)\s*(?:มม\.?|มิลลิเมตร)", text)
    if m:
        return m.group(1)
    return None


def _detect_size_gte(text):
    """Detect size with '>=' comparison (e.g. 'ขนาด 100 ขึ้นไป', 'ตั้งแต่ 100')."""
    # "ขนาด 100 ขึ้นไป" / "ขนาด 100 มม. ขึ้นไป"
    m = re.search(r"ขนาด\s*(\d+)\s*(?:มม\.?\s*)?(?:ขึ้นไป|ขึ้น)", text)
    if m:
        return m.group(1)
    # "ตั้งแต่ 100 มม." / "ตั้งแต่ขนาด 100"
    m = re.search(r"ตั้งแต่\s*(?:ขนาด\s*)?(\d+)", text)
    if m:
        return m.group(1)
    # "มากกว่า 100" / "เกิน 100"
    m = re.search(r"(?:มากกว่า|เกิน|เกินกว่า)\s*(\d+)", text)
    if m:
        return m.group(1)
    return None


def _detect_age(text):
    """Detect age-based filter (e.g. 'อายุ 10 ปีขึ้นไป', 'เก่ากว่า 20 ปี')."""
    m = re.search(r"อายุ\s*(?:เกิน|มากกว่า)?\s*(\d+)\s*ปี", text)
    if m:
        return int(m.group(1))
    m = re.search(r"เก่า(?:กว่า|เกิน)?\s*(\d+)\s*ปี", text)
    if m:
        return int(m.group(1))
    return None


def _detect_year(text):
    """Extract year from text. Supports both พ.ศ. and ค.ศ."""
    m = re.search(r"(?:พ\.?ศ\.?\s*)?(\d{4})", text)
    if m:
        year = int(m.group(1))
        if year > 2400:  # พ.ศ.
            year -= 543
        return year
    return None


# Thai month names → month number
_THAI_MONTHS = {
    "มกราคม": 1, "ม.ค.": 1, "มค": 1,
    "กุมภาพันธ์": 2, "ก.พ.": 2, "กพ": 2,
    "มีนาคม": 3, "มี.ค.": 3, "มีค": 3,
    "เมษายน": 4, "เม.ย.": 4, "เมย": 4,
    "พฤษภาคม": 5, "พ.ค.": 5, "พค": 5,
    "มิถุนายน": 6, "มิ.ย.": 6, "มิย": 6,
    "กรกฎาคม": 7, "ก.ค.": 7, "กค": 7,
    "สิงหาคม": 8, "ส.ค.": 8, "สค": 8,
    "กันยายน": 9, "ก.ย.": 9, "กย": 9,
    "ตุลาคม": 10, "ต.ค.": 10, "ตค": 10,
    "พฤศจิกายน": 11, "พ.ย.": 11, "พย": 11,
    "ธันวาคม": 12, "ธ.ค.": 12, "ธค": 12,
}

_SORTED_MONTH_KW = sorted(_THAI_MONTHS.keys(), key=len, reverse=True)


def _detect_month(text):
    """Detect Thai month name in text, return month number."""
    for kw in _SORTED_MONTH_KW:
        if kw in text:
            return _THAI_MONTHS[kw]
    return None


def _detect_postgis_zone(text):
    """Detect PostGIS zone query (e.g. 'เขต 2', 'โซน 3')."""
    m = re.search(r"(?:เขต|โซน)\s*(\d+)", text)
    if m:
        return m.group(1)
    return None


def _detect_unit_km(text):
    """Detect if user wants result in kilometers."""
    return bool(re.search(r"กิโลเมตร|กม\.?|km", text, re.IGNORECASE))


def _build_match(pwa_code, pipe_type=None, pipe_func_id=None, size=None, size_gte=None, extra=None):
    """Build a $match stage with optional filters."""
    match = {}
    if pwa_code:
        match["properties.pwaCode"] = pwa_code
    if pipe_type:
        match["properties.typeId"] = pipe_type
    if pipe_func_id:
        match["properties.functionId"] = pipe_func_id
    if size and not size_gte:
        match["properties.sizeId"] = size
    if size_gte:
        match["properties.sizeId"] = {"$gte": size_gte}
    if extra:
        match.update(extra)
    return {"$match": match} if match else {"$match": {}}


def parse_rule(prompt, pwa_code=""):
    """
    Try to match the user prompt against known patterns.

    Returns:
        dict (same format as LLM intent) if matched, None otherwise.
    """
    text = prompt.strip()

    # Detect layer
    layer, layer_label = _detect_layer(text)

    # ── Common detectors ──────────────────────────────────
    is_count = bool(re.search(
        r"จำนวน|ทั้งหมด|(?:มี|รวม)?กี่(?:เครื่อง|ตัว|จุด|หลัง|ท่อ|แห่ง|อัน|รายการ)?",
        text
    ))
    is_group = bool(re.search(r"แยกตาม|แบ่งตาม|จำแนกตาม", text))
    is_show_position = bool(re.search(r"แสดง|ดู|ตำแหน่ง|ที่ตั้ง|แผนที่|อยู่ที่ไหน|อยู่ตรงไหน", text))
    is_total_length = bool(re.search(
        r"(?:ความยาว|ยาว).*(?:รวม|ทั้งหมด|ทั้งประเทศ)|รวม.*(?:กี่เมตร|เมตร|กม\.?|กิโลเมตร)",
        text
    ))
    is_pipe_total = layer == "pipe" and bool(re.search(
        r"รวม.*(?:เมตร|กม|กิโล)|ยาว.*(?:รวม|ทั้งหมด|กี่)", text
    ))
    is_nationwide = bool(re.search(r"ทั้งประเทศ|ทุกสาขา|ทั้งหมดทุก", text))

    size = _detect_size(text)
    size_gte = _detect_size_gte(text)
    year = _detect_year(text)
    month = _detect_month(text)
    age = _detect_age(text)
    pipe_type = _detect_pipe_type(text) if layer == "pipe" else None
    pipe_func_id, pipe_func_label = _detect_pipe_function(text) if layer == "pipe" else (None, None)
    want_km = _detect_unit_km(text)
    zone = _detect_postgis_zone(text)

    # If nationwide query, clear pwa_code
    effective_pwa = "" if is_nationwide else pwa_code

    # ─────────────────────────────────────────────────────
    # POSTGIS: "สาขาในเขต X", "รายชื่อสาขาเขต X"
    # ─────────────────────────────────────────────────────
    if zone and re.search(r"สาขา", text):
        return {
            "text_response": "กำลังค้นหารายชื่อสาขาทั้งหมดในเขต {} จากฐานข้อมูล PostGIS ค่ะ".format(zone),
            "target_db": "postgis",
            "response_type": "table",
            "intent_summary": "List all branches in zone {}".format(zone),
            "query": {
                "postgis": {
                    "sql": "SELECT pwa_code, name, zone FROM pwa_office.pwa_office234 WHERE zone = '{}' ORDER BY name".format(zone)
                }
            },
            "_rule_matched": "postgis_zone",
        }

    # POSTGIS: "สาขาทั้งหมด", "รายชื่อสาขา"
    if re.search(r"สาขา", text) and is_count and not layer:
        return {
            "text_response": "กำลังค้นหารายชื่อสาขาทั้งหมดจากฐานข้อมูล PostGIS ค่ะ",
            "target_db": "postgis",
            "response_type": "table",
            "intent_summary": "List all branches",
            "query": {
                "postgis": {
                    "sql": "SELECT pwa_code, name, zone FROM pwa_office.pwa_office234 ORDER BY name"
                }
            },
            "_rule_matched": "postgis_all_branches",
        }

    # Count fire hydrants / pipe in zone (needs PostGIS to get pwa_codes first)
    # e.g. "จำนวนหัวดับเพลิงทั้งหมดใน เขต 10"
    # This requires multi-step query → let LLM handle for now

    if not layer:
        return None  # Can't determine layer → fallback to LLM

    # ─────────────────────────────────────────────────────
    # PIPE: Total length (with optional type/size/function filters)
    # "ความยาวท่อรวม", "ท่อ AC ขนาด 100 ขึ้นไป ยาวรวมกี่เมตร"
    # "ความยาวท่อส่งน้ำรวม ของสาขาจันทบุรี"
    # ─────────────────────────────────────────────────────
    if layer == "pipe" and (is_total_length or is_pipe_total):
        match_stage = _build_match(effective_pwa, pipe_type, pipe_func_id, size, size_gte)

        pipeline = [
            match_stage,
            {"$group": {
                "_id": None,
                "total_length": {"$sum": {"$toDouble": "$properties.length"}}
            }},
        ]

        if want_km:
            pipeline.append({"$project": {
                "_id": 0,
                "total_length_km": {"$round": [{"$divide": ["$total_length", 1000]}, 2]}
            }})
        else:
            pipeline.append({"$project": {"_id": 0, "total_length": {"$round": ["$total_length", 2]}}})

        # Build description
        desc_parts = []
        if pipe_type:
            desc_parts.append("ชนิด {}".format(pipe_type))
        if pipe_func_label:
            desc_parts.append(pipe_func_label)
        if size_gte:
            desc_parts.append("ขนาด {} มม. ขึ้นไป".format(size_gte))
        elif size:
            desc_parts.append("ขนาด {} มม.".format(size))
        desc = " ".join(desc_parts)
        if desc:
            desc = " " + desc

        return {
            "text_response": "กำลังคำนวณความยาวรวมของท่อประปา{}ค่ะ".format(desc),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Total pipe length{}".format(" " + desc if desc else ""),
            "query": {
                "mongo": {
                    "pwa_code": effective_pwa or None,
                    "layer": "pipe",
                    "pipeline": pipeline,
                    "operation": "aggregate",
                }
            },
            "_rule_matched": "pipe_total_length",
        }

    # ─────────────────────────────────────────────────────
    # GROUP BY — "ท่อแยกตามขนาด", "ท่อแยกตามชนิด"
    # ─────────────────────────────────────────────────────
    if is_group and layer:
        group_field = None
        group_label = ""

        if re.search(r"ขนาด", text):
            if layer == "pipe":
                group_field = "$properties.sizeId"
                group_label = "ขนาด (มม.)"
            elif layer == "meter":
                group_field = "$properties.meterSizeCode"
                group_label = "ขนาดมิเตอร์"
            elif layer in ("valve", "firehydrant"):
                group_field = "$properties.sizeId"
                group_label = "ขนาด (มม.)"
        elif re.search(r"ชนิด|ประเภท|วัสดุ", text):
            if layer == "pipe":
                group_field = "$properties.typeId"
                group_label = "ชนิดท่อ"
            elif layer == "valve":
                group_field = "$properties.typeId"
                group_label = "ชนิดวาล์ว"
            elif layer == "leakpoint":
                group_field = "$properties.pipeTypeId"
                group_label = "ชนิดท่อ"
        elif re.search(r"สถานะ", text):
            if layer in ("valve", "firehydrant"):
                group_field = "$properties.statusId"
                group_label = "สถานะ"
            elif layer == "meter":
                group_field = "$properties.custStat"
                group_label = "สถานะลูกค้า"
            elif layer == "leakpoint":
                group_field = "$properties.LeakStatus"
                group_label = "สถานะ"
        elif re.search(r"หน้าที่|ฟังก์ชัน", text):
            if layer == "pipe":
                group_field = "$properties.functionId"
                group_label = "หน้าที่ท่อ"
            elif layer == "valve":
                group_field = "$properties.functionId"
                group_label = "หน้าที่ประตูน้ำ"
        elif re.search(r"เกรด|ชั้น|class", text):
            if layer == "pipe":
                group_field = "$properties.classId"
                group_label = "ชั้นท่อ"
        elif re.search(r"สาขา", text):
            group_field = "$properties.pwaCode"
            group_label = "สาขา"
        elif re.search(r"สาเหตุ", text) and layer == "leakpoint":
            group_field = "$properties.cause"
            group_label = "สาเหตุ"
        elif re.search(r"ผลิตภัณฑ์|ยี่ห้อ", text) and layer == "pipe":
            group_field = "$properties.productId"
            group_label = "ผลิตภัณฑ์"

        if group_field:
            match_stage = _build_match(effective_pwa, pipe_type if layer == "pipe" else None)

            # If grouping by sาขา + pipe, also sum length
            extra_accum = {}
            if group_label == "สาขา" and layer == "pipe":
                extra_accum["ความยาวรวม"] = {"$sum": {"$toDouble": "$properties.length"}}

            group_stage = {"$group": {"_id": group_field, "จำนวน": {"$sum": 1}}}
            group_stage["$group"].update(extra_accum)

            project_stage = {"$project": {"_id": 0, group_label: "$_id", "จำนวน": "$count"}}
            if extra_accum:
                project_stage = {"$project": {"_id": 0, group_label: "$_id", "จำนวน": 1, "ความยามรวม": 1}}

            pipeline = [
                match_stage,
                group_stage,
                {"$project": {"_id": 0, group_label: "$_id", "จำนวน": "$จำนวน"}},
                {"$sort": {"จำนวน": -1}},
            ]
            return {
                "text_response": "กำลังแยก{}ตาม{}ค่ะ".format(layer_label, group_label),
                "target_db": "mongo",
                "response_type": "table",
                "intent_summary": "Group {} by {}".format(layer, group_label),
                "query": {
                    "mongo": {
                        "pwa_code": effective_pwa or None,
                        "layer": layer,
                        "pipeline": pipeline,
                        "operation": "aggregate",
                    }
                },
                "_rule_matched": "group_by",
            }

    # ─────────────────────────────────────────────────────
    # SHOW POSITION — "แสดงตำแหน่งหัวดับเพลิง"
    # "แสดงท่อชนิด HDPE ขนาด 100 ขึ้นไป"
    # "แสดงตำแหน่งมาตรวัดน้ำอายุ 20 ปีขึ้นไป"
    # ─────────────────────────────────────────────────────
    if is_show_position and not is_count and not is_group:
        extra = {}

        # Age filter for pipes (yearInstall) and meters (beginCustDate)
        if age:
            cutoff_year = datetime.now().year + 543 - age  # พ.ศ.
            if layer == "pipe":
                extra["properties.yearInstall"] = {"$lte": cutoff_year}
            elif layer == "meter":
                cutoff_date = "{}-01-01T00:00:00Z".format(datetime.now().year - age)
                extra["properties.beginCustDate"] = {"$lte": cutoff_date}

        match_stage = _build_match(
            effective_pwa,
            pipe_type if layer == "pipe" else None,
            pipe_func_id if layer == "pipe" else None,
            size, size_gte, extra
        )

        desc_parts = [layer_label]
        if pipe_type:
            desc_parts.append("ชนิด {}".format(pipe_type))
        if pipe_func_label:
            desc_parts.append(pipe_func_label)
        if size_gte:
            desc_parts.append("ขนาด {} มม. ขึ้นไป".format(size_gte))
        elif size:
            desc_parts.append("ขนาด {} มม.".format(size))
        if age:
            desc_parts.append("อายุ {} ปีขึ้นไป".format(age))

        return {
            "text_response": "กำลังค้นหาตำแหน่ง{}ค่ะ".format(" ".join(desc_parts)),
            "target_db": "mongo",
            "response_type": "geojson",
            "intent_summary": "Show {} positions".format(layer),
            "query": {
                "mongo": {
                    "pwa_code": effective_pwa or None,
                    "layer": layer,
                    "pipeline": [match_stage["$match"]] if match_stage["$match"] else [{}],
                    "operation": "find",
                }
            },
            "_rule_matched": "show_position",
        }

    # ─────────────────────────────────────────────────────
    # COUNT with type + size filter — "ท่อ AC ขนาด 100 กี่ท่อ"
    # ─────────────────────────────────────────────────────
    if is_count and (size or size_gte or pipe_type) and layer:
        extra = {}
        if age and layer == "pipe":
            cutoff_year = datetime.now().year + 543 - age
            extra["properties.yearInstall"] = {"$lte": cutoff_year}

        match_stage = _build_match(
            effective_pwa,
            pipe_type if layer == "pipe" else None,
            pipe_func_id if layer == "pipe" else None,
            size, size_gte, extra
        )

        desc_parts = [layer_label]
        if pipe_type:
            desc_parts.append("ชนิด {}".format(pipe_type))
        if size_gte:
            desc_parts.append("ขนาด {} มม. ขึ้นไป".format(size_gte))
        elif size:
            desc_parts.append("ขนาด {} มม.".format(size))

        return {
            "text_response": "กำลังนับจำนวน {} ค่ะ".format(" ".join(desc_parts)),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} with filters".format(layer),
            "query": {
                "mongo": {
                    "pwa_code": effective_pwa or None,
                    "layer": layer,
                    "pipeline": [match_stage["$match"]],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_with_filter",
        }

    # ─────────────────────────────────────────────────────
    # COUNT with age — "ท่ออายุ 10 ปีขึ้นไป กี่ท่อ"
    # ─────────────────────────────────────────────────────
    if is_count and age and layer:
        extra = {}
        if layer == "pipe":
            cutoff_year = datetime.now().year + 543 - age
            extra["properties.yearInstall"] = {"$lte": cutoff_year}
        elif layer == "meter":
            cutoff_date = "{}-01-01T00:00:00Z".format(datetime.now().year - age)
            extra["properties.beginCustDate"] = {"$lte": cutoff_date}

        match_stage = _build_match(effective_pwa, extra=extra)

        return {
            "text_response": "กำลังนับจำนวน{} อายุ {} ปีขึ้นไปค่ะ".format(layer_label, age),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} older than {} years".format(layer, age),
            "query": {
                "mongo": {
                    "pwa_code": effective_pwa or None,
                    "layer": layer,
                    "pipeline": [match_stage["$match"]],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_with_age",
        }

    # ─────────────────────────────────────────────────────
    # COUNT with date — "จุดแตกรั่วปี 2567"
    # ─────────────────────────────────────────────────────
    if is_count and (year or month) and layer:
        filt = {}
        if effective_pwa:
            filt["properties.pwaCode"] = effective_pwa

        # Choose date field based on layer
        date_field = "properties.leakDatetime" if layer == "leakpoint" else "properties.recordDate"

        date_desc = ""
        if year and month:
            start = "{}-{:02d}-01T00:00:00Z".format(year, month)
            if month == 12:
                end = "{}-01-01T00:00:00Z".format(year + 1)
            else:
                end = "{}-{:02d}-01T00:00:00Z".format(year, month + 1)
            filt[date_field] = {"$gte": start, "$lt": end}
            date_desc = "เดือน {}/{} ".format(month, year + 543)
        elif year:
            filt[date_field] = {
                "$gte": "{}-01-01T00:00:00Z".format(year),
                "$lt": "{}-01-01T00:00:00Z".format(year + 1),
            }
            date_desc = "ปี {} ".format(year + 543)
        elif month:
            cy = datetime.now().year
            start = "{}-{:02d}-01T00:00:00Z".format(cy, month)
            if month == 12:
                end = "{}-01-01T00:00:00Z".format(cy + 1)
            else:
                end = "{}-{:02d}-01T00:00:00Z".format(cy, month + 1)
            filt[date_field] = {"$gte": start, "$lt": end}
            date_desc = "เดือน {} ".format(month)

        return {
            "text_response": "กำลังนับจำนวน{} {}ค่ะ".format(layer_label, date_desc),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} in {}".format(layer, date_desc.strip()),
            "query": {
                "mongo": {
                    "pwa_code": effective_pwa or None,
                    "layer": layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_with_date",
        }

    # ─────────────────────────────────────────────────────
    # SIMPLE COUNT ALL — "จำนวนมาตรทั้งหมด"
    # ─────────────────────────────────────────────────────
    if is_count and layer:
        filt = {}
        if effective_pwa:
            filt["properties.pwaCode"] = effective_pwa

        return {
            "text_response": "กำลังนับจำนวน{}ทั้งหมดค่ะ".format(layer_label),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count all {}".format(layer),
            "query": {
                "mongo": {
                    "pwa_code": effective_pwa or None,
                    "layer": layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_all",
        }

    # No pattern matched
    return None
