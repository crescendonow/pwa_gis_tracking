"""
Rule-Based Parser — fast path for common GIS queries.
Matches ~90% of typical user questions via regex, responds < 100ms.
Falls back to LLM for unmatched patterns.

Refactored from rule_parser.py (1178 lines) into a package.
"""

import logging

from .detectors import ParseContext
from .patterns import get_patterns
from .followup import parse_followup

log = logging.getLogger("text_to_query")


def parse_rule(prompt, pwa_code=""):
    """
    Try to match the user prompt against known patterns.

    Returns:
        dict (same format as LLM intent) if matched, None otherwise.
    """
    text = prompt.strip()
    ctx = ParseContext(text, pwa_code)

    # Need layer for most patterns (postgis patterns check internally)
    for pattern in get_patterns():
        if pattern.match(ctx):
            result = pattern.build(ctx)
            if result is not None:
                return result

    # Check: most patterns need a layer; postgis patterns handle their own
    if not ctx.layer:
        return None

    return None


# Re-export for backward compatibility
__all__ = ["parse_rule", "parse_followup"]
