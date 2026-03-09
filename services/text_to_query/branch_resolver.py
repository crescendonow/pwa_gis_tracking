"""
Branch name → pwa_code resolver.
Loads all branches from PostGIS on startup and provides fuzzy lookup.
"""

import logging
import re

from sqlalchemy import text

from config import pg_engine

log = logging.getLogger("text_to_query")

# ── In-memory branch cache ────────────────────────────
# { "พัทยา": "1020", "ชลบุรี": "5531011", ... }
_branch_cache = {}
# { "1020": "กปภ.สาขาพัทยา", ... }
_code_to_name = {}


def _add_variant(name, code):
    """Add a name variant to the cache (skip if empty or too short)."""
    name = name.strip()
    if name and len(name) >= 2:
        _branch_cache[name] = code


def _load_branches():
    """Load all branches from PostGIS into memory."""
    global _branch_cache, _code_to_name
    if pg_engine is None:
        log.warning("[branch_resolver] SKIP: pg_engine is None — PostgreSQL not connected")
        return

    try:
        log.info("[branch_resolver] Loading branches from PostGIS...")
        with pg_engine.connect() as conn:
            rows = conn.execute(
                text("SELECT pwa_code, name FROM pwa_office.pwa_office234 ORDER BY name")
            ).fetchall()

        log.info("[branch_resolver] Query returned %d rows", len(rows))

        for row in rows:
            code = str(row[0]).strip()
            name = str(row[1]).strip()
            _code_to_name[code] = name

            # 1. Full name: "กปภ.สาขาพัทยา (ชั้นพิเศษ)"
            _add_variant(name, code)

            # 2. Without "กปภ." prefix: "สาขาพัทยา (ชั้นพิเศษ)"
            no_org = re.sub(r"^กปภ\.\s*", "", name).strip()
            _add_variant(no_org, code)

            # 3. Without "กปภ.สาขา" prefix: "พัทยา (ชั้นพิเศษ)"
            no_branch = re.sub(r"^(?:กปภ\.\s*)?สาขา\s*", "", name).strip()
            _add_variant(no_branch, code)

            # 4. Strip parenthetical suffix: "(ชั้นพิเศษ)", "(ชั้นที่ 1)", etc.
            #    "กปภ.สาขาพัทยา (ชั้นพิเศษ)" → "กปภ.สาขาพัทยา"
            base = re.sub(r"\s*\(.*?\)\s*$", "", name).strip()
            if base != name:
                _add_variant(base, code)
                # "สาขาพัทยา"
                _add_variant(re.sub(r"^กปภ\.\s*", "", base).strip(), code)
                # "พัทยา"
                _add_variant(re.sub(r"^(?:กปภ\.\s*)?สาขา\s*", "", base).strip(), code)

            # 5. Core name only (no prefix, no suffix): "พัทยา"
            core = re.sub(r"^(?:กปภ\.\s*)?สาขา\s*", "", name).strip()
            core = re.sub(r"\s*\(.*?\)\s*$", "", core).strip()
            _add_variant(core, code)

        log.info("[branch_resolver] Loaded %d branches, %d keywords", len(_code_to_name), len(_branch_cache))
    except Exception as exc:
        log.error("[branch_resolver] FAILED to load branches: %s", exc)


# Load on import
_load_branches()


def resolve_branch_name(prompt):
    """
    Scan the prompt for a branch name and return its pwa_code.

    Args:
        prompt: User's Thai text prompt

    Returns:
        pwa_code string if found, None otherwise
    """
    if not _branch_cache:
        _load_branches()

    if not _branch_cache:
        return None

    # Sort by name length (longest first) to prevent partial matches
    # e.g., "สาขาพระพุทธบาท" should match before "สาขาพระ"
    for name in sorted(_branch_cache.keys(), key=len, reverse=True):
        if name in prompt:
            code = _branch_cache[name]
            log.info("Branch resolved: '%s' → pwa_code=%s", name, code)
            return code

    return None


def get_branch_name(pwa_code):
    """Get branch name from pwa_code."""
    if not _code_to_name:
        _load_branches()
    return _code_to_name.get(pwa_code, "")


def is_valid_pwa_code(pwa_code):
    """Check if pwa_code exists in the branch list."""
    if not _code_to_name:
        _load_branches()
    return pwa_code in _code_to_name


def get_codes_in_zone(zone):
    """
    Get all pwa_codes in a given zone.

    Args:
        zone: Zone number (string, e.g. "9")

    Returns:
        list of (pwa_code, name) tuples
    """
    if pg_engine is None:
        log.warning("[branch_resolver] pg_engine is None — cannot query zone")
        return []

    try:
        with pg_engine.connect() as conn:
            rows = conn.execute(
                text("SELECT pwa_code, name FROM pwa_office.pwa_office234 WHERE zone = :zone ORDER BY name"),
                {"zone": str(zone)}
            ).fetchall()
        result = [(str(r[0]).strip(), str(r[1]).strip()) for r in rows]
        log.info("[branch_resolver] Zone %s: found %d branches", zone, len(result))
        return result
    except Exception as exc:
        log.error("[branch_resolver] Zone query failed: %s", exc)
        return []


def get_all_codes():
    """
    Get ALL pwa_codes (nationwide).
    Returns list of (pwa_code, name) tuples.
    """
    if not _code_to_name:
        _load_branches()
    return [(code, name) for code, name in _code_to_name.items()]


def reload_cache():
    """Force reload the branch cache."""
    global _branch_cache, _code_to_name
    _branch_cache = {}
    _code_to_name = {}
    _load_branches()
