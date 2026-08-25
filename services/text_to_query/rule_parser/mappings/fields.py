"""Field registry — ชื่อ field + ชนิดข้อมูลจริงในแต่ละ layer (verified 2026-08-22)."""

LAYER_FIELDS = {
    "pipe":           {"size": "sizeId",        "type": "typeId",         "status": None,           "date": "recordDate"},
    "valve":          {"size": "sizeId",        "type": "typeId",         "status": "statusId",     "date": "recordDate"},
    "firehydrant":    {"size": "sizeId",        "type": None,             "status": "statusId",     "date": "recordDate"},
    "meter":          {"size": "meterSizeCode", "type": None,             "status": "custStat",     "date": "recordDate"},
    "bldg":           {"size": None,            "type": "buildingTypeId", "status": "useStatusId",  "date": "recordDate"},
    "leakpoint":      {"size": "pipeSizeId",    "type": "pipeTypeId",     "status": None,           "date": "leakDatetime"},
    "pwa_waterworks": {"size": None,            "type": "pwaStationId",   "status": None,           "date": None},
    "dma_boundary":   {"size": None,            "type": None,             "status": None,           "date": "recordDate"},
    "step_test":      {"size": None,            "type": None,             "status": None,           "date": None},
    "flow_meter":     {"size": "pipeSize",      "type": "pipeType",       "status": None,           "date": None},
    "struct":         {"size": None,            "type": None,             "status": None,           "date": None},
    "pipe_serv":      {"size": None,            "type": None,             "status": None,           "date": None},
}

# layer ที่เก็บ "ขนาด" เป็นตัวเลข (int/double) — เทียบด้วย number ตรง ๆ ได้
NUMERIC_SIZE_LAYERS = {"pipe", "valve", "firehydrant", "leakpoint", "flow_meter"}
# layer ที่เก็บ "ชนิด" เป็นตัวเลข
NUMERIC_TYPE_LAYERS = {"valve", "bldg", "pwa_waterworks"}


def _f(layer, key):
    return (LAYER_FIELDS.get(layer) or {}).get(key)


def size_field(layer, prefixed=True):
    f = _f(layer, "size")
    return ("properties." + f) if (f and prefixed) else f


def type_field(layer, prefixed=True):
    f = _f(layer, "type")
    return ("properties." + f) if (f and prefixed) else f


def status_field(layer, prefixed=True):
    f = _f(layer, "status")
    return ("properties." + f) if (f and prefixed) else f


def date_field(layer, prefixed=True):
    f = _f(layer, "date")
    return ("properties." + f) if (f and prefixed) else f


def is_numeric_size(layer):
    return layer in NUMERIC_SIZE_LAYERS


def size_value(layer, raw):
    """แปลงค่าขนาดให้ตรงชนิดใน DB — pipe/valve/... = int, meter = str"""
    if raw is None:
        return None
    if is_numeric_size(layer):
        try:
            return int(raw)
        except (TypeError, ValueError):
            return None
    return str(raw)
