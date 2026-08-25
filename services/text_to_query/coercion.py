"""
แปลงชนิดข้อมูลใน pipeline ที่ LLM สร้าง ให้ตรงกับที่ MongoDB เก็บจริง
(rule parser สร้าง pipeline ที่ typed ถูกต้องอยู่แล้ว — ฟังก์ชันนี้ใช้เป็น safety net
สำหรับ intent ที่มาจาก LLM เท่านั้น เผื่อ LLM ยังตอบผิดชนิดข้อมูลแม้จะแก้ prompts.py แล้ว)
"""

import re
from datetime import datetime

_ISO = re.compile(r"^\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2})?)?(\.\d+)?Z?$")

# field ที่เป็นตัวเลขใน DB (ไม่ขึ้นกับ layer)
# หมายเหตุ: ไม่ใส่ typeId / pipeTypeId / custStat / meterSizeCode ในนี้โดยตั้งใจ
# เพราะ pipe.typeId, leakpoint.pipeTypeId, meter.* เป็น string จริง (ยกเว้น valve.typeId
# ที่เป็น int แต่แยกไม่ออกจากชื่อ field เฉย ๆ — ปล่อยผ่านไม่แก้ ดีกว่าแก้ผิด)
_NUMERIC_FIELDS = {
    "sizeId", "pipeSizeId", "functionId", "classId", "layingId", "productId",
    "statusId", "typeId_valve", "yearInstall", "useStatusId", "buildingTypeId",
    "useTypeId", "pwaStationId", "roundOpen", "depth", "length", "repairCost",
    "pressure", "averageWaterUsage", "presentWaterUsage",
}
_DATE_FIELDS = {
    "recordDate", "leakDatetime", "repairDatetime", "beginCustDate",
    "promiseDate", "checkDate", "bgnMTRDT",
}
# alias ชื่อ field ที่ LLM มักสะกดผิด
_FIELD_ALIASES = {"pipeSizesId": "pipeSizeId", "LeakStatus": "typeId"}

_NUMERIC_OPS = ("$gte", "$gt", "$lte", "$lt", "$eq", "$ne")
_LIST_OPS = ("$in", "$nin")


def _base_field_name(key):
    """ดึงชื่อ field ตัวสุดท้ายจาก 'properties.pipeSizeId' หรือ '$properties.pipeSizeId' → 'pipeSizeId'"""
    return key.rsplit(".", 1)[-1].lstrip("$")


def _to_number(val):
    """แปลง numeric string → int/float, คืนค่าเดิมถ้าแปลงไม่ได้หรือไม่ใช่ string"""
    if isinstance(val, bool) or not isinstance(val, str):
        return val
    s = val.strip()
    try:
        if re.match(r"^-?\d+$", s):
            return int(s)
        return float(s)
    except ValueError:
        return val


def _to_datetime(val):
    """แปลง ISO string หรือ {'$date': '...'} → datetime, คืนค่าเดิมถ้าแปลงไม่ได้"""
    if isinstance(val, dict) and "$date" in val:
        return _to_datetime(val["$date"])
    if isinstance(val, str) and _ISO.match(val.strip()):
        s = val.strip().rstrip("Z")
        for fmt in ("%Y-%m-%dT%H:%M:%S.%f", "%Y-%m-%dT%H:%M:%S", "%Y-%m-%d %H:%M:%S", "%Y-%m-%d"):
            try:
                return datetime.strptime(s, fmt)
            except ValueError:
                continue
    return val


def _coerce_numeric_value(val):
    """cast ค่าตัวเลข รองรับทั้งค่าตรง ๆ, dict operator ($gte/.../$in) และ list"""
    if isinstance(val, dict):
        out = {}
        for op, v in val.items():
            if op in _NUMERIC_OPS:
                out[op] = _to_number(v)
            elif op in _LIST_OPS and isinstance(v, list):
                out[op] = [_to_number(x) for x in v]
            else:
                out[op] = v
        return out
    if isinstance(val, list):
        return [_to_number(x) for x in val]
    return _to_number(val)


def _coerce_date_value(val):
    """cast ค่าวันที่ รองรับทั้งค่าตรง ๆ และ dict operator ($gte/.../$in)"""
    if isinstance(val, dict):
        out = {}
        for op, v in val.items():
            if op in _NUMERIC_OPS:
                out[op] = _to_datetime(v)
            elif op in _LIST_OPS and isinstance(v, list):
                out[op] = [_to_datetime(x) for x in v]
            else:
                out[op] = v
        return out
    return _to_datetime(val)


def coerce_pipeline(node, layer=""):
    """เดิน pipeline แล้วแก้ (1) ชื่อ field alias (2) string→number (3) string→datetime"""
    if isinstance(node, dict):
        result = {}
        for key, val in node.items():
            new_key = key
            base = _base_field_name(key)

            # (1) แก้ alias field ที่สะกดผิด — ทั้งตำแหน่ง key (เช่น "properties.pipeSizesId")
            if base in _FIELD_ALIASES:
                new_key = key[: len(key) - len(base)] + _FIELD_ALIASES[base]
                base = _FIELD_ALIASES[base]

            new_val = coerce_pipeline(val, layer)

            if base in _NUMERIC_FIELDS:
                new_val = _coerce_numeric_value(new_val)
            elif base in _DATE_FIELDS:
                new_val = _coerce_date_value(new_val)

            result[new_key] = new_val
        return result

    if isinstance(node, list):
        return [coerce_pipeline(item, layer) for item in node]

    if isinstance(node, str) and node.startswith("$"):
        # (1) แก้ alias field ที่สะกดผิด — ตำแหน่งค่าที่ขึ้นต้น $ เช่น "$properties.pipeSizesId"
        base = _base_field_name(node)
        if base in _FIELD_ALIASES:
            return node[: len(node) - len(base)] + _FIELD_ALIASES[base]
        return node

    return node
