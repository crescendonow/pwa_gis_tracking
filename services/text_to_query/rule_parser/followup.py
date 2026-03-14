"""
Follow-up parser — modify a previous query with additional filters.
E.g., after "ความยาวท่อ AC สาขาเชียงใหม่", user says "ขนาด 100 มม.ขึ้นไป"
"""

from datetime import datetime

from .detectors import (
    detect_layer, detect_size, detect_size_gte, detect_pipe_type,
    detect_pipe_function, detect_meter_status, detect_inch_size,
    detect_age, detect_year_range, detect_exclude_sleeve,
)


def parse_followup(text, prev_context):
    """
    Parse a follow-up query that modifies the previous query context.

    prev_context: dict with layer, pwa_code, operation, pipeline, response_type, timestamp
    Returns: intent dict (same format as parse_rule) or None.
    """
    if not prev_context:
        return None

    layer = prev_context.get("layer", "")
    if not layer:
        return None

    # Don't follow up if text explicitly mentions a DIFFERENT layer
    detected_layer, _ = detect_layer(text)
    if detected_layer and detected_layer != layer:
        return None

    # ── Detect modifiers from text ──────────────────────
    size = detect_size(text)
    size_gte = detect_size_gte(text)
    pipe_type = detect_pipe_type(text) if layer == "pipe" else None
    pipe_func_id, pipe_func_label = detect_pipe_function(text) if layer == "pipe" else (None, None)
    meter_stat_id, meter_stat_label = detect_meter_status(text) if layer == "meter" else (None, None)
    inch_size = detect_inch_size(text)
    age = detect_age(text)
    year_range = detect_year_range(text) if layer == "pipe" else None
    exclude_sleeve = detect_exclude_sleeve(text) if layer == "pipe" else False

    if inch_size and not size:
        size = inch_size
    if inch_size and not size_gte:
        size_gte = inch_size

    # Disambiguate age vs size
    if age and size_gte and str(age) == size_gte:
        size_gte = None
    if age and size and str(age) == size:
        size = None

    # Must detect at least one modifier
    if not any([size, size_gte, pipe_type, pipe_func_id, meter_stat_id, age, year_range, exclude_sleeve]):
        return None

    # ── Extract previous $match and merge new filters ───
    prev_pipeline = list(prev_context.get("pipeline", []))
    operation = prev_context.get("operation", "count")

    if operation == "aggregate" and prev_pipeline:
        if "$match" in prev_pipeline[0]:
            prev_match = dict(prev_pipeline[0]["$match"])
        else:
            prev_match = dict(prev_pipeline[0])
    elif prev_pipeline:
        prev_match = dict(prev_pipeline[0])
    else:
        prev_match = {}

    # Add new filters
    if pipe_type:
        prev_match["properties.typeId"] = pipe_type
    if pipe_func_id:
        prev_match["properties.functionId"] = pipe_func_id
    if size and not size_gte:
        prev_match["properties.sizeId"] = size
    if size_gte:
        prev_match["$expr"] = {"$gte": [{"$toInt": "$properties.sizeId"}, int(size_gte)]}
    if meter_stat_id:
        prev_match["properties.custStat"] = meter_stat_id
    if age and layer == "pipe":
        cutoff_year = datetime.now().year + 543 - age
        prev_match["properties.yearInstall"] = {"$lte": cutoff_year}
    if year_range and layer == "pipe":
        prev_match["properties.yearInstall"] = year_range
    if exclude_sleeve:
        prev_match["properties.functionId"] = {"$ne": "6"}

    # Rebuild pipeline
    if operation == "aggregate":
        new_pipeline = [{"$match": prev_match}] + prev_pipeline[1:]
    else:
        new_pipeline = [prev_match]

    # ── Build description ───────────────────────────────
    desc_parts = []
    if pipe_type:
        desc_parts.append("ชนิด {}".format(pipe_type))
    if pipe_func_label:
        desc_parts.append(pipe_func_label)
    if size_gte:
        desc_parts.append("ขนาด {} มม. ขึ้นไป".format(size_gte))
    elif size:
        desc_parts.append("ขนาด {} มม.".format(size))
    if meter_stat_label:
        desc_parts.append("สถานะ {}".format(meter_stat_label))
    if age:
        desc_parts.append("อายุ {} ปีขึ้นไป".format(age))
    if exclude_sleeve:
        desc_parts.append("ไม่รวมท่อปลอก")

    return {
        "text_response": "ต่อจากคำถามก่อนหน้า — เพิ่มเงื่อนไข {} ค่ะ".format(" ".join(desc_parts)),
        "target_db": "mongo",
        "response_type": prev_context.get("response_type", "numeric"),
        "intent_summary": "Follow-up: add {} filter on {}".format(" ".join(desc_parts), layer),
        "query": {
            "mongo": {
                "pwa_code": prev_context.get("pwa_code") or None,
                "layer": layer,
                "pipeline": new_pipeline,
                "operation": operation,
            }
        },
        "_rule_matched": "followup",
    }
