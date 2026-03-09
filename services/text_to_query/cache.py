"""
SQLite-backed exact-match cache for query results.
"""

import hashlib
import json
import logging
import os
import sqlite3
import time
import unicodedata

from config import CACHE_TTL_HOURS, CACHE_MAX_ENTRIES

log = logging.getLogger("text_to_query")

_DB_PATH = os.path.join(os.path.dirname(__file__), "cache.db")


def _get_conn():
    conn = sqlite3.connect(_DB_PATH)
    conn.execute("PRAGMA journal_mode=WAL")
    return conn


def _init_db():
    conn = _get_conn()
    conn.execute("""
        CREATE TABLE IF NOT EXISTS query_cache (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            prompt_hash TEXT UNIQUE NOT NULL,
            prompt_text TEXT NOT NULL,
            pwa_code TEXT DEFAULT '',
            response_json TEXT NOT NULL,
            created_at REAL NOT NULL,
            hit_count INTEGER DEFAULT 0,
            last_hit_at REAL
        )
    """)
    conn.execute("CREATE INDEX IF NOT EXISTS idx_prompt_hash ON query_cache(prompt_hash)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_created_at ON query_cache(created_at)")
    conn.execute("""
        CREATE TABLE IF NOT EXISTS query_log (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            prompt_text TEXT NOT NULL,
            pwa_code TEXT DEFAULT '',
            uid TEXT DEFAULT '',
            target_db TEXT DEFAULT '',
            response_type TEXT DEFAULT '',
            cached INTEGER DEFAULT 0,
            execution_time_ms INTEGER DEFAULT 0,
            created_at REAL NOT NULL
        )
    """)
    conn.commit()
    conn.close()


# Initialize on import
_init_db()


def _normalize(text):
    """Normalize prompt text for consistent hashing."""
    text = text.strip().lower()
    text = unicodedata.normalize("NFC", text)
    # Collapse whitespace
    text = " ".join(text.split())
    return text


def _make_hash(prompt, pwa_code):
    """Create SHA-256 hash of normalized prompt + pwa_code."""
    key = "{}|{}".format(_normalize(prompt), pwa_code or "")
    return hashlib.sha256(key.encode("utf-8")).hexdigest()


def get_cached(prompt, pwa_code):
    """
    Look up an exact-match cached response.
    Returns the response dict if found and not expired, None otherwise.
    """
    prompt_hash = _make_hash(prompt, pwa_code)
    ttl_seconds = CACHE_TTL_HOURS * 3600
    now = time.time()

    conn = _get_conn()
    try:
        row = conn.execute(
            "SELECT id, response_json, created_at FROM query_cache WHERE prompt_hash = ?",
            (prompt_hash,),
        ).fetchone()

        if row is None:
            return None

        row_id, response_json, created_at = row

        # Check TTL
        if now - created_at > ttl_seconds:
            conn.execute("DELETE FROM query_cache WHERE id = ?", (row_id,))
            conn.commit()
            return None

        # Update hit count
        conn.execute(
            "UPDATE query_cache SET hit_count = hit_count + 1, last_hit_at = ? WHERE id = ?",
            (now, row_id),
        )
        conn.commit()

        return json.loads(response_json)
    except Exception as exc:
        log.error("Cache read error: %s", exc)
        return None
    finally:
        conn.close()


def set_cached(prompt, pwa_code, response):
    """Store a query response in the cache."""
    prompt_hash = _make_hash(prompt, pwa_code)
    now = time.time()

    conn = _get_conn()
    try:
        # Enforce max entries (LRU eviction)
        count = conn.execute("SELECT COUNT(*) FROM query_cache").fetchone()[0]
        if count >= CACHE_MAX_ENTRIES:
            # Delete oldest 10%
            delete_count = max(1, CACHE_MAX_ENTRIES // 10)
            conn.execute(
                "DELETE FROM query_cache WHERE id IN "
                "(SELECT id FROM query_cache ORDER BY last_hit_at ASC, created_at ASC LIMIT ?)",
                (delete_count,),
            )

        conn.execute(
            """INSERT OR REPLACE INTO query_cache
               (prompt_hash, prompt_text, pwa_code, response_json, created_at, hit_count, last_hit_at)
               VALUES (?, ?, ?, ?, ?, 0, NULL)""",
            (prompt_hash, prompt[:500], pwa_code or "", json.dumps(response, ensure_ascii=False), now),
        )
        conn.commit()
    except Exception as exc:
        log.error("Cache write error: %s", exc)
    finally:
        conn.close()


def log_query(prompt, pwa_code, uid, target_db, response_type, cached, execution_time_ms):
    """Log a query for audit trail."""
    now = time.time()
    conn = _get_conn()
    try:
        conn.execute(
            """INSERT INTO query_log
               (prompt_text, pwa_code, uid, target_db, response_type, cached, execution_time_ms, created_at)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?)""",
            (prompt[:500], pwa_code or "", uid or "", target_db or "", response_type or "",
             1 if cached else 0, execution_time_ms, now),
        )
        conn.commit()
    except Exception as exc:
        log.error("Query log error: %s", exc)
    finally:
        conn.close()


def clear_cache():
    """Clear all cached entries."""
    conn = _get_conn()
    try:
        conn.execute("DELETE FROM query_cache")
        conn.commit()
        log.info("Cache cleared")
    except Exception as exc:
        log.error("Cache clear error: %s", exc)
    finally:
        conn.close()


def cleanup_expired():
    """Remove expired cache entries."""
    ttl_seconds = CACHE_TTL_HOURS * 3600
    cutoff = time.time() - ttl_seconds
    conn = _get_conn()
    try:
        result = conn.execute("DELETE FROM query_cache WHERE created_at < ?", (cutoff,))
        if result.rowcount > 0:
            log.info("Cleaned up %d expired cache entries", result.rowcount)
        conn.commit()
    except Exception as exc:
        log.error("Cache cleanup error: %s", exc)
    finally:
        conn.close()
