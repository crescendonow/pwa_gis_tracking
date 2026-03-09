"""
Text-to-Query Microservice — "น้องหนึ่งน้ำ"
Converts Thai natural-language questions into MongoDB / PostGIS queries.
READ-ONLY: absolutely no INSERT, UPDATE, DELETE.
"""

import logging
import time

from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field
from slowapi import Limiter
from slowapi.util import get_remote_address
from slowapi.errors import RateLimitExceeded
from starlette.responses import JSONResponse

from config import PORT, RATE_LIMIT, OLLAMA_MODEL
from cache import get_cached, set_cached
from rule_parser import parse_rule
from intent import generate_query_intent
from branch_resolver import resolve_branch_name, _branch_cache, _code_to_name
from validators import validate_sql, validate_mongo_pipeline
from executors.mongo_executor import execute_mongo
from executors.postgis_executor import execute_postgis
from formatters import format_response

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
log = logging.getLogger("text_to_query")

# ── App ──────────────────────────────────────────────
app = FastAPI(title="Text-to-Query Service", version="1.0.0")

limiter = Limiter(key_func=get_remote_address)
app.state.limiter = limiter


@app.exception_handler(RateLimitExceeded)
async def rate_limit_handler(request: Request, exc: RateLimitExceeded):
    return JSONResponse(
        status_code=429,
        content={"status": "error", "message": "คำขอมากเกินไปค่ะ กรุณารอสักครู่แล้วลองใหม่"},
    )


app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)


# ── Models ───────────────────────────────────────────
class QueryRequest(BaseModel):
    prompt: str = Field(..., max_length=500)
    pwa_code: str = Field(default="")
    uid: str = Field(default="")
    permission: str = Field(default="")


# ── Endpoints ────────────────────────────────────────
@app.get("/health")
async def health():
    from config import pg_engine, mongo_db
    return {
        "status": "Text-to-Query service is running",
        "model": OLLAMA_MODEL,
        "postgis_connected": pg_engine is not None,
        "mongodb_connected": mongo_db is not None,
        "branches_loaded": len(_code_to_name),
        "branch_keywords": len(_branch_cache),
        "sample_branches": dict(list(_code_to_name.items())[:5]) if _code_to_name else {},
    }


@app.get("/debug/branches")
async def debug_branches(q: str = ""):
    """Debug: show branch keywords. Use ?q=พัทยา to search."""
    if q:
        matches = {k: v for k, v in _branch_cache.items() if q in k}
        return {"query": q, "matches": matches, "count": len(matches)}
    # Show all keywords grouped by code (first 10 codes)
    sample = {}
    for code in list(_code_to_name.keys())[:10]:
        keywords = [k for k, v in _branch_cache.items() if v == code]
        sample[code] = {"name": _code_to_name[code], "keywords": keywords}
    return {"total_keywords": len(_branch_cache), "total_branches": len(_code_to_name), "sample": sample}


@app.post("/api/text-to-query")
@limiter.limit(RATE_LIMIT)
async def text_to_query(req: QueryRequest, request: Request):
    t0 = time.time()
    prompt = req.prompt.strip()
    if not prompt:
        raise HTTPException(400, detail="prompt is required")

    pwa_code = req.pwa_code.strip()

    # 0. Resolve branch name → pwa_code from prompt
    #    e.g., "สาขาพระพุทธบาท" → "1080", "ชลบุรี" → "5531011"
    resolved_code = resolve_branch_name(prompt)
    if resolved_code:
        log.info("Branch resolved from prompt: %s → pwa_code=%s", prompt[:40], resolved_code)
        pwa_code = resolved_code

    # 1. Cache check
    cached = get_cached(prompt, pwa_code)
    if cached is not None:
        cached["metadata"]["cached"] = True
        log.info("Cache HIT for: %s", prompt[:60])
        return cached

    # 2. Rule-based parser (fast path < 100ms, covers ~80% of use cases)
    intent = parse_rule(prompt, pwa_code)
    used_rule = False
    if intent is not None:
        rule_name = intent.pop("_rule_matched", "unknown")
        log.info("Rule-based match [%s] for: %s", rule_name, prompt[:60])
        used_rule = True
    else:
        # 3. LLM → intent + query (fallback)
        try:
            intent = await generate_query_intent(prompt, pwa_code)
        except Exception as exc:
            log.error("LLM error: %s", exc)
            raise HTTPException(502, detail="ไม่สามารถเชื่อมต่อ LLM ได้ค่ะ: {}".format(str(exc)))

    if intent is None:
        raise HTTPException(
            400,
            detail="ไม่สามารถเข้าใจคำถามได้ค่ะ กรุณาลองถามใหม่ เช่น 'จำนวนหัวดับเพลิงทั้งหมด'",
        )

    target_db = intent.get("target_db", "mongo")
    response_type = intent.get("response_type", "table")
    text_response = intent.get("text_response", "")
    query_info = intent.get("query", {})

    # 3. Validate
    if target_db in ("postgis", "both"):
        sql = query_info.get("postgis", {}).get("sql", "")
        if sql and not validate_sql(sql):
            raise HTTPException(
                400,
                detail={
                    "status": "error",
                    "message": "คำสั่ง SQL ไม่ปลอดภัย ระบบอนุญาตเฉพาะการอ่านข้อมูลเท่านั้นค่ะ",
                    "query_display": {"type": "sql", "code": sql},
                },
            )

    if target_db in ("mongo", "both"):
        mongo_q = query_info.get("mongo", {})
        pipeline = mongo_q.get("pipeline", [])
        if pipeline and not validate_mongo_pipeline(pipeline):
            raise HTTPException(
                400,
                detail={
                    "status": "error",
                    "message": "MongoDB pipeline ไม่ปลอดภัย ระบบอนุญาตเฉพาะการอ่านข้อมูลเท่านั้นค่ะ",
                    "query_display": {"type": "mongodb", "code": str(pipeline)},
                },
            )

    # 4. Execute
    result_data = None
    query_display = None

    try:
        if target_db == "postgis":
            sql = query_info.get("postgis", {}).get("sql", "")
            query_display = {"type": "sql", "code": sql}
            result_data = execute_postgis(sql, response_type)

        elif target_db == "mongo":
            mongo_q = query_info.get("mongo", {})
            pipeline = mongo_q.get("pipeline", [])
            layer = mongo_q.get("layer", "")
            operation = mongo_q.get("operation", "find")
            code = pwa_code or mongo_q.get("pwa_code", "")
            query_display = {"type": "mongodb", "code": _format_mongo_display(operation, layer, pipeline)}
            result_data = execute_mongo(code, layer, operation, pipeline, response_type)

        elif target_db == "both":
            # PostGIS first, then MongoDB
            pg_sql = query_info.get("postgis", {}).get("sql", "")
            mongo_q = query_info.get("mongo", {})
            query_display = {
                "type": "sql+mongodb",
                "code": "-- SQL:\n{}\n\n// MongoDB:\n{}".format(
                    pg_sql,
                    _format_mongo_display(
                        mongo_q.get("operation", "find"),
                        mongo_q.get("layer", ""),
                        mongo_q.get("pipeline", []),
                    ),
                ),
            }
            # Execute PostGIS part
            if pg_sql:
                result_data = execute_postgis(pg_sql, response_type)
            # If we also need mongo, merge or override
            if mongo_q.get("pipeline") or mongo_q.get("layer"):
                mongo_result = execute_mongo(
                    pwa_code or mongo_q.get("pwa_code", ""),
                    mongo_q.get("layer", ""),
                    mongo_q.get("operation", "find"),
                    mongo_q.get("pipeline", []),
                    response_type,
                )
                if result_data is None:
                    result_data = mongo_result

    except Exception as exc:
        log.error("Query execution error: %s", exc)
        raise HTTPException(
            500,
            detail="เกิดข้อผิดพลาดในการ query ข้อมูลค่ะ: {}".format(str(exc)),
        )

    if result_data is None:
        result_data = {"message": "ไม่พบข้อมูลค่ะ"}

    # 5. Format response
    elapsed_ms = int((time.time() - t0) * 1000)
    response = format_response(
        status="success",
        response_type=response_type,
        query_display=query_display,
        result=result_data,
        target_db=target_db,
        text_response=text_response,
        layer=query_info.get("mongo", {}).get("layer", ""),
        pwa_code=pwa_code,
        execution_time_ms=elapsed_ms,
        cached=False,
        model="rule-based" if used_rule else OLLAMA_MODEL,
    )

    # 6. Cache store
    set_cached(prompt, pwa_code, response)

    return response


def _format_mongo_display(operation, layer, pipeline):
    """Build a readable MongoDB query string for display."""
    import json
    if operation == "aggregate":
        return "db.features_<{layer}>.aggregate({pipeline})".format(
            layer=layer,
            pipeline=json.dumps(pipeline, ensure_ascii=False, indent=2),
        )
    elif operation == "count":
        filt = pipeline[0] if pipeline else {}
        return "db.features_<{layer}>.countDocuments({filt})".format(
            layer=layer,
            filt=json.dumps(filt, ensure_ascii=False, indent=2),
        )
    else:
        filt = pipeline[0] if pipeline else {}
        return "db.features_<{layer}>.find({filt}).limit(1000)".format(
            layer=layer,
            filt=json.dumps(filt, ensure_ascii=False, indent=2),
        )


# ── Main ─────────────────────────────────────────────
if __name__ == "__main__":
    import uvicorn
    uvicorn.run("main:app", host="127.0.0.1", port=PORT, reload=True)
