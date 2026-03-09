"""
PostGIS SQL executor — read-only SQL execution.
"""

import json
import logging
from datetime import date, datetime
from decimal import Decimal

from sqlalchemy import text

from config import pg_engine

log = logging.getLogger("text_to_query")


def execute_postgis(sql, response_type):
    """
    Execute a validated SQL query against PostGIS.

    Args:
        sql: Validated SELECT SQL string
        response_type: 'geojson', 'numeric', or 'table'

    Returns:
        dict with result data
    """
    if pg_engine is None:
        raise RuntimeError("PostgreSQL not connected")

    log.info("PostGIS executing: %s", sql[:200])

    with pg_engine.connect() as conn:
        result = conn.execute(text(sql))
        columns = list(result.keys())
        raw_rows = result.fetchall()

    # Convert rows to dicts with safe values
    rows = []
    for row in raw_rows:
        d = {}
        for i, col in enumerate(columns):
            d[col] = _safe_value(row[i])
        rows.append(d)

    if response_type == "geojson":
        return _build_geojson(rows, columns)
    elif response_type == "numeric":
        return _build_numeric(rows, columns)
    else:
        return _build_table(rows, columns)


def _build_geojson(rows, columns):
    """Build GeoJSON FeatureCollection from rows with a 'geojson' column."""
    features = []
    for row in rows:
        geojson_str = row.get("geojson")
        if not geojson_str:
            continue
        try:
            geom = json.loads(geojson_str) if isinstance(geojson_str, str) else geojson_str
            props = {k: v for k, v in row.items() if k != "geojson"}
            features.append({
                "type": "Feature",
                "geometry": geom,
                "properties": props,
            })
        except (json.JSONDecodeError, TypeError):
            pass

    return {
        "type": "FeatureCollection",
        "features": features,
    }


def _build_numeric(rows, columns):
    """Extract a single numeric value from result."""
    if not rows:
        return {"value": 0, "label": "ไม่พบข้อมูล", "unit": ""}

    row = rows[0]
    # Find first numeric column
    for col in columns:
        val = row.get(col)
        if isinstance(val, (int, float)):
            return {"value": val, "label": col, "unit": ""}

    # Fallback: return row count
    return {"value": len(rows), "label": "จำนวนแถว", "unit": "แถว"}


def _build_table(rows, columns):
    """Build table response."""
    # Filter out geojson column from display
    display_cols = [c for c in columns if c != "geojson"]
    display_rows = [{k: v for k, v in r.items() if k != "geojson"} for r in rows[:200]]

    return {
        "columns": display_cols,
        "rows": display_rows,
        "row_count": len(rows),
    }


def _safe_value(v):
    """Convert database values to JSON-safe types."""
    if isinstance(v, (datetime, date)):
        return v.isoformat()
    if isinstance(v, Decimal):
        return float(v)
    if isinstance(v, memoryview):
        return v.tobytes().hex()
    if isinstance(v, bytes):
        return v.hex()
    return v
