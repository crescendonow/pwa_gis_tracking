"""
Test suite for rule_parser — run before & after refactoring to ensure parity.

Usage:
    cd services/text_to_query
    python -m pytest test_rule_parser.py -v
    OR
    python test_rule_parser.py
"""

import sys
import os

# Ensure the service directory is on the path
sys.path.insert(0, os.path.dirname(__file__))

from rule_parser import parse_rule, parse_followup


# ── Helper ────────────────────────────────────────────────
def _assert_rule(prompt, pwa_code="", **expected):
    """Run parse_rule and check expected fields."""
    result = parse_rule(prompt, pwa_code)
    assert result is not None, "parse_rule returned None for: {}".format(prompt)
    for key, val in expected.items():
        actual = result
        for part in key.split("."):
            if isinstance(actual, dict):
                actual = actual.get(part)
            else:
                actual = None
        assert actual == val, "For '{}': expected {}={!r}, got {!r}".format(prompt, key, val, actual)
    return result


def _assert_no_match(prompt, pwa_code=""):
    """Ensure parse_rule returns None (falls through to LLM)."""
    result = parse_rule(prompt, pwa_code)
    assert result is None, "Expected None but got match '{}' for: {}".format(
        result.get("_rule_matched") if result else "?", prompt
    )


# ═══════════════════════════════════════════════════════════
# 1. PostGIS patterns
# ═══════════════════════════════════════════════════════════

def test_postgis_zone():
    r = _assert_rule(
        "รายชื่อสาขาในเขต 2",
        _rule_matched="postgis_zone",
        target_db="postgis",
        response_type="table",
    )
    assert "zone = '2'" in r["query"]["postgis"]["sql"]


def test_postgis_all_branches():
    r = _assert_rule(
        "สาขาทั้งหมดมีกี่สาขา",
        _rule_matched="postgis_all_branches",
        target_db="postgis",
    )


# ═══════════════════════════════════════════════════════════
# 2. Pipe total length
# ═══════════════════════════════════════════════════════════

def test_pipe_total_length_simple():
    r = _assert_rule(
        "ความยาวท่อรวมทั้งหมด",
        _rule_matched="pipe_total_length",
        response_type="numeric",
    )
    mongo = r["query"]["mongo"]
    assert mongo["layer"] == "pipe"
    assert mongo["operation"] == "aggregate"


def test_pipe_total_length_with_type():
    r = _assert_rule(
        "ท่อ AC ยาวรวมกี่เมตร",
        _rule_matched="pipe_total_length",
    )
    pipeline = r["query"]["mongo"]["pipeline"]
    match_stage = pipeline[0]
    match = match_stage.get("$match", match_stage)
    assert match.get("properties.typeId") == "AC"


def test_pipe_total_length_with_size_gte():
    r = _assert_rule(
        "ความยาวท่อ HDPE ขนาด 100 มม. ขึ้นไป รวมทั้งหมด",
        _rule_matched="pipe_total_length",
    )
    pipeline = r["query"]["mongo"]["pipeline"]
    match_stage = pipeline[0]
    match = match_stage.get("$match", match_stage)
    assert match.get("properties.typeId") == "HDPE"


def test_pipe_total_length_km():
    r = _assert_rule(
        "ความยาวท่อรวมกี่กิโลเมตร",
        _rule_matched="pipe_total_length",
    )
    pipeline = r["query"]["mongo"]["pipeline"]
    project = pipeline[-1]
    assert "total_length_km" in project.get("$project", {})


def test_pipe_total_length_with_function():
    r = _assert_rule(
        "ความยาวท่อส่งน้ำรวมทั้งหมด",
        _rule_matched="pipe_total_length",
    )
    pipeline = r["query"]["mongo"]["pipeline"]
    match_stage = pipeline[0]
    match = match_stage.get("$match", match_stage)
    assert match.get("properties.functionId") == "1"


def test_pipe_total_length_exclude_sleeve():
    r = _assert_rule(
        "ความยาวท่อรวมไม่รวมท่อปลอก",
        _rule_matched="pipe_total_length",
    )
    pipeline = r["query"]["mongo"]["pipeline"]
    match_stage = pipeline[0]
    match = match_stage.get("$match", match_stage)
    assert match.get("properties.functionId") == {"$ne": "6"}


# ═══════════════════════════════════════════════════════════
# 3. Group-by patterns
# ═══════════════════════════════════════════════════════════

def test_group_by_pipe_type():
    r = _assert_rule(
        "ท่อแยกตามชนิด",
        _rule_matched="group_by",
        response_type="table",
    )
    mongo = r["query"]["mongo"]
    assert mongo["operation"] == "aggregate"
    assert mongo["layer"] == "pipe"


def test_group_by_pipe_size():
    r = _assert_rule(
        "ท่อแยกตามขนาด",
        _rule_matched="group_by",
    )


def test_group_by_valve_status():
    r = _assert_rule(
        "ประตูน้ำแยกตามสถานะ",
        _rule_matched="group_by",
    )
    assert r["query"]["mongo"]["layer"] == "valve"


def test_group_by_leakpoint_cause():
    r = _assert_rule(
        "จุดแตกรั่วแยกตามสาเหตุ",
        _rule_matched="group_by",
    )
    assert r["query"]["mongo"]["layer"] == "leakpoint"


def test_group_by_compound():
    r = _assert_rule(
        "ท่อแยกตามชนิดและขนาด",
        _rule_matched="group_by",
    )


# ═══════════════════════════════════════════════════════════
# 4. Show position (geojson)
# ═══════════════════════════════════════════════════════════

def test_show_position_simple():
    r = _assert_rule(
        "แสดงตำแหน่งหัวดับเพลิง",
        _rule_matched="show_position",
        response_type="geojson",
    )
    assert r["query"]["mongo"]["layer"] == "firehydrant"
    assert r["query"]["mongo"]["operation"] == "find"


def test_show_position_with_filters():
    r = _assert_rule(
        "แสดงท่อชนิด HDPE ขนาด 100 ขึ้นไป",
        _rule_matched="show_position",
        response_type="geojson",
    )
    assert r["query"]["mongo"]["layer"] == "pipe"


def test_show_position_with_age():
    r = _assert_rule(
        "แสดงตำแหน่งท่ออายุ 20 ปีขึ้นไป",
        _rule_matched="show_position",
        response_type="geojson",
    )


# ═══════════════════════════════════════════════════════════
# 5. Count patterns
# ═══════════════════════════════════════════════════════════

def test_count_meter_status():
    r = _assert_rule(
        "จำนวนมาตรตาย",
        _rule_matched="count_meter_status",
        response_type="numeric",
    )
    assert r["query"]["mongo"]["layer"] == "meter"
    pipeline = r["query"]["mongo"]["pipeline"]
    filt = pipeline[0]
    assert filt.get("properties.custStat") == "3"


def test_count_with_filter():
    r = _assert_rule(
        "ท่อ AC ขนาด 100 มม. มีกี่ท่อ",
        _rule_matched="count_with_filter",
        response_type="numeric",
    )


def test_count_with_age():
    r = _assert_rule(
        "ท่ออายุ 10 ปีขึ้นไป กี่ท่อ",
        _rule_matched="count_with_age",
        response_type="numeric",
    )


def test_count_all():
    r = _assert_rule(
        "จำนวนมาตรวัดน้ำทั้งหมด",
        _rule_matched="count_all",
        response_type="numeric",
    )
    assert r["query"]["mongo"]["layer"] == "meter"


def test_count_fiscal_year():
    r = _assert_rule(
        "จำนวนจุดแตกรั่วปีงบประมาณ 2566",
        _rule_matched="count_fiscal_year",
        response_type="numeric",
    )
    assert r["query"]["mongo"]["layer"] == "leakpoint"


def test_count_with_date():
    r = _assert_rule(
        "จำนวนจุดแตกรั่วปี 2567",
        _rule_matched="count_with_date",
        response_type="numeric",
    )


def test_count_with_month_year():
    r = _assert_rule(
        "จำนวนจุดซ่อมท่อเดือนมกราคม 2567",
        _rule_matched="count_with_date",
    )


# ═══════════════════════════════════════════════════════════
# 6. Fallback date patterns
# ═══════════════════════════════════════════════════════════

def test_fallback_layer_year():
    """Layer + year without explicit count keyword → still matches."""
    r = _assert_rule(
        "จุดแตกรั่วปี 2567",
        _rule_matched="count_with_date",
    )


# ═══════════════════════════════════════════════════════════
# 7. Zone + layer patterns
# ═══════════════════════════════════════════════════════════

def test_zone_pipe_total_length():
    r = _assert_rule(
        "ความยาวท่อรวมของเขต 9",
    )
    assert r.get("_zone") == "9"
    assert r["query"]["mongo"]["layer"] == "pipe"


def test_zone_count():
    r = _assert_rule(
        "จำนวนหัวดับเพลิงเขต 10",
    )
    assert r.get("_zone") == "10"


# ═══════════════════════════════════════════════════════════
# 8. Nationwide
# ═══════════════════════════════════════════════════════════

def test_nationwide_count():
    r = _assert_rule("จำนวนท่อทั้งหมดทั้งประเทศ")
    assert r.get("_nationwide") is True or r["query"]["mongo"]["pwa_code"] is None


# ═══════════════════════════════════════════════════════════
# 9. No match → None
# ═══════════════════════════════════════════════════════════

def test_no_match_generic():
    _assert_no_match("สวัสดี")


def test_no_match_ambiguous():
    _assert_no_match("ปัญหาน้ำประปา")


# ═══════════════════════════════════════════════════════════
# 10. Follow-up
# ═══════════════════════════════════════════════════════════

def test_followup_add_size():
    ctx = {
        "layer": "pipe",
        "pwa_code": "5531012",
        "operation": "aggregate",
        "response_type": "numeric",
        "pipeline": [
            {"$match": {"properties.typeId": "AC"}},
            {"$group": {"_id": None, "total_length": {"$sum": {"$toDouble": "$properties.length"}}}},
            {"$project": {"_id": 0, "total_length": {"$round": ["$total_length", 2]}}},
        ],
    }
    r = parse_followup("ขนาด 100 มม. ขึ้นไป", ctx)
    assert r is not None
    assert r["_rule_matched"] == "followup"
    match = r["query"]["mongo"]["pipeline"][0].get("$match", r["query"]["mongo"]["pipeline"][0])
    assert "$expr" in match or "properties.sizeId" in match


def test_followup_different_layer_ignored():
    ctx = {"layer": "pipe", "pwa_code": "", "operation": "count", "pipeline": [{}]}
    r = parse_followup("แสดงตำแหน่งหัวดับเพลิง", ctx)
    assert r is None, "Follow-up should return None when a different layer is mentioned"


def test_followup_no_modifier():
    ctx = {"layer": "pipe", "pwa_code": "", "operation": "count", "pipeline": [{}]}
    r = parse_followup("ขอบคุณครับ", ctx)
    assert r is None


# ═══════════════════════════════════════════════════════════
# 11. Detectors
# ═══════════════════════════════════════════════════════════

def test_detect_inch_size():
    r = _assert_rule("ท่อขนาด 4 นิ้ว มีกี่ท่อ")
    assert r is not None


def test_detect_large_pipe():
    r = _assert_rule("ท่อขนาดใหญ่มีกี่ท่อ")
    assert r is not None


def test_pwa_code_passthrough():
    r = _assert_rule("จำนวนท่อทั้งหมด", pwa_code="5531012")
    assert r["query"]["mongo"]["pwa_code"] == "5531012"


# ═══════════════════════════════════════════════════════════
# Run
# ═══════════════════════════════════════════════════════════

if __name__ == "__main__":
    import traceback
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    passed = failed = 0
    for fn in tests:
        try:
            fn()
            passed += 1
            print("  PASS  {}".format(fn.__name__))
        except Exception:
            failed += 1
            print("  FAIL  {}".format(fn.__name__))
            traceback.print_exc()
            print()
    print("\n{} passed, {} failed out of {} tests".format(passed, failed, len(tests)))
    sys.exit(1 if failed else 0)
