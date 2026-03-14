"""PostGIS patterns — zone queries and branch listing."""

import re


class PostgisZonePattern:
    """POSTGIS: "สาขาในเขต X", "รายชื่อสาขาเขต X"."""

    def match(self, ctx):
        return ctx.zone and re.search(r"สาขา", ctx.text)

    def build(self, ctx):
        return {
            "text_response": "กำลังค้นหารายชื่อสาขาทั้งหมดในเขต {} จากฐานข้อมูล PostGIS ค่ะ".format(ctx.zone),
            "target_db": "postgis",
            "response_type": "table",
            "intent_summary": "List all branches in zone {}".format(ctx.zone),
            "query": {
                "postgis": {
                    "sql": "SELECT pwa_code, name, zone FROM pwa_office.pwa_office234 WHERE zone = '{}' ORDER BY name".format(ctx.zone)
                }
            },
            "_rule_matched": "postgis_zone",
        }


class PostgisAllBranchesPattern:
    """POSTGIS: "สาขาทั้งหมด", "รายชื่อสาขา"."""

    def match(self, ctx):
        return re.search(r"สาขา", ctx.text) and ctx.is_count and not ctx.layer

    def build(self, ctx):
        return {
            "text_response": "กำลังค้นหารายชื่อสาขาทั้งหมดจากฐานข้อมูล PostGIS ค่ะ",
            "target_db": "postgis",
            "response_type": "table",
            "intent_summary": "List all branches",
            "query": {
                "postgis": {
                    "sql": "SELECT pwa_code, name, zone FROM pwa_office.pwa_office234 ORDER BY name"
                }
            },
            "_rule_matched": "postgis_all_branches",
        }
