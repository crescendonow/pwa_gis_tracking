"""Group-by patterns — "ท่อแยกตามขนาด", "ท่อแยกตามชนิด"."""

import re
from datetime import datetime

from ..query_builder import build_match
from ..mappings.fields import type_field, size_field, status_field
from ..mappings.meter import cust_stat_filter

LABEL_TYPE = {"pipe": "ชนิดท่อ", "valve": "ชนิดวาล์ว", "leakpoint": "ชนิดท่อ", "bldg": "ประเภทอาคาร"}
LABEL_SIZE = {
    "pipe": "ขนาด (มม.)", "valve": "ขนาด (มม.)", "firehydrant": "ขนาด (มม.)",
    "leakpoint": "ขนาดท่อ (มม.)", "meter": "ขนาดมิเตอร์",
}
LABEL_STATUS = {"valve": "สถานะ", "firehydrant": "สถานะ", "meter": "สถานะลูกค้า", "bldg": "สถานะการใช้น้ำ"}


class GroupByPattern:
    """GROUP BY — "ท่อแยกตามขนาด", "ท่อแยกตามชนิด"."""

    def match(self, ctx):
        return ctx.is_group and ctx.layer

    def build(self, ctx):
        group_field, group_label = self._resolve_group(ctx)
        if not group_field:
            return None

        extra_match = {}
        if ctx.meter_stat_id and ctx.layer == "meter":
            extra_match["properties.custStat"] = cust_stat_filter(ctx.meter_stat_id)
        if ctx.age and ctx.layer == "pipe":
            cutoff_year = datetime.now().year + 543 - ctx.age
            extra_match["properties.yearInstall"] = {"$lte": cutoff_year}

        match_stage = build_match(
            ctx.effective_pwa,
            ctx.pipe_type if ctx.layer in ("pipe", "leakpoint") else None,
            layer=ctx.layer,
            size_gte=ctx.size_gte, size_lt=ctx.size_lt,
            extra=extra_match if extra_match else None,
        )

        # If grouping by สาขา + pipe, also sum length
        extra_accum = {}
        if group_label == "สาขา" and ctx.layer == "pipe":
            extra_accum["ความยาวรวม"] = {"$sum": {"$toDouble": "$properties.length"}}

        group_stage = {"$group": {"_id": group_field, "จำนวน": {"$sum": 1}}}
        group_stage["$group"].update(extra_accum)

        # Build $project stage — มิติ (ชนิด/ขนาด/ฯลฯ) มาก่อน แล้วค่อย "จำนวน" ต่อท้ายเสมอ
        # เพื่อให้อ่านผลลัพธ์เป็นตารางง่าย (ชนิด | ขนาด | จำนวน)
        if isinstance(group_field, dict):
            project = {"_id": 0}
            for k in group_field:                 # dict ใน Python 3.7+ รักษาลำดับ insertion
                project[k] = "$_id.{}".format(k)
            if extra_accum:
                project["ความยาวรวม"] = 1
            project["จำนวน"] = "$จำนวน"           # ← ต่อท้ายเสมอ
            project_stage = {"$project": project}
        else:
            if extra_accum:
                project_stage = {"$project": {"_id": 0, group_label: "$_id", "ความยาวรวม": 1, "จำนวน": 1}}
            else:
                project_stage = {"$project": {"_id": 0, group_label: "$_id", "จำนวน": "$จำนวน"}}

        pipeline = [
            match_stage,
            group_stage,
            project_stage,
            {"$sort": {"จำนวน": -1}},
        ]
        return {
            "text_response": "กำลังแยก{}ตาม{}ค่ะ".format(ctx.layer_label, group_label),
            "target_db": "mongo",
            "response_type": "table",
            "intent_summary": "Group {} by {}".format(ctx.layer, group_label),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": ctx.layer,
                    "pipeline": pipeline,
                    "operation": "aggregate",
                }
            },
            "_rule_matched": "group_by",
        }

    def _resolve_group(self, ctx):
        """Determine group_field and group_label from text."""
        text = ctx.text
        layer = ctx.layer

        is_by_branch = bool(re.search(r"รายสาขา", text))

        group_target_m = re.search(r"(?:แยกตาม|แบ่งตาม|จำแนกตาม)(.*)", text)
        group_target = group_target_m.group(1) if group_target_m else text

        has_type_in_grp = bool(re.search(r"ชนิด|ประเภท|วัสดุ", group_target))
        has_size_in_grp = bool(re.search(r"ขนาด", group_target))

        if is_by_branch:
            return "$properties.pwaCode", "สาขา"

        if has_type_in_grp and has_size_in_grp:
            tf, sf = type_field(layer), size_field(layer)
            if not (tf and sf):
                return None, None
            label_t = "ชนิดท่อ" if layer in ("pipe", "leakpoint") else "ชนิด"
            label_s = "ขนาดท่อ" if layer == "leakpoint" else "ขนาด"
            return {label_t: "$" + tf, label_s: "$" + sf}, label_t + "+" + label_s

        if has_type_in_grp:
            tf = type_field(layer)
            return ("$" + tf, LABEL_TYPE.get(layer, "ชนิด")) if tf else (None, None)

        if has_size_in_grp:
            sf = size_field(layer)
            return ("$" + sf, LABEL_SIZE.get(layer, "ขนาด")) if sf else (None, None)

        if re.search(r"สถานะ", group_target):
            st = status_field(layer)
            return ("$" + st, LABEL_STATUS.get(layer, "สถานะ")) if st else (None, None)
            # leakpoint → status = None → คืน (None, None) → build() คืน None
            #   → parse_rule จะไปลอง pattern ถัดไป (count) ตามลำดับเดิม

        if re.search(r"หน้าที่|ฟังก์ชัน", group_target):
            if layer == "pipe":
                return "$properties.functionId", "หน้าที่ท่อ"
            elif layer == "valve":
                return "$properties.functionId", "หน้าที่ประตูน้ำ"

        if re.search(r"เกรด|ชั้น|class", group_target):
            if layer == "pipe":
                return "$properties.classId", "ชั้นท่อ"

        if re.search(r"สาขา", group_target):
            return "$properties.pwaCode", "สาขา"

        if re.search(r"สาเหตุ", group_target) and layer == "leakpoint":
            return "$properties.cause", "สาเหตุ"

        if re.search(r"ผลิตภัณฑ์|ยี่ห้อ", group_target) and layer == "pipe":
            return "$properties.productId", "ผลิตภัณฑ์"

        return None, None
