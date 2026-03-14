"""
LLM integration — call Gemini Flash API and parse structured JSON intent.
"""

import asyncio
import json
import logging
import re

import httpx

from config import GEMINI_API_KEY, GEMINI_MODEL, LLM_TIMEOUT
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


# ── Gemini Flash ──────────────────────────────────────────

async def _call_gemini(messages, timeout=None):
    """Call Google Gemini API (generateContent endpoint)."""
    if not GEMINI_API_KEY:
        raise RuntimeError("GEMINI_API_KEY not configured")

    if timeout is None:
        timeout = LLM_TIMEOUT

    # Convert messages to Gemini format
    contents = []
    system_text = ""
    for msg in messages:
        if msg["role"] == "system":
            system_text = msg["content"]
        elif msg["role"] == "user":
            contents.append({"role": "user", "parts": [{"text": msg["content"]}]})
        elif msg["role"] == "assistant":
            contents.append({"role": "model", "parts": [{"text": msg["content"]}]})

    body = {
        "contents": contents,
        "generationConfig": {
            "temperature": 0,
            "maxOutputTokens": 1024,
            "responseMimeType": "application/json",
            "responseSchema": {
                "type": "OBJECT",
                "properties": {
                    "text_response": {"type": "STRING"},
                    "target_db": {"type": "STRING", "enum": ["mongo", "postgis"]},
                    "response_type": {"type": "STRING", "enum": ["geojson", "numeric", "table"]},
                    "intent_summary": {"type": "STRING"},
                    "query": {
                        "type": "OBJECT",
                        "properties": {
                            "mongo": {
                                "type": "OBJECT",
                                "properties": {
                                    "pwa_code": {"type": "STRING", "nullable": True},
                                    "layer": {
                                        "type": "STRING",
                                        "enum": [
                                            "pipe", "valve", "firehydrant", "meter",
                                            "bldg", "leakpoint", "pwa_waterworks", "dma_boundary",
                                        ],
                                    },
                                    "pipeline": {"type": "ARRAY", "items": {"type": "OBJECT"}},
                                    "operation": {"type": "STRING", "enum": ["find", "aggregate", "count"]},
                                },
                            },
                            "postgis": {
                                "type": "OBJECT",
                                "properties": {
                                    "sql": {"type": "STRING"},
                                },
                            },
                        },
                    },
                },
                "required": ["text_response", "target_db", "response_type", "intent_summary", "query"],
            },
        },
    }
    if system_text:
        body["systemInstruction"] = {"parts": [{"text": system_text}]}

    url = "https://generativelanguage.googleapis.com/v1beta/models/{}:generateContent?key={}".format(
        GEMINI_MODEL, GEMINI_API_KEY
    )

    max_retries = 3
    for attempt in range(max_retries):
        async with httpx.AsyncClient(timeout=timeout) as client:
            resp = await client.post(url, json=body)

            if resp.status_code == 429 and attempt < max_retries - 1:
                wait = 2 ** attempt  # 1s, 2s
                log.warning("Gemini 429 rate limited, retry in %ds (attempt %d/%d)", wait, attempt + 1, max_retries)
                await asyncio.sleep(wait)
                continue

            resp.raise_for_status()
            data = resp.json()

        # Extract text from response
        candidates = data.get("candidates", [])
        if not candidates:
            raise RuntimeError("Gemini returned no candidates")

        parts = candidates[0].get("content", {}).get("parts", [])
        if not parts:
            raise RuntimeError("Gemini returned no content parts")

        return parts[0].get("text", "")

    raise RuntimeError("Gemini rate limited after {} retries".format(max_retries))


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
    raw = await _call_gemini(messages)
    log.info("LLM [%s] raw output (attempt 1): %s", GEMINI_MODEL, raw[:300])

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

    raw2 = await _call_gemini(messages)
    log.info("LLM [%s] raw output (attempt 2): %s", GEMINI_MODEL, raw2[:300])

    cleaned2 = _clean_llm_output(raw2)
    try:
        return json.loads(cleaned2)
    except (json.JSONDecodeError, TypeError) as exc:
        log.error("JSON parse failed (attempt 2): %s — %s", exc, cleaned2[:200])
        return None
