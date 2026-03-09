"""
Query validation — enforce read-only for both SQL and MongoDB.
"""

import re
import json
import logging

log = logging.getLogger("text_to_query")

# ── SQL Validation ───────────────────────────────────

FORBIDDEN_SQL_KW = re.compile(
    r"\b(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|GRANT|REVOKE|EXECUTE|EXEC)\b",
    re.IGNORECASE,
)


def validate_sql(sql):
    """
    Validate SQL is read-only.
    Returns True if safe, False if contains forbidden operations.
    """
    if not sql or not sql.strip():
        return False

    # Remove SQL comments
    cleaned = re.sub(r"--.*$", "", sql, flags=re.MULTILINE)
    cleaned = re.sub(r"/\*.*?\*/", "", cleaned, flags=re.DOTALL)
    cleaned = cleaned.strip()

    upper = cleaned.upper()

    # Must start with SELECT or WITH (CTE)
    if not (upper.startswith("SELECT") or upper.startswith("WITH")):
        log.warning("SQL rejected: does not start with SELECT/WITH")
        return False

    # Check for forbidden keywords
    if FORBIDDEN_SQL_KW.search(cleaned):
        log.warning("SQL rejected: contains forbidden keyword")
        return False

    # No semicolons (prevent statement stacking)
    if ";" in cleaned:
        log.warning("SQL rejected: contains semicolon")
        return False

    return True


def clean_sql(raw):
    """Clean raw SQL output from LLM."""
    # Strip <think>...</think>
    raw = re.sub(r"<think>.*?</think>", "", raw, flags=re.DOTALL)
    # Strip markdown code fences
    raw = re.sub(r"```(?:sql)?\s*", "", raw)
    raw = re.sub(r"```\s*$", "", raw, flags=re.MULTILINE)
    return raw.strip().rstrip(";")


# ── MongoDB Validation ───────────────────────────────

FORBIDDEN_MONGO_STAGES = {
    "$merge", "$out", "$delete", "$replaceroot",
    "$collstats", "$indexstats", "$planCacheStats",
}

FORBIDDEN_MONGO_OPS = {
    "insert", "insertone", "insertmany",
    "update", "updateone", "updatemany",
    "delete", "deleteone", "deletemany",
    "drop", "rename", "createindex", "dropindex",
    "bulkwrite", "replaceone",
}


def validate_mongo_pipeline(pipeline):
    """
    Validate MongoDB aggregation pipeline is read-only.
    Returns True if safe, False if contains forbidden operations.
    """
    if not isinstance(pipeline, list):
        log.warning("MongoDB rejected: pipeline is not a list")
        return False

    pipeline_str = json.dumps(pipeline).lower()

    # Check for forbidden stages
    for stage in FORBIDDEN_MONGO_STAGES:
        if stage.lower() in pipeline_str:
            log.warning("MongoDB rejected: contains forbidden stage %s", stage)
            return False

    # Check for forbidden operations in string form
    for op in FORBIDDEN_MONGO_OPS:
        if op in pipeline_str:
            log.warning("MongoDB rejected: contains forbidden operation %s", op)
            return False

    # Walk the pipeline stages
    for stage in pipeline:
        if not isinstance(stage, dict):
            continue
        for key in stage:
            if key.lower().lstrip("$") in {"merge", "out", "delete"}:
                log.warning("MongoDB rejected: stage key %s is forbidden", key)
                return False

    return True


def validate_mongo_operation(operation):
    """Validate the MongoDB operation type is read-only."""
    allowed = {"find", "aggregate", "count", "countdocuments", "distinct"}
    return operation.lower() in allowed
