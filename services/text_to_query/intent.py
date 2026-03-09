"""
LLM integration — call Cloud LLM API and parse structured JSON intent.
Supports: Gemini Flash (primary), with Ollama as optional fallback.
"""

import json
import logging
import re

import httpx

from config import (
    LLM_PROVIDER,
    GEMINI_API_KEY, GEMINI_MODEL,
    OLLAMA_BASE_URL, OLLAMA_MODEL, OLLAMA_TIMEOUT,
    LLM_TIMEOUT,
)
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
        },
    }
    if system_text:
        body["systemInstruction"] = {"parts": [{"text": system_text}]}

    url = "https://generativelanguage.googleapis.com/v1beta/models/{}:generateContent?key={}".format(
        GEMINI_MODEL, GEMINI_API_KEY
    )

    async with httpx.AsyncClient(timeout=timeout) as client:
        resp = await client.post(url, json=body)
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


# ── Ollama (fallback) ─────────────────────────────────────

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


# ── Main entry point ──────────────────────────────────────

async def _call_llm(messages):
    """Route to configured LLM provider."""
    if LLM_PROVIDER == "gemini":
        return await _call_gemini(messages)
    else:
        return await _call_ollama(messages)


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
    raw = await _call_llm(messages)
    log.info("LLM [%s] raw output (attempt 1): %s", LLM_PROVIDER, raw[:300])

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

    raw2 = await _call_llm(messages)
    log.info("LLM [%s] raw output (attempt 2): %s", LLM_PROVIDER, raw2[:300])

    cleaned2 = _clean_llm_output(raw2)
    try:
        return json.loads(cleaned2)
    except (json.JSONDecodeError, TypeError) as exc:
        log.error("JSON parse failed (attempt 2): %s — %s", exc, cleaned2[:200])
        return None
