"""Fallback pattern — layer + date/fiscal year → count."""

from datetime import datetime


class FallbackDatePattern:
    """FALLBACK: layer + date/fiscal year → count (no explicit จำนวน/กี่)."""

    def match(self, ctx):
        return ctx.layer and (ctx.year or ctx.month or ctx.fiscal_start)

    def build(self, ctx):
        filt = {}
        if ctx.effective_pwa:
            filt["properties.pwaCode"] = ctx.effective_pwa
        date_field = "properties.leakDatetime" if ctx.layer == "leakpoint" else "properties.recordDate"

        if ctx.fiscal_start:
            filt[date_field] = {"$gte": ctx.fiscal_start, "$lt": ctx.fiscal_end}
            date_desc = "ปีงบประมาณ {} ".format(ctx.fiscal_year)
        elif ctx.year and ctx.month:
            start = "{}-{:02d}-01T00:00:00Z".format(ctx.year, ctx.month)
            end = ("{}-01-01T00:00:00Z".format(ctx.year + 1) if ctx.month == 12
                   else "{}-{:02d}-01T00:00:00Z".format(ctx.year, ctx.month + 1))
            filt[date_field] = {"$gte": start, "$lt": end}
            date_desc = "เดือน {}/{} ".format(ctx.month, ctx.year + 543)
        elif ctx.year:
            filt[date_field] = {
                "$gte": "{}-01-01T00:00:00Z".format(ctx.year),
                "$lt": "{}-01-01T00:00:00Z".format(ctx.year + 1),
            }
            date_desc = "ปี {} ".format(ctx.year + 543)
        else:
            cy = datetime.now().year
            start = "{}-{:02d}-01T00:00:00Z".format(cy, ctx.month)
            end = ("{}-01-01T00:00:00Z".format(cy + 1) if ctx.month == 12
                   else "{}-{:02d}-01T00:00:00Z".format(cy, ctx.month + 1))
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
