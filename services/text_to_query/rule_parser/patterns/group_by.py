"""Group-by patterns — "ท่อแยกตามขนาด", "ท่อแยกตามชนิด"."""

import re
from datetime import datetime

from ..query_builder import build_match


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
            extra_match["properties.custStat"] = ctx.meter_stat_id
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

        # Build $project stage
        if isinstance(group_field, dict):
            project = {"_id": 0, "จำนวน": "$จำนวน"}
            for k in group_field:
                project[k] = "$_id.{}".format(k)
            if extra_accum:
                project["ความยาวรวม"] = 1
            project_stage = {"$project": project}
        else:
            if extra_accum:
                project_stage = {"$project": {"_id": 0, group_label: "$_id", "จำนวน": 1, "ความยาวรวม": 1}}
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
            if layer == "pipe":
                return {"ชนิดท่อ": "$properties.typeId", "ขนาด": "$properties.sizeId"}, "ชนิดท่อ+ขนาด"
            elif layer == "leakpoint":
                return {"ชนิดท่อ": "$properties.pipeTypeId", "ขนาดท่อ": "$properties.pipeSizesId"}, "ชนิดท่อ+ขนาดท่อ"
            else:
                return {"ชนิด": "$properties.typeId", "ขนาด": "$properties.sizeId"}, "ชนิด+ขนาด"

        if has_type_in_grp:
            if layer == "pipe":
                return "$properties.typeId", "ชนิดท่อ"
            elif layer == "valve":
                return "$properties.typeId", "ชนิดวาล์ว"
            elif layer == "leakpoint":
                return "$properties.pipeTypeId", "ชนิดท่อ"

        if has_size_in_grp:
            if layer == "pipe":
                return "$properties.sizeId", "ขนาด (มม.)"
            elif layer == "meter":
                return "$properties.meterSizeCode", "ขนาดมิเตอร์"
            elif layer in ("valve", "firehydrant"):
                return "$properties.sizeId", "ขนาด (มม.)"
            elif layer == "leakpoint":
                return "$properties.pipeSizesId", "ขนาดท่อ (มม.)"

        if re.search(r"สถานะ", group_target):
            if layer in ("valve", "firehydrant"):
                return "$properties.statusId", "สถานะ"
            elif layer == "meter":
                return "$properties.custStat", "สถานะลูกค้า"
            elif layer == "leakpoint":
                return "$properties.LeakStatus", "สถานะ"

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
