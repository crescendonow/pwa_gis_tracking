"""
Integration test — รันคำถามตัวอย่าง 8 ข้อจาก templates/detail.html ผ่าน
parse_rule → execute_mongo / execute_postgis จริง (ไม่ mock)

อ้างอิง: note/18_plan_for_edit_text_to_sql.md ข้อ 5.2

ต้องต่อ MongoDB ได้ (และ PostGIS สำหรับข้อ 4) — ข้ามอัตโนมัติถ้า mongo_db is None
เพื่อไม่พังบนเครื่องที่ไม่มี VPN

Usage:
    cd services/text_to_query
    python -m pytest test_live_mongo.py -v
    OR
    python test_live_mongo.py
"""

import sys
import os

# Ensure the service directory is on the path
sys.path.insert(0, os.path.dirname(__file__))

import pytest

from config import mongo_db, pg_engine
from rule_parser import parse_rule
from branch_resolver import resolve_branch_name, get_all_codes, get_codes_in_zone
from executors.mongo_executor import execute_mongo, execute_mongo_multi
from executors.postgis_executor import execute_postgis
from validators import validate_sql

# ข้ามทั้งไฟล์ถ้าต่อ MongoDB ไม่ได้ (เช่น รันนอก VPN) — ตาม plan ข้อ 5.2
pytestmark = pytest.mark.skipif(
    mongo_db is None, reason="MongoDB not reachable — skip live integration tests"
)


def _run(prompt, pwa_code=""):
    """
    จำลอง flow ของ main.py text_to_query(): resolve branch name จากข้อความ →
    parse_rule → execute_mongo/execute_postgis จริง (ไม่ผ่าน HTTP/FastAPI)
    """
    resolved = resolve_branch_name(prompt)
    if resolved:
        pwa_code = resolved

    intent = parse_rule(prompt, pwa_code)
    assert intent is not None, "parse_rule returned None for: {}".format(prompt)

    intent.pop("_rule_matched", None)
    zone = intent.pop("_zone", None)
    nationwide = intent.pop("_nationwide", False)

    target_db = intent.get("target_db", "mongo")
    response_type = intent.get("response_type", "table")
    query_info = intent.get("query", {})

    if target_db == "postgis":
        sql = query_info.get("postgis", {}).get("sql", "")
        if pg_engine is None:
            pytest.skip("PostGIS not reachable — skip")
        assert validate_sql(sql), "SQL failed validation: {}".format(sql)
        result = execute_postgis(sql, response_type)
    else:
        mongo_q = query_info.get("mongo", {})
        layer = mongo_q.get("layer", "")
        operation = mongo_q.get("operation", "find")
        pipeline = mongo_q.get("pipeline", [])
        code = pwa_code or mongo_q.get("pwa_code", "")

        if nationwide:
            all_codes = [b[0] for b in get_all_codes()]
            result = execute_mongo_multi(all_codes, layer, operation, pipeline, response_type)
        elif zone:
            zone_codes = [b[0] for b in get_codes_in_zone(zone)]
            result = execute_mongo_multi(zone_codes, layer, operation, pipeline, response_type)
        else:
            result = execute_mongo(code, layer, operation, pipeline, response_type)

    return response_type, result


# ═══════════════════════════════════════════════════════════
# 8 คำถามตัวอย่างจาก templates/detail.html (note/18_plan_for_edit_text_to_sql.md ข้อ 5.2)
# ═══════════════════════════════════════════════════════════

def test_q1_show_firehydrant_nongruea():
    """1. แสดงตำแหน่งหัวดับเพลิงทั้งหมดสาขาหนองเรือ → geojson 438 features"""
    response_type, result = _run("แสดงตำแหน่งหัวดับเพลิงทั้งหมดสาขาหนองเรือ")
    assert response_type == "geojson"
    assert result.get("type") == "FeatureCollection"
    n = len(result.get("features", []))
    assert n == 438, "Expected 438 features, got {}".format(n)


def test_q2_count_meter_chiangmai():
    """2. จำนวนมาตรวัดน้ำทั้งหมดในสาขาเชียงใหม่ → numeric > 140,000"""
    response_type, result = _run("จำนวนมาตรวัดน้ำทั้งหมดในสาขาเชียงใหม่")
    assert response_type == "numeric"
    val = result.get("value", 0)
    assert val > 140000, "Expected > 140,000, got {}".format(val)


def test_q3_pipe_length_hatyai():
    """3. ท่อขนาด 100 มม. สาขาหาดใหญ่ ยาวรวมกี่เมตร → numeric > 0"""
    response_type, result = _run("ท่อขนาด 100 มม. สาขาหาดใหญ่ ยาวรวมกี่เมตร")
    assert response_type == "numeric"
    val = result.get("value", 0)
    assert val > 0, "Expected > 0, got {}".format(val)


def test_q4_postgis_zone2_branches():
    """4. ขอรายชื่อสาขาทั้งหมดในเขต 2 → table (PostGIS) > 0 rows"""
    if pg_engine is None:
        pytest.skip("PostGIS not reachable")
    response_type, result = _run("ขอรายชื่อสาขาทั้งหมดในเขต 2")
    assert response_type == "table"
    assert result.get("row_count", 0) > 0


def test_q5_count_dead_meter_rangsit():
    """5. จำนวนมาตรตายสาขารังสิต

    ตามที่ระบุใน plan ข้อ 6 Q1: ข้อมูลจริงไม่มีค่า custStat='3' เลยในสาขาที่สุ่มตรวจ (มีแต่ '1','2','4','5','6')
    ดังนั้นคำถามนี้ยังคาดว่าจะได้ 0 จนกว่าทีมข้อมูลจะยืนยันตารางรหัส custStat ที่ถูกต้อง — จึงไม่ assert
    ว่าต้อง > 0 (ต่างจากอีก 7 ข้อ) เพียงตรวจโครงสร้าง response และบันทึกค่าจริงไว้ให้เห็น
    """
    response_type, result = _run("จำนวนมาตรตายสาขารังสิต")
    assert response_type == "numeric"
    val = result.get("value")
    assert isinstance(val, (int, float)) and val >= 0
    print("[INFO] januan matr tai sakha Rangsit = {} (need custStat confirmation, see plan item 6 Q1)".format(val))


def test_q6_leakpoint_group_by_rangsit():
    """6. จำนวนจุดซ่อมท่อขนาด 100 ขึ้นไปแยกตามชนิด ขนาด สาขารังสิต
    → table 42 rows, แถวแรก ชนิดท่อ=PVC, ขนาดท่อ=100, จำนวน=12962
    """
    response_type, result = _run("จำนวนจุดซ่อมท่อขนาด 100 ขึ้นไปแยกตามชนิด ขนาด สาขารังสิต")
    assert response_type == "table"
    assert result.get("row_count") == 42, "Expected 42 rows, got {}".format(result.get("row_count"))
    first = result["rows"][0]
    assert first.get("ชนิดท่อ") == "PVC"
    assert first.get("ขนาดท่อ") == 100
    assert first.get("จำนวน") == 12962


def test_q7_pipe_length_pattaya_old():
    """7. ความยาวท่อจ่ายน้ำขนาด 100 ขึ้นไป อายุมากกว่า 30 ปี สาขาพัทยา → numeric > 0"""
    response_type, result = _run("ความยาวท่อจ่ายน้ำขนาด 100 ขึ้นไป อายุมากกว่า 30 ปี สาขาพัทยา")
    assert response_type == "numeric"
    val = result.get("value", 0)
    assert val > 0, "Expected > 0, got {}".format(val)


def test_q8_show_waterworks_nationwide():
    """8. แสดงตำแหน่งข้อมูลสถานีผลิตโรงกรองน้ำทุกสาขา → geojson > 0 features (ทุกสาขา = nationwide)"""
    response_type, result = _run("แสดงตำแหน่งข้อมูลสถานีผลิตโรงกรองน้ำทุกสาขา")
    assert response_type == "geojson"
    n = len(result.get("features", []))
    assert n > 0, "Expected > 0 features, got {}".format(n)


# ═══════════════════════════════════════════════════════════
# Run standalone (without pytest)
# ═══════════════════════════════════════════════════════════

if __name__ == "__main__":
    import traceback

    if mongo_db is None:
        print("MongoDB not reachable — skipping all live integration tests.")
        sys.exit(0)

    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    passed = failed = skipped = 0
    for fn in tests:
        try:
            fn()
            passed += 1
            print("  PASS  {}".format(fn.__name__))
        except BaseException as exc:
            # pytest.skip() raises a BaseException subclass (not Exception) by design
            if type(exc).__name__ == "Skipped":
                skipped += 1
                print("  SKIP  {} — {}".format(fn.__name__, exc))
                continue
            failed += 1
            print("  FAIL  {}".format(fn.__name__))
            traceback.print_exc()
            print()
    print("\n{} passed, {} failed, {} skipped out of {} tests".format(
        passed, failed, skipped, len(tests)))
    sys.exit(1 if failed else 0)
