"""Zone + Layer patterns — queries across all branches in a zone."""

from datetime import datetime

from ..query_builder import build_match


class ZoneLayerPattern:
    """ZONE + LAYER: "ความยาวท่อรวม ของเขต 9", "จำนวนหัวดับเพลิงใน เขต 10"."""

    def match(self, ctx):
        return ctx.zone and ctx.layer

    def build(self, ctx):
        extra = {}
        if ctx.age and ctx.layer == "pipe":
            cutoff_year = datetime.now().year + 543 - ctx.age
            extra["properties.yearInstall"] = {"$lte": cutoff_year}

        match_stage = build_match(
            "",
            ctx.pipe_type if ctx.layer in ("pipe", "leakpoint") else None,
            ctx.pipe_func_id if ctx.layer == "pipe" else None,
            ctx.size, ctx.size_gte, ctx.size_lt, extra, ctx.layer
        )

        if ctx.is_total_length or (ctx.layer == "pipe" and ctx.is_pipe_total):
            return self._build_length(ctx, match_stage)
        else:
            return self._build_count(ctx, match_stage)

    def _build_length(self, ctx, match_stage):
        pipeline = [
            match_stage,
            {"$group": {"_id": None, "total_length": {"$sum": {"$toDouble": "$properties.length"}}}},
        ]
        if ctx.want_km:
            pipeline.append({"$project": {"_id": 0, "total_length_km": {"$round": [{"$divide": ["$total_length", 1000]}, 2]}}})
        else:
            pipeline.append({"$project": {"_id": 0, "total_length": {"$round": ["$total_length", 2]}}})

        desc_parts = ["ท่อประปา"]
        if ctx.pipe_type:
            desc_parts.append("ชนิด {}".format(ctx.pipe_type))
        if ctx.size_gte:
            desc_parts.append("ขนาด {} มม. ขึ้นไป".format(ctx.size_gte))
        if ctx.age:
            desc_parts.append("อายุ {} ปีขึ้นไป".format(ctx.age))

        return {
            "text_response": "กำลังคำนวณความยาวรวมของ{} ในเขต {} ค่ะ".format(" ".join(desc_parts), ctx.zone),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Total pipe length in zone {}".format(ctx.zone),
            "query": {
                "mongo": {
                    "pwa_code": None,
                    "layer": ctx.layer,
                    "pipeline": pipeline,
                    "operation": "aggregate",
                }
            },
            "_zone": ctx.zone,
            "_rule_matched": "zone_pipe_total_length",
        }

    def _build_count(self, ctx, match_stage):
        filt = match_stage["$match"]
        desc_parts = [ctx.layer_label]
        if ctx.pipe_type:
            desc_parts.append("ชนิด {}".format(ctx.pipe_type))
        if ctx.size_gte:
            desc_parts.append("ขนาด {} มม. ขึ้นไป".format(ctx.size_gte))
        if ctx.age:
            desc_parts.append("อายุ {} ปีขึ้นไป".format(ctx.age))

        return {
            "text_response": "กำลังนับจำนวน{} ในเขต {} ค่ะ".format(" ".join(desc_parts), ctx.zone),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} in zone {}".format(ctx.layer, ctx.zone),
            "query": {
                "mongo": {
                    "pwa_code": None,
                    "layer": ctx.layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_zone": ctx.zone,
            "_rule_matched": "zone_count",
        }
