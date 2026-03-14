"""Show position pattern — geojson responses."""

from datetime import datetime

from ..query_builder import build_match


class ShowPositionPattern:
    """SHOW POSITION — "แสดงตำแหน่งหัวดับเพลิง"."""

    def match(self, ctx):
        return ctx.is_show_position and not ctx.is_group

    def build(self, ctx):
        extra = {}

        if ctx.age:
            cutoff_year = datetime.now().year + 543 - ctx.age
            if ctx.layer == "pipe":
                extra["properties.yearInstall"] = {"$lte": cutoff_year}
            elif ctx.layer == "meter":
                cutoff_date = "{}-01-01T00:00:00Z".format(datetime.now().year - ctx.age)
                extra["properties.beginCustDate"] = {"$lte": cutoff_date}

        match_stage = build_match(
            ctx.effective_pwa,
            ctx.pipe_type if ctx.layer in ("pipe", "leakpoint") else None,
            ctx.pipe_func_id if ctx.layer == "pipe" else None,
            ctx.size, ctx.size_gte, ctx.size_lt, extra, ctx.layer
        )

        desc_parts = [ctx.layer_label]
        if ctx.pipe_type:
            desc_parts.append("ชนิด {}".format(ctx.pipe_type))
        if ctx.pipe_func_label:
            desc_parts.append(ctx.pipe_func_label)
        if ctx.size_gte and ctx.size_lt:
            desc_parts.append("ขนาด {}-{} มม.".format(ctx.size_gte, ctx.size_lt))
        elif ctx.size_gte:
            desc_parts.append("ขนาด {} มม. ขึ้นไป".format(ctx.size_gte))
        elif ctx.size_lt:
            desc_parts.append("ขนาดต่ำกว่า {} มม.".format(ctx.size_lt))
        elif ctx.size:
            desc_parts.append("ขนาด {} มม.".format(ctx.size))
        if ctx.age:
            desc_parts.append("อายุ {} ปีขึ้นไป".format(ctx.age))

        result = {
            "text_response": "กำลังค้นหาตำแหน่ง{}{}ค่ะ".format(
                " ".join(desc_parts),
                " ทั้งประเทศ" if ctx.is_nationwide else "",
            ),
            "target_db": "mongo",
            "response_type": "geojson",
            "intent_summary": "Show {} positions".format(ctx.layer),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": ctx.layer,
                    "pipeline": [match_stage["$match"]] if match_stage["$match"] else [{}],
                    "operation": "find",
                }
            },
            "_rule_matched": "show_position",
        }
        if ctx.is_nationwide:
            result["_nationwide"] = True
        return result
