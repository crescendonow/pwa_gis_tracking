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
    rule_matched=None,
    can_retry_llm=False,
):
    """
    Build the unified response envelope.

    Response types:
    - geojson: result is a FeatureCollection
    - numeric: result has value, label, unit
    - table: result has columns, rows, row_count

    rule_matched: ชื่อ pattern ของ rule parser ที่ตอบคำถามนี้ (None ถ้าตอบจาก LLM)
    can_retry_llm: True ถ้าคำตอบมาจาก rule — frontend ใช้ตัดสินใจแสดงปุ่ม "ถามใหม่ด้วย AI"
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
            "rule_matched": rule_matched,
            "can_retry_llm": can_retry_llm,
        },
    }
