"""
Response formatting — unified envelope for all response types.
"""


def format_response(
    status,
    response_type,
    query_display,
    result,
    target_db,
    text_response="",
    layer="",
    pwa_code="",
    execution_time_ms=0,
    cached=False,
    model="",
):
    """
    Build the unified response envelope.

    Response types:
    - geojson: result is a FeatureCollection
    - numeric: result has value, label, unit
    - table: result has columns, rows, row_count
    """
    return {
        "status": status,
        "text_response": text_response,
        "response_type": response_type,
        "query_display": query_display,
        "result": result,
        "metadata": {
            "target_db": target_db,
            "layer": layer,
            "pwa_code": pwa_code,
            "execution_time_ms": execution_time_ms,
            "cached": cached,
            "model": model,
        },
    }
