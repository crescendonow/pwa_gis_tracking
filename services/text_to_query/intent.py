"""
LLM integration — call Ollama and parse structured JSON intent.
"""

import json
import logging
import re

import httpx

from config import OLLAMA_BASE_URL, OLLAMA_MODEL, OLLAMA_TIMEOUT
from prompts import SYSTEM_PROMPT

log = logging.getLogger("text_to_query")


def _clean_llm_output(raw):
    """Strip <think> blocks, markdown fences, and extract JSON."""
    # Remove <think>...</think> blocks (Gemma/Qwen thinking)
    raw = re.sub(r"<think>.*?</think>", "", raw, flags=re.DOTALL)
    # Remove markdown code fences
    raw = re.sub(r"```(?:json)?\s*", "", raw)
    raw = re.sub(r"```\s*$", "", raw, flags=re.MULTILINE)
    raw = raw.strip()

    # Try to find JSON object in the text
    # Look for the first { ... } block
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


async def _call_ollama(messages, timeout=None):
    """Call Ollama native /api/chat endpoint."""
    if timeout is None:
        timeout = OLLAMA_TIMEOUT
    async with httpx.AsyncClient(timeout=timeout) as client:
        resp = await client.post(
            "{}/api/chat".format(OLLAMA_BASE_URL),
            headers={"Content-Type": "application/json"},
            json={
                "model": OLLAMA_MODEL,
                "messages": messages,
                "stream": False,
                "keep_alive": "30m",
                "options": {
                    "temperature": 0,
                    "num_predict": 1024,
                },
            },
        )
        resp.raise_for_status()
        data = resp.json()
        return data["message"]["content"]


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
    raw = await _call_ollama(messages)
    log.info("LLM raw output (attempt 1): %s", raw[:300])

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

    raw2 = await _call_ollama(messages)
    log.info("LLM raw output (attempt 2): %s", raw2[:300])

    cleaned2 = _clean_llm_output(raw2)
    try:
        return json.loads(cleaned2)
    except (json.JSONDecodeError, TypeError) as exc:
        log.error("JSON parse failed (attempt 2): %s — %s", exc, cleaned2[:200])
        return None
