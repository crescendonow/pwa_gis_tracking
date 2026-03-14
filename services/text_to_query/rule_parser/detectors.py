"""
Detector functions — extract structured information from Thai text.
Each detector returns a value if found, or None/False.
"""

import re
from datetime import datetime

from .mappings import (
    LAYER_KEYWORDS, _SORTED_LAYER_KW,
    PIPE_TYPES, _SORTED_PIPE_TYPE_KW,
    PIPE_FUNCTIONS, _SORTED_PIPE_FUNC_KW,
    METER_STATUS_KW, _SORTED_METER_STATUS_KW,
    _THAI_MONTHS, _SORTED_MONTH_KW,
)


def detect_layer(text):
    """Detect which GIS layer the user is asking about."""
    lower = text.lower()
    for kw in _SORTED_LAYER_KW:
        if kw in lower:
            return LAYER_KEYWORDS[kw]
    return None, None


def detect_pipe_type(text):
    """Detect pipe type (AC, HDPE, PVC, etc.)."""
    lower = text.lower()
    for kw in _SORTED_PIPE_TYPE_KW:
        if kw in lower:
            return PIPE_TYPES[kw]
    return None


def detect_pipe_function(text):
    """Detect pipe function (ท่อส่งน้ำ, ท่อจ่ายน้ำ, etc.)."""
    for kw in _SORTED_PIPE_FUNC_KW:
        if kw in text:
            return PIPE_FUNCTIONS[kw]
    return None, None


def detect_meter_status(text):
    """Detect meter status filter (มาตรตาย, มาตรปกติ, etc.)."""
    for kw in _SORTED_METER_STATUS_KW:
        if kw in text:
            return METER_STATUS_KW[kw]
    return None, None


def detect_inch_size(text):
    """Detect size in inches and convert to mm (e.g. '4 นิ้ว' → 100)."""
    m = re.search(r"(\d+)\s*นิ้ว", text)
    if m:
        inches = int(m.group(1))
        return str(int(inches * 25.4))
    return None


def detect_year_range(text):
    """Detect year-based filter (e.g. 'ก่อนปี 2560', 'หลังปี 2565')."""
    m = re.search(r"ก่อน\s*(?:ปี\s*)?(?:พ\.?ศ\.?\s*)?(\d{4})", text)
    if m:
        year = int(m.group(1))
        if year < 2400:
            year += 543
        return {"$lte": year}
    m = re.search(r"(?:หลัง|ตั้งแต่ปี)\s*(?:พ\.?ศ\.?\s*)?(\d{4})", text)
    if m:
        year = int(m.group(1))
        if year < 2400:
            year += 543
        return {"$gte": year}
    return None


def detect_exclude_sleeve(text):
    """Detect if user wants to exclude ท่อปลอก (functionId != '6')."""
    return bool(re.search(r"ไม่รวมท่อปลอก|ไม่นับท่อปลอก|ยกเว้นท่อปลอก", text))


def detect_large_pipe(text):
    """Detect 'ท่อขนาดใหญ่' keyword → size >= 400."""
    return bool(re.search(r"ท่อขนาดใหญ่|ท่อใหญ่", text))


def detect_size(text):
    """Extract pipe/meter size from text (e.g. '100 มม.', 'ขนาด 200')."""
    m = re.search(r"ขนาด\s*(\d+)\s*(?:มม\.?|มิลลิเมตร)?", text)
    if m:
        return m.group(1)
    m = re.search(r"(\d+)\s*(?:มม\.?|มิลลิเมตร)", text)
    if m:
        return m.group(1)
    return None


def detect_size_gte(text):
    """Detect size with '>=' comparison (e.g. 'ขนาด 100 ขึ้นไป', 'ตั้งแต่ 100')."""
    m = re.search(r"ขนาด\s*(\d+)\s*(?:มม\.?\s*)?(?:ขึ้นไป|ขึ้น)", text)
    if m:
        return m.group(1)
    m = re.search(r"ตั้งแต่\s*(?:ขนาด\s*)?(\d+)", text)
    if m:
        return m.group(1)
    m = re.search(r"(?:มากกว่า|เกิน|เกินกว่า)\s*(\d+)", text)
    if m:
        num_end = m.end()
        after = text[num_end:num_end + 10]
        if re.search(r"อายุ", text[:m.start() + 10]) and re.search(r"ปี", after):
            return None
        return m.group(1)
    return None


def detect_size_lt(text):
    """Detect size with '<' comparison (e.g. 'ขนาดต่ำกว่า 100', 'ขนาดน้อยกว่า 100')."""
    m = re.search(r"(?:ขนาด)?(?:ต่ำกว่า|น้อยกว่า|เล็กกว่า|ไม่เกิน|ไม่ถึง)\s*(\d+)", text)
    if m:
        return m.group(1)
    return None


def detect_fiscal_year(text):
    """Detect fiscal year (ปีงบประมาณ) — runs Oct(year-1) to Sep(year) in Buddhist era."""
    m = re.search(r"ปีงบ(?:ประมาณ)?\s*(?:พ\.?ศ\.?\s*)?(\d{4})", text)
    if m:
        be_year = int(m.group(1))
        if be_year > 2400:
            ce_year = be_year - 543
        else:
            ce_year = be_year
        start = "{}-10-01T00:00:00Z".format(ce_year - 1)
        end = "{}-10-01T00:00:00Z".format(ce_year)
        return start, end, be_year
    return None, None, None


def detect_age(text):
    """Detect age-based filter (e.g. 'อายุ 10 ปีขึ้นไป', 'เก่ากว่า 20 ปี')."""
    m = re.search(r"อายุ\s*(?:เกิน|มากกว่า)?\s*(\d+)\s*ปี", text)
    if m:
        return int(m.group(1))
    m = re.search(r"เก่า(?:กว่า|เกิน)?\s*(\d+)\s*ปี", text)
    if m:
        return int(m.group(1))
    return None


def detect_year(text):
    """Extract year from text. Supports both พ.ศ. and ค.ศ."""
    m = re.search(r"(?:พ\.?ศ\.?\s*)?(\d{4})", text)
    if m:
        year = int(m.group(1))
        if year > 2400:
            year -= 543
        return year
    return None


def detect_month(text):
    """Detect Thai month name in text, return month number."""
    for kw in _SORTED_MONTH_KW:
        if kw in text:
            return _THAI_MONTHS[kw]
    return None


def detect_postgis_zone(text):
    """Detect PostGIS zone query (e.g. 'เขต 2', 'โซน 3')."""
    m = re.search(r"(?:เขต|โซน)\s*(\d+)", text)
    if m:
        return m.group(1)
    return None


def detect_unit_km(text):
    """Detect if user wants result in kilometers."""
    return bool(re.search(r"กิโลเมตร|กม\.?|km", text, re.IGNORECASE))


class ParseContext:
    """Holds all detected features from a user prompt."""

    def __init__(self, text, pwa_code=""):
        self.text = text
        self.pwa_code = pwa_code

        # Layer
        self.layer, self.layer_label = detect_layer(text)

        # Boolean flags
        self.is_count = bool(re.search(
            r"จำนวน|ทั้งหมด|(?:มี|รวม)?กี่(?:เครื่อง|ตัว|จุด|หลัง|ท่อ|แห่ง|อัน|รายการ)?",
            text
        ))
        self.is_group = bool(re.search(r"แยกตาม|แบ่งตาม|จำแนกตาม|รายสาขา", text))
        self.is_show_position = bool(re.search(
            r"แสดง|ดู|ตำแหน่ง|ที่ตั้ง|แผนที่|อยู่ที่ไหน|อยู่ตรงไหน|พิกัด|ข้อมูล", text
        ))
        self.is_total_length = bool(re.search(
            r"(?:ความยาว|ยาว).*(?:รวม|ทั้งหมด|ทั้งประเทศ)|รวม.*(?:กี่เมตร|เมตร|กม\.?|กิโลเมตร)",
            text
        ))
        self.is_pipe_total = self.layer == "pipe" and bool(re.search(
            r"รวม.*(?:เมตร|กม|กิโล)|ยาว.*(?:รวม|ทั้งหมด|กี่)", text
        ))

        # Zone
        self.zone = detect_postgis_zone(text)

        # Nationwide
        self.is_nationwide = bool(re.search(r"ทั้งประเทศ|ทุกสาขา|ทั้งหมดทุก|รวมทุกเขต", text))
        if not self.is_nationwide and re.search(r"ทั้งหมด", text):
            if not re.search(r"สาขา", text) and not self.zone and not pwa_code:
                self.is_nationwide = True

        # Sizes
        self.size = detect_size(text)
        self.size_gte = detect_size_gte(text)
        self.size_lt = detect_size_lt(text)

        # Time
        self.year = detect_year(text)
        self.month = detect_month(text)
        self.age = detect_age(text)
        self.fiscal_start, self.fiscal_end, self.fiscal_year = detect_fiscal_year(text)

        # Layer-specific
        self.pipe_type = detect_pipe_type(text) if self.layer in ("pipe", "leakpoint") else None
        self.pipe_func_id, self.pipe_func_label = (
            detect_pipe_function(text) if self.layer == "pipe" else (None, None)
        )
        self.want_km = detect_unit_km(text)
        self.meter_stat_id, self.meter_stat_label = (
            detect_meter_status(text) if self.layer == "meter" else (None, None)
        )
        self.inch_size = detect_inch_size(text)
        self.year_range = detect_year_range(text) if self.layer == "pipe" else None
        self.exclude_sleeve = detect_exclude_sleeve(text) if self.layer == "pipe" else False
        self.is_large_pipe = detect_large_pipe(text) if self.layer == "pipe" else False

        # ── Post-processing ───────────────────────────────
        # Inch → mm conversion
        if self.inch_size and not self.size:
            self.size = self.inch_size
        if self.inch_size and not self.size_gte:
            self.size_gte = self.inch_size

        # "ท่อขนาดใหญ่" → size >= 400
        if self.is_large_pipe and not self.size_gte:
            self.size_gte = "400"

        # Disambiguate: if age detected, don't treat same number as size
        if self.age and self.size_gte and str(self.age) == self.size_gte:
            self.size_gte = None
        if self.age and self.size and str(self.age) == self.size:
            self.size = None

        # Effective pwa_code (clear if nationwide)
        self.effective_pwa = "" if self.is_nationwide else pwa_code
