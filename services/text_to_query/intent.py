"""
LLM integration — call OpenRouter (Qwen3-Coder) API and parse structured JSON intent.
"""

import asyncio
import json
import logging
import re

import httpx

from config import OPENROUTER_API_KEY, OPENROUTER_MODEL, LLM_TIMEOUT
from prompts import SYSTEM_PROMPT

log = logging.getLogger("text_to_query")


def _clean_llm_output(raw):
    """Strip <think> blocks, markdown fences, and extract JSON."""
    raw = re.sub(r"<think>.*?</think>", "", raw, flags=re.DOTALL)
    raw = re.sub(r"```(?:json)?\s*", "", raw)
    raw = re.sub(r"```\s*$", "", raw, flags=re.MULTILINE)
    raw = raw.strip()

    depth = 0
    start = None
    for i, ch in enumerate(raw):
        if ch == "{":
            if start is None:
                start = i
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0 and start is not None:
                return raw[start : i + 1]

    return raw


# ── OpenRouter (Qwen3-Coder) ──────────────────────────────

_INTENT_SCHEMA = {
    "type": "object",
    "properties": {
        "text_response": {"type": "string"},
        "target_db": {"type": "string", "enum": ["mongo", "postgis"]},
        "response_type": {"type": "string", "enum": ["geojson", "numeric", "table"]},
        "intent_summary": {"type": "string"},
        "query": {
            "type": "object",
            "properties": {
                "mongo": {
                    "type": "object",
                    "properties": {
                        "pwa_code": {"type": ["string", "null"]},
                        "layer": {
                            "type": "string",
                            "enum": [
                                "pipe", "valve", "firehydrant", "meter",
                                "bldg", "leakpoint", "pwa_waterworks", "struct",
                                "pipe_serv", "dma_boundary", "step_test", "flow_meter",
                            ],
                        },
                        "pipeline": {"type": "array", "items": {"type": "object"}},
                        "operation": {"type": "string", "enum": ["find", "aggregate", "count"]},
                    },
                },
                "postgis": {
                    "type": "object",
                    "properties": {
                        "sql": {"type": "string"},
                    },
                },
            },
        },
    },
    "required": ["text_response", "target_db", "response_type", "intent_summary", "query"],
}


async def _call_openrouter(messages, timeout=None):
    """Call OpenRouter API (chat completions endpoint) with Qwen3-Coder."""
    if not OPENROUTER_API_KEY:
        raise RuntimeError("OPENROUTER_API_KEY not configured")

    if timeout is None:
        timeout = LLM_TIMEOUT

    body = {
        "model": OPENROUTER_MODEL,
        "messages": messages,
        "temperature": 0,
        "max_tokens": 1024,
        "response_format": {
            "type": "json_schema",
            "json_schema": {"name": "query_intent", "schema": _INTENT_SCHEMA},
        },
        # ให้ OpenRouter route ไปเฉพาะ provider ที่รองรับ response_format
        "provider": {"require_parameters": True},
    }

    url = "https://openrouter.ai/api/v1/chat/completions"
    headers = {"Authorization": "Bearer {}".format(OPENROUTER_API_KEY), "X-Title": "PWA GIS Text-to-Query"}

    max_retries = 3
    for attempt in range(max_retries):
        async with httpx.AsyncClient(timeout=timeout) as client:
            resp = await client.post(url, json=body, headers=headers)

            if resp.status_code == 429 and attempt < max_retries - 1:
                wait = 2 ** attempt  # 1s, 2s
                log.warning("OpenRouter 429 rate limited, retry in %ds (attempt %d/%d)", wait, attempt + 1, max_retries)
                await asyncio.sleep(wait)
                continue

            resp.raise_for_status()
            data = resp.json()

        # Extract text from response
        choices = data.get("choices", [])
        if not choices:
            raise RuntimeError("OpenRouter returned no choices")

        content = choices[0].get("message", {}).get("content", "")
        if not content:
            raise RuntimeError("OpenRouter returned empty content")

        return content

    raise RuntimeError("OpenRouter rate limited after {} retries".format(max_retries))


# ── Main entry point ──────────────────────────────────────

async def generate_query_intent(prompt, pwa_code=""):
    """
    Send user prompt to LLM and get back structured intent JSON.
    Returns dict with target_db, response_type, query, etc.
    Returns None if parsing fails after retry.
    """
    user_msg = prompt
    if pwa_code:
        user_msg = "(context: pwa_code={}) {}".format(pwa_code, prompt)

    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": user_msg},
    ]

    # First attempt
    raw = await _call_openrouter(messages)
    log.info("LLM [%s] raw output (attempt 1): %s", OPENROUTER_MODEL, raw[:300])

    cleaned = _clean_llm_output(raw)
    try:
        return json.loads(cleaned)
    except (json.JSONDecodeError, TypeError) as exc:
        log.warning("JSON parse failed (attempt 1): %s — %s", exc, cleaned[:200])

    # Retry with explicit instruction
    messages.append({"role": "assistant", "content": raw})
    messages.append({
        "role": "user",
        "content": "คำตอบก่อนหน้าไม่ใช่ JSON ที่ถูกต้อง กรุณาตอบเป็น JSON เท่านั้น ตาม format ที่กำหนด",
    })

    raw2 = await _call_openrouter(messages)
    log.info("LLM [%s] raw output (attempt 2): %s", OPENROUTER_MODEL, raw2[:300])

    cleaned2 = _clean_llm_output(raw2)
    try:
        return json.loads(cleaned2)
    except (json.JSONDecodeError, TypeError) as exc:
        log.error("JSON parse failed (attempt 2): %s — %s", exc, cleaned2[:200])
        return None
