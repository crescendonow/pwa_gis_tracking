"""Fallback pattern — layer + date/fiscal year → count."""

from datetime import datetime

from ..daterange import year_range, month_range
from ..mappings.fields import date_field


class FallbackDatePattern:
    """FALLBACK: layer + date/fiscal year → count (no explicit จำนวน/กี่)."""

    def match(self, ctx):
        return ctx.layer and (ctx.year or ctx.month or ctx.fiscal_start)

    def build(self, ctx):
        filt = {}
        if ctx.effective_pwa:
            filt["properties.pwaCode"] = ctx.effective_pwa
        dfield = date_field(ctx.layer)

        if ctx.fiscal_start:
            filt[dfield] = {"$gte": ctx.fiscal_start, "$lt": ctx.fiscal_end}
            date_desc = "ปีงบประมาณ {} ".format(ctx.fiscal_year)
        elif ctx.year and ctx.month:
            start, end = month_range(ctx.year, ctx.month)
            filt[dfield] = {"$gte": start, "$lt": end}
            date_desc = "เดือน {}/{} ".format(ctx.month, ctx.year + 543)
        elif ctx.year:
            start, end = year_range(ctx.year)
            filt[dfield] = {"$gte": start, "$lt": end}
            date_desc = "ปี {} ".format(ctx.year + 543)
        else:
            cy = datetime.now().year
            start, end = month_range(cy, ctx.month)
            filt[dfield] = {"$gte": start, "$lt": end}
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
