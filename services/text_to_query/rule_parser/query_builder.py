"""Helper to build MongoDB $match stages (ชนิดข้อมูลตรงกับ DB จริง)."""

from .mappings.fields import size_field, type_field, is_numeric_size, size_value


def build_match(pwa_code, pipe_type=None, pipe_func_id=None, size=None,
                size_gte=None, size_lt=None, extra=None, layer=None):
    """สร้าง $match stage — ใช้การเปรียบเทียบตัวเลขตรง ๆ (ใช้ index ได้ / ไม่ throw)"""
    match = {}
    if pwa_code:
        match["properties.pwaCode"] = pwa_code

    if pipe_type:
        tf = type_field(layer)
        if tf:
            match[tf] = pipe_type

    if pipe_func_id is not None:
        # functionId เก็บเป็น int ใน DB
        match["properties.functionId"] = int(pipe_func_id)

    sf = size_field(layer)
    if sf:
        if is_numeric_size(layer):
            rng = {}
            if size_gte is not None:
                rng["$gte"] = int(size_gte)
            if size_lt is not None:
                rng["$lt"] = int(size_lt)
            if rng:
                match[sf] = rng
            elif size is not None:
                match[sf] = size_value(layer, size)
        else:
            # meter: meterSizeCode เป็น string — รองรับเฉพาะค่าเท่ากับ
            if size is not None and not size_gte and not size_lt:
                match[sf] = size_value(layer, size)

    if extra:
        match.update(extra)
    return {"$match": match} if match else {"$match": {}}
