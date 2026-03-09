"""
Rule-Based Parser — fast path for common GIS queries.
Matches ~80% of typical user questions via regex, responds < 100ms.
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
    # leakpoint
    "จุดแตกรั่ว": ("leakpoint", "จุดแตกรั่ว"),
    "จุดรั่ว": ("leakpoint", "จุดรั่ว"),
    "น้ำรั่ว": ("leakpoint", "จุดน้ำรั่ว"),
    "แตกรั่ว": ("leakpoint", "จุดแตกรั่ว"),
    # pwa_waterworks
    "สำนักงาน": ("pwa_waterworks", "สำนักงาน"),
    # pipe_serv
    "ท่อบริการ": ("pipe_serv", "ท่อบริการ"),
    "ท่อแยกเข้าบ้าน": ("pipe_serv", "ท่อบริการ"),
}

# Sort keywords longest first so "ท่อประปา" matches before "ท่อ"
_SORTED_LAYER_KW = sorted(LAYER_KEYWORDS.keys(), key=len, reverse=True)


def _detect_layer(text):
    """Detect which GIS layer the user is asking about."""
    for kw in _SORTED_LAYER_KW:
        if kw in text:
            return LAYER_KEYWORDS[kw]
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


def _detect_year(text):
    """Extract year from text. Supports both พ.ศ. and ค.ศ."""
    # พ.ศ. year (e.g. 2567, 2566)
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


def parse_rule(prompt, pwa_code=""):
    """
    Try to match the user prompt against known patterns.

    Returns:
        dict (same format as LLM intent) if matched, None otherwise.
    """
    text = prompt.strip()

    # Detect layer
    layer, layer_label = _detect_layer(text)

    # ─────────────────────────────────────────────────────
    # PATTERN 1: Count all — "จำนวน{layer}ทั้งหมด"
    # Keywords: จำนวน, ทั้งหมด, กี่+unit, มีกี่, รวมกี่
    # ─────────────────────────────────────────────────────
    is_count = bool(re.search(
        r"จำนวน|ทั้งหมด|(?:มี|รวม)?กี่(?:เครื่อง|ตัว|จุด|หลัง|ท่อ|แห่ง|อัน|รายการ)?",
        text
    ))
    # Exclude "แยกตาม" or "แบ่งตาม" (those are group-by, not count)
    is_group = bool(re.search(r"แยกตาม|แบ่งตาม|จำแนกตาม", text))
    # Exclude "แสดง/ดู ตำแหน่ง" (those are geojson)
    is_show_position = bool(re.search(r"แสดง|ดู|ตำแหน่ง|ที่ตั้ง|แผนที่|อยู่ที่ไหน|อยู่ตรงไหน", text))
    # Detect total length
    is_total_length = bool(re.search(r"(?:ความยาว|ยาว).*(?:รวม|ทั้งหมด)|รวม.*(?:กี่เมตร|เมตร|กม\.?|กิโลเมตร)", text))
    # Detect "ท่อรวม" meaning total pipe length
    is_pipe_total = layer == "pipe" and bool(re.search(r"รวม.*(?:เมตร|กม|กิโล)|ยาว.*(?:รวม|ทั้งหมด|กี่)", text))

    size = _detect_size(text)
    year = _detect_year(text)
    month = _detect_month(text)

    # ─────────────────────────────────────────────────────
    # PATTERN: PostGIS — "สาขาในเขต X", "รายชื่อสาขาเขต X"
    # ─────────────────────────────────────────────────────
    zone = _detect_postgis_zone(text)
    if zone and re.search(r"สาขา", text):
        return {
            "text_response": "กำลังค้นหารายชื่อสาขาทั้งหมดในเขต {} จากฐานข้อมูล PostGIS ค่ะ".format(zone),
            "target_db": "postgis",
            "response_type": "table",
            "intent_summary": "List all branches in zone {}".format(zone),
            "query": {
                "postgis": {
                    "sql": "SELECT pwa_code, name, zone, ST_AsGeoJSON(wkb_geometry) AS geojson FROM pwa_office.pwa_office234 WHERE zone = '{}' ORDER BY name".format(zone)
                }
            },
            "_rule_matched": "postgis_zone",
        }

    # ─────────────────────────────────────────────────────
    # PATTERN: List all branches — "สาขาทั้งหมด", "รายชื่อสาขา"
    # ─────────────────────────────────────────────────────
    if re.search(r"สาขา", text) and is_count and not layer:
        return {
            "text_response": "กำลังค้นหารายชื่อสาขาทั้งหมดจากฐานข้อมูล PostGIS ค่ะ",
            "target_db": "postgis",
            "response_type": "table",
            "intent_summary": "List all branches",
            "query": {
                "postgis": {
                    "sql": "SELECT pwa_code, name, zone, ST_AsGeoJSON(wkb_geometry) AS geojson FROM pwa_office.pwa_office234 ORDER BY name"
                }
            },
            "_rule_matched": "postgis_all_branches",
        }

    if not layer:
        return None  # Can't determine layer → fallback to LLM

    # ─────────────────────────────────────────────────────
    # PATTERN 2: Total pipe length — "ท่อยาวรวมกี่เมตร"
    # ─────────────────────────────────────────────────────
    if layer == "pipe" and (is_total_length or is_pipe_total):
        pipeline = [
            {"$match": {"properties.pwaCode": pwa_code}} if pwa_code else {"$match": {}},
        ]
        if size:
            pipeline[0]["$match"]["properties.sizeId"] = size

        pipeline.extend([
            {"$group": {
                "_id": None,
                "total_length": {"$sum": {"$toDouble": "$properties.length"}}
            }},
            {"$project": {"_id": 0, "total_length": 1}},
        ])

        size_text = " ขนาด {} มม.".format(size) if size else ""
        return {
            "text_response": "กำลังคำนวณความยาวรวมของท่อประปา{}ทั้งหมดค่ะ".format(size_text),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Total pipe length{}".format(" size {}mm".format(size) if size else ""),
            "query": {
                "mongo": {
                    "pwa_code": pwa_code or None,
                    "layer": "pipe",
                    "pipeline": pipeline,
                    "operation": "aggregate",
                }
            },
            "_rule_matched": "pipe_total_length",
        }

    # ─────────────────────────────────────────────────────
    # PATTERN 3: Group by field — "ท่อแยกตามขนาด"
    # ─────────────────────────────────────────────────────
    if is_group and layer:
        # Determine the grouping field
        group_field = None
        group_label = ""

        if re.search(r"ขนาด", text):
            if layer == "pipe":
                group_field = "$properties.sizeId"
                group_label = "ขนาด (มม.)"
            elif layer == "meter":
                group_field = "$properties.meterSizeCode"
                group_label = "ขนาดมิเตอร์"
            elif layer == "valve":
                group_field = "$properties.sizeId"
                group_label = "ขนาด (มม.)"
            elif layer == "firehydrant":
                group_field = "$properties.sizeId"
                group_label = "ขนาด"
        elif re.search(r"ชนิด|ประเภท|วัสดุ", text):
            if layer == "pipe":
                group_field = "$properties.typeId"
                group_label = "ชนิดท่อ"
            elif layer == "valve":
                group_field = "$properties.typeId"
                group_label = "ชนิดวาล์ว"
        elif re.search(r"สถานะ", text):
            if layer in ("valve", "firehydrant"):
                group_field = "$properties.statusId"
                group_label = "สถานะ"
            elif layer == "meter":
                group_field = "$properties.custStat"
                group_label = "สถานะลูกค้า"
        elif re.search(r"หน้าที่|ฟังก์ชัน", text):
            if layer == "pipe":
                group_field = "$properties.functionId"
                group_label = "หน้าที่ท่อ"
        elif re.search(r"เกรด|ชั้น|class", text):
            if layer == "pipe":
                group_field = "$properties.classId"
                group_label = "ชั้นท่อ"

        if group_field:
            match_stage = {"$match": {"properties.pwaCode": pwa_code}} if pwa_code else {"$match": {}}
            pipeline = [
                match_stage,
                {"$group": {"_id": group_field, "count": {"$sum": 1}}},
                {"$project": {"_id": 0, group_label: "$_id", "จำนวน": "$count"}},
                {"$sort": {"จำนวน": -1}},
            ]
            return {
                "text_response": "กำลังแยก{}ตาม{}ค่ะ".format(layer_label, group_label),
                "target_db": "mongo",
                "response_type": "table",
                "intent_summary": "Group {} by {}".format(layer, group_label),
                "query": {
                    "mongo": {
                        "pwa_code": pwa_code or None,
                        "layer": layer,
                        "pipeline": pipeline,
                        "operation": "aggregate",
                    }
                },
                "_rule_matched": "group_by",
            }

    # ─────────────────────────────────────────────────────
    # PATTERN 4: Show positions — "แสดงตำแหน่งหัวดับเพลิง"
    # ─────────────────────────────────────────────────────
    if is_show_position and not is_count and not is_group:
        filt = {}
        if pwa_code:
            filt["properties.pwaCode"] = pwa_code
        if size:
            filt["properties.sizeId"] = size

        desc_parts = [layer_label]
        if size:
            desc_parts.append("ขนาด {} มม.".format(size))

        return {
            "text_response": "กำลังค้นหาตำแหน่ง{}ค่ะ".format("".join(desc_parts)),
            "target_db": "mongo",
            "response_type": "geojson",
            "intent_summary": "Show {} positions".format(layer),
            "query": {
                "mongo": {
                    "pwa_code": pwa_code or None,
                    "layer": layer,
                    "pipeline": [filt] if filt else [{}],
                    "operation": "find",
                }
            },
            "_rule_matched": "show_position",
        }

    # ─────────────────────────────────────────────────────
    # PATTERN 5: Count with size filter — "ท่อขนาด 100 กี่ท่อ"
    # ─────────────────────────────────────────────────────
    if is_count and size and layer:
        filt = {}
        if pwa_code:
            filt["properties.pwaCode"] = pwa_code
        filt["properties.sizeId"] = size

        return {
            "text_response": "กำลังนับจำนวน{} ขนาด {} มม. ค่ะ".format(layer_label, size),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} with size {}mm".format(layer, size),
            "query": {
                "mongo": {
                    "pwa_code": pwa_code or None,
                    "layer": layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_with_size",
        }

    # ─────────────────────────────────────────────────────
    # PATTERN 6: Count with date filter — "จุดแตกรั่วปี 2567"
    # ─────────────────────────────────────────────────────
    if is_count and (year or month) and layer:
        filt = {}
        if pwa_code:
            filt["properties.pwaCode"] = pwa_code

        date_desc = ""
        if year and month:
            start = "{}-{:02d}-01T00:00:00Z".format(year, month)
            if month == 12:
                end = "{}-01-01T00:00:00Z".format(year + 1)
            else:
                end = "{}-{:02d}-01T00:00:00Z".format(year, month + 1)
            filt["recordDate"] = {"$gte": start, "$lt": end}
            date_desc = "เดือน {}/{} ".format(month, year + 543)
        elif year:
            filt["recordDate"] = {
                "$gte": "{}-01-01T00:00:00Z".format(year),
                "$lt": "{}-01-01T00:00:00Z".format(year + 1),
            }
            date_desc = "ปี {} ".format(year + 543)
        elif month:
            # Current year
            cy = datetime.now().year
            start = "{}-{:02d}-01T00:00:00Z".format(cy, month)
            if month == 12:
                end = "{}-01-01T00:00:00Z".format(cy + 1)
            else:
                end = "{}-{:02d}-01T00:00:00Z".format(cy, month + 1)
            filt["recordDate"] = {"$gte": start, "$lt": end}
            date_desc = "เดือน {} ".format(month)

        return {
            "text_response": "กำลังนับจำนวน{} {}ค่ะ".format(layer_label, date_desc),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} in {}".format(layer, date_desc.strip()),
            "query": {
                "mongo": {
                    "pwa_code": pwa_code or None,
                    "layer": layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_with_date",
        }

    # ─────────────────────────────────────────────────────
    # PATTERN 7: Simple count all — "จำนวนมาตรทั้งหมด"
    # ─────────────────────────────────────────────────────
    if is_count and layer:
        filt = {}
        if pwa_code:
            filt["properties.pwaCode"] = pwa_code

        return {
            "text_response": "กำลังนับจำนวน{}ทั้งหมดค่ะ".format(layer_label),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count all {}".format(layer),
            "query": {
                "mongo": {
                    "pwa_code": pwa_code or None,
                    "layer": layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_all",
        }

    # No pattern matched
    return None
