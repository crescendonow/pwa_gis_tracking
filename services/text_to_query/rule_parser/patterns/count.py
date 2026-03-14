"""Count patterns — all count-based query patterns."""

from datetime import datetime

from ..query_builder import build_match


class CountMeterStatusPattern:
    """COUNT with meter status — "จำนวนมาตรตาย", "มาตรปกติกี่เครื่อง"."""

    def match(self, ctx):
        return ctx.is_count and ctx.meter_stat_id and ctx.layer == "meter"

    def build(self, ctx):
        filt = {}
        if ctx.effective_pwa:
            filt["properties.pwaCode"] = ctx.effective_pwa
        filt["properties.custStat"] = ctx.meter_stat_id

        return {
            "text_response": "กำลังนับจำนวนมาตรวัดน้ำ สถานะ{}ค่ะ".format(ctx.meter_stat_label),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count meters with custStat={}".format(ctx.meter_stat_id),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": "meter",
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_meter_status",
        }


class CountWithFilterPattern:
    """COUNT with type + size filter — "ท่อ AC ขนาด 100 กี่ท่อ"."""

    def match(self, ctx):
        return ctx.is_count and (ctx.size or ctx.size_gte or ctx.size_lt or ctx.pipe_type) and ctx.layer

    def build(self, ctx):
        extra = {}
        if ctx.age and ctx.layer == "pipe":
            cutoff_year = datetime.now().year + 543 - ctx.age
            extra["properties.yearInstall"] = {"$lte": cutoff_year}
        if ctx.year_range and ctx.layer == "pipe":
            extra["properties.yearInstall"] = ctx.year_range
        if ctx.exclude_sleeve and ctx.layer == "pipe":
            extra["properties.functionId"] = {"$ne": "6"}

        match_stage = build_match(
            ctx.effective_pwa,
            ctx.pipe_type if ctx.layer in ("pipe", "leakpoint") else None,
            ctx.pipe_func_id if ctx.layer == "pipe" else None,
            ctx.size, ctx.size_gte, ctx.size_lt, extra, ctx.layer
        )

        desc_parts = [ctx.layer_label]
        if ctx.pipe_type:
            desc_parts.append("ชนิด {}".format(ctx.pipe_type))
        if ctx.size_gte and ctx.size_lt:
            desc_parts.append("ขนาด {}-{} มม.".format(ctx.size_gte, ctx.size_lt))
        elif ctx.size_gte:
            desc_parts.append("ขนาด {} มม. ขึ้นไป".format(ctx.size_gte))
        elif ctx.size_lt:
            desc_parts.append("ขนาดต่ำกว่า {} มม.".format(ctx.size_lt))
        elif ctx.size:
            desc_parts.append("ขนาด {} มม.".format(ctx.size))

        return {
            "text_response": "กำลังนับจำนวน {} ค่ะ".format(" ".join(desc_parts)),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} with filters".format(ctx.layer),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": ctx.layer,
                    "pipeline": [match_stage["$match"]],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_with_filter",
        }


class CountWithAgePattern:
    """COUNT with age — "ท่ออายุ 10 ปีขึ้นไป กี่ท่อ"."""

    def match(self, ctx):
        return ctx.is_count and ctx.age and ctx.layer

    def build(self, ctx):
        extra = {}
        if ctx.layer == "pipe":
            cutoff_year = datetime.now().year + 543 - ctx.age
            extra["properties.yearInstall"] = {"$lte": cutoff_year}
        elif ctx.layer == "meter":
            cutoff_date = "{}-01-01T00:00:00Z".format(datetime.now().year - ctx.age)
            extra["properties.beginCustDate"] = {"$lte": cutoff_date}

        match_stage = build_match(ctx.effective_pwa, extra=extra, layer=ctx.layer)

        return {
            "text_response": "กำลังนับจำนวน{} อายุ {} ปีขึ้นไปค่ะ".format(ctx.layer_label, ctx.age),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} older than {} years".format(ctx.layer, ctx.age),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": ctx.layer,
                    "pipeline": [match_stage["$match"]],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_with_age",
        }


class CountFiscalYearPattern:
    """COUNT with fiscal year — "จุดแตกรั่วปีงบประมาณ 2566"."""

    def match(self, ctx):
        return ctx.is_count and ctx.fiscal_start and ctx.layer

    def build(self, ctx):
        filt = {}
        if ctx.effective_pwa:
            filt["properties.pwaCode"] = ctx.effective_pwa
        date_field = "properties.leakDatetime" if ctx.layer == "leakpoint" else "properties.recordDate"
        filt[date_field] = {"$gte": ctx.fiscal_start, "$lt": ctx.fiscal_end}

        return {
            "text_response": "กำลังนับจำนวน{} ปีงบประมาณ {} ค่ะ".format(ctx.layer_label, ctx.fiscal_year),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} in fiscal year {}".format(ctx.layer, ctx.fiscal_year),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": ctx.layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_fiscal_year",
        }


class CountWithDatePattern:
    """COUNT with date — "จุดแตกรั่วปี 2567"."""

    def match(self, ctx):
        return ctx.is_count and (ctx.year or ctx.month) and ctx.layer

    def build(self, ctx):
        filt = {}
        if ctx.effective_pwa:
            filt["properties.pwaCode"] = ctx.effective_pwa

        date_field = "properties.leakDatetime" if ctx.layer == "leakpoint" else "properties.recordDate"

        date_desc = ""
        if ctx.year and ctx.month:
            start = "{}-{:02d}-01T00:00:00Z".format(ctx.year, ctx.month)
            if ctx.month == 12:
                end = "{}-01-01T00:00:00Z".format(ctx.year + 1)
            else:
                end = "{}-{:02d}-01T00:00:00Z".format(ctx.year, ctx.month + 1)
            filt[date_field] = {"$gte": start, "$lt": end}
            date_desc = "เดือน {}/{} ".format(ctx.month, ctx.year + 543)
        elif ctx.year:
            filt[date_field] = {
                "$gte": "{}-01-01T00:00:00Z".format(ctx.year),
                "$lt": "{}-01-01T00:00:00Z".format(ctx.year + 1),
            }
            date_desc = "ปี {} ".format(ctx.year + 543)
        elif ctx.month:
            cy = datetime.now().year
            start = "{}-{:02d}-01T00:00:00Z".format(cy, ctx.month)
            if ctx.month == 12:
                end = "{}-01-01T00:00:00Z".format(cy + 1)
            else:
                end = "{}-{:02d}-01T00:00:00Z".format(cy, ctx.month + 1)
            filt[date_field] = {"$gte": start, "$lt": end}
            date_desc = "เดือน {} ".format(ctx.month)

        return {
            "text_response": "กำลังนับจำนวน{} {}ค่ะ".format(ctx.layer_label, date_desc),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count {} in {}".format(ctx.layer, date_desc.strip()),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": ctx.layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_with_date",
        }


class CountAllPattern:
    """SIMPLE COUNT ALL — "จำนวนมาตรทั้งหมด"."""

    def match(self, ctx):
        return ctx.is_count and ctx.layer

    def build(self, ctx):
        filt = {}
        if ctx.effective_pwa:
            filt["properties.pwaCode"] = ctx.effective_pwa

        result = {
            "text_response": "กำลังนับจำนวน{}ทั้งหมด{}ค่ะ".format(
                ctx.layer_label,
                " ทั้งประเทศ" if ctx.is_nationwide else "",
            ),
            "target_db": "mongo",
            "response_type": "numeric",
            "intent_summary": "Count all {}".format(ctx.layer),
            "query": {
                "mongo": {
                    "pwa_code": ctx.effective_pwa or None,
                    "layer": ctx.layer,
                    "pipeline": [filt],
                    "operation": "count",
                }
            },
            "_rule_matched": "count_all",
        }
        if ctx.is_nationwide:
            result["_nationwide"] = True
        return result
