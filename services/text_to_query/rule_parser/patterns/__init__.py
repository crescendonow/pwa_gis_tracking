"""
Pattern registry — each pattern class handles one type of query.
Patterns are checked in priority order; first match wins.
"""

from .postgis import PostgisZonePattern, PostgisAllBranchesPattern
from .zone_layer import ZoneLayerPattern
from .pipe_length import PipeTotalLengthPattern
from .group_by import GroupByPattern
from .show_position import ShowPositionPattern
from .count import (
    CountMeterStatusPattern,
    CountWithFilterPattern,
    CountWithAgePattern,
    CountFiscalYearPattern,
    CountWithDatePattern,
    CountAllPattern,
)
from .fallback import FallbackDatePattern


class BasePattern:
    """Base class for all patterns."""
    priority = 100

    def match(self, ctx):
        raise NotImplementedError

    def build(self, ctx):
        raise NotImplementedError


# Ordered by priority (first match wins)
_PATTERNS = [
    PostgisZonePattern(),
    PostgisAllBranchesPattern(),
    ZoneLayerPattern(),
    PipeTotalLengthPattern(),
    GroupByPattern(),
    ShowPositionPattern(),
    CountMeterStatusPattern(),
    CountWithFilterPattern(),
    CountWithAgePattern(),
    CountFiscalYearPattern(),
    CountWithDatePattern(),
    CountAllPattern(),
    FallbackDatePattern(),
]


def get_patterns():
    """Return all registered patterns in priority order."""
    return _PATTERNS
