"""Pipe total length pattern."""

from datetime import datetime

from ..query_builder import build_match


class PipeTotalLengthPattern:
    """PIPE: Total length (with optional type/size/function filters)."""

    def match(self, ctx):
        return ctx.layer == "pipe" and (ctx.is_total_length or ctx.is_pipe_total)

    def build(self, ctx):
        extra = {}
        if ctx.age:
            cutoff_year = datetime.now().year + 543 - ctx.age
            extra["properties.yearInstall"] = {"$lte": cutoff_year}
        if ctx.year_range:
            extra["properties.yearInstall"] = ctx.year_range
        if ctx.exclude_sleeve:
            extra["properties.functionId"] = {"$ne": "6"}

        match_stage = build_match(
            ctx.effective_pwa, ctx.pipe_type, ctx.pipe_func_id,
            ctx.size, ctx.size_gte, ctx.size_lt, extra, "pipe"
        )

        pipeline = [
            match_stage,
            {"$group": {
                "_id": None,
                "total_length": {"$sum": {"$toDouble": "$properties.length"}}
            }},
        ]

        if ctx.want_km:
            pipeline.append({"$project": {
                "_id": 0,
                "total_length_km": {"$round": [{"$divide": ["$total_length", 1000]}, 2]}
            }})
        else:
            pipeline.append({"$project": {"_id": 0, "total_length": {"$round": ["$total_length", 2]}}})

        # Build description
        desc_parts = []
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
        desc = " ".join(desc_parts)
        if desc:
            desc = " " + desc

        result = {
            "text_response": "กำลังคำนวณความยาวรวมของท่อประปา{}{}ค่ะ".format(
                desc,
                " ทั้งประเทศ" if ctx.is_nationwide else "",
            ),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Total pipe length{}".format(" " + desc if desc else ""),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": "pipe",
                    "pipeline": pipeline,
                    "operation": "aggregate",
                }
            },
            "_rule_matched": "pipe_total_length",
        }
        if ctx.is_nationwide:
            result["_nationwide"] = True
        return result
