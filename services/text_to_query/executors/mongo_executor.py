"""
MongoDB query executor — mirrors Go FindCollectionID logic.
READ ONLY: find, aggregate, countDocuments only.
"""

import json
import logging

from bson import ObjectId
from config import mongo_db

log = logging.getLogger("text_to_query")


def _find_collection_id(pwa_code, layer):
    """
    Look up the MongoDB collection ObjectID from the 'collections'
    metadata collection using alias format: b{pwaCode}_{featureType}
    Mirrors Go service FindCollectionID logic.
    """
    if mongo_db is None:
        raise RuntimeError("MongoDB not connected")

    code = pwa_code
    if not code.startswith("b"):
        code = "b" + code
    alias = "{}_{}".format(code, layer)

    meta = mongo_db["collections"].find_one({"alias": alias})
    if not meta:
        raise ValueError("Collection not found: {}".format(alias))

    return str(meta["_id"])


def execute_mongo(pwa_code, layer, operation, pipeline, response_type):
    """
    Execute a MongoDB query and return formatted result.

    Args:
        pwa_code: PWA branch code (e.g. '1020')
        layer: Layer name (e.g. 'pipe', 'firehydrant')
        operation: 'find', 'aggregate', or 'count'
        pipeline: List of filter/stages
        response_type: 'geojson', 'numeric', or 'table'

    Returns:
        dict with result data
    """
    if mongo_db is None:
        raise RuntimeError("MongoDB not connected")

    if not pwa_code or not layer:
        raise ValueError("pwa_code and layer are required for MongoDB queries")

    # Resolve collection
    collection_id = _find_collection_id(pwa_code, layer)
    col_name = "features_{}".format(collection_id)
    collection = mongo_db[col_name]
    log.info("MongoDB: %s.%s (alias=b%s_%s) op=%s", "vallaris_feature", col_name, pwa_code, layer, operation)

    if operation == "count":
        filt = pipeline[0] if pipeline else {}
        count = collection.count_documents(filt)
        return {
            "value": count,
            "label": "จำนวน {} ทั้งหมด".format(_layer_thai(layer)),
            "unit": _layer_unit(layer),
        }

    elif operation == "aggregate":
        cursor = collection.aggregate(pipeline)
        rows = list(cursor)

        # Check if result is numeric (single group result)
        if len(rows) == 1 and "_id" in rows[0]:
            row = rows[0]
            # Find the first non-_id field as the value
            for key, val in row.items():
                if key != "_id":
                    return {
                        "value": val if isinstance(val, (int, float)) else str(val),
                        "label": key,
                        "unit": "",
                    }

        if response_type == "table":
            if rows:
                columns = [k for k in rows[0].keys() if k != "_id"]
                return {
                    "columns": columns,
                    "rows": [{k: _safe_val(v) for k, v in r.items() if k != "_id"} for r in rows[:200]],
                    "row_count": len(rows),
                }
            return {"columns": [], "rows": [], "row_count": 0}

        # Numeric aggregate
        if rows:
            row = rows[0]
            for key, val in row.items():
                if key != "_id":
                    return {"value": val, "label": key, "unit": ""}
        return {"value": 0, "label": "ไม่พบข้อมูล", "unit": ""}

    else:
        # find operation
        filt = pipeline[0] if pipeline else {}
        cursor = collection.find(filt).limit(1000)
        docs = list(cursor)

        if response_type == "geojson":
            features = []
            for doc in docs:
                geom = doc.get("geometry")
                if not geom:
                    continue
                props = doc.get("properties", {})
                safe_props = {}
                for k, v in props.items():
                    safe_props[k] = _safe_val(v)
                features.append({
                    "type": "Feature",
                    "geometry": geom,
                    "properties": safe_props,
                })
            return {
                "type": "FeatureCollection",
                "features": features,
            }

        elif response_type == "table":
            rows = []
            columns = set()
            for doc in docs:
                props = doc.get("properties", {})
                row = {}
                for k, v in props.items():
                    row[k] = _safe_val(v)
                    columns.add(k)
                rows.append(row)
            return {
                "columns": sorted(columns),
                "rows": rows[:200],
                "row_count": len(docs),
            }

        elif response_type == "numeric":
            return {
                "value": len(docs),
                "label": "จำนวน {} ที่พบ".format(_layer_thai(layer)),
                "unit": _layer_unit(layer),
            }

        # Default: return as geojson
        features = []
        for doc in docs:
            geom = doc.get("geometry")
            if not geom:
                continue
            props = {k: _safe_val(v) for k, v in doc.get("properties", {}).items()}
            features.append({"type": "Feature", "geometry": geom, "properties": props})
        return {"type": "FeatureCollection", "features": features}


def _safe_val(v):
    """Convert MongoDB values to JSON-safe types."""
    if isinstance(v, ObjectId):
        return str(v)
    if isinstance(v, bytes):
        return v.hex()
    if hasattr(v, "isoformat"):
        return v.isoformat()
    if isinstance(v, (dict, list)):
        return json.dumps(v, ensure_ascii=False, default=str)
    return v


LAYER_THAI = {
    "pipe": "ท่อประปา",
    "valve": "ประตูน้ำ",
    "firehydrant": "หัวดับเพลิง",
    "meter": "มาตรวัดน้ำ",
    "bldg": "อาคาร",
    "leakpoint": "จุดแตกรั่ว",
    "pwa_waterworks": "สำนักงาน",
    "struct": "รั้วบ้าน",
    "pipe_serv": "ท่อบริการ",
}

LAYER_UNIT = {
    "pipe": "ท่อ",
    "valve": "ตัว",
    "firehydrant": "จุด",
    "meter": "เครื่อง",
    "bldg": "หลัง",
    "leakpoint": "จุด",
    "pwa_waterworks": "แห่ง",
    "struct": "แห่ง",
    "pipe_serv": "ท่อ",
}


def _layer_thai(layer):
    return LAYER_THAI.get(layer, layer)


def _layer_unit(layer):
    return LAYER_UNIT.get(layer, "รายการ")
