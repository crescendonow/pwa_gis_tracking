"""
Configuration — environment variables & database connections.
"""

import os
import logging
from urllib.parse import quote_plus

from dotenv import load_dotenv
from pymongo import MongoClient
from sqlalchemy import create_engine

load_dotenv()

log = logging.getLogger("text_to_query")

# ── Service ──────────────────────────────────────────
PORT = int(os.getenv("PORT", "5022"))

# ── LLM Provider ────────────────────────────────────
# "gemini" (default, fast & cheap) or "ollama" (local, free but slow)
LLM_PROVIDER = os.getenv("LLM_PROVIDER", "gemini")
LLM_TIMEOUT = int(os.getenv("LLM_TIMEOUT", "30"))

# ── Gemini Flash ────────────────────────────────────
GEMINI_API_KEY = os.getenv("GEMINI_API_KEY", "")
GEMINI_MODEL = os.getenv("GEMINI_MODEL", "gemini-2.0-flash")

# ── Ollama (fallback) ──────────────────────────────
OLLAMA_BASE_URL = os.getenv("OLLAMA_BASE_URL", "http://localhost:11434")
OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "scb10x/typhoon2.1-gemma3-4b")
OLLAMA_TIMEOUT = int(os.getenv("OLLAMA_TIMEOUT", "600"))

# ── PostgreSQL (PostGIS) ─────────────────────────────
PG_HOST = os.getenv("PG_HOST", "192.168.242.38")
PG_PORT = os.getenv("PG_PORT", "5432")
PG_DB = os.getenv("PG_DB", "pgweb_gis2")
PG_USER = os.getenv("PG_USER", "gispwadb")
PG_PASS = os.getenv("PG_PASS", "")
PG_SSLMODE = os.getenv("PG_SSLMODE", "disable")

_pg_url = (
    "postgresql://{user}:{password}@{host}:{port}/{db}"
    "?sslmode={sslmode}&client_encoding=utf8"
).format(
    user=quote_plus(PG_USER), password=quote_plus(PG_PASS),
    host=PG_HOST, port=PG_PORT,
    db=PG_DB, sslmode=PG_SSLMODE,
)

pg_engine = None
try:
    pg_engine = create_engine(_pg_url, pool_pre_ping=True, pool_size=5, max_overflow=2)
    log.info("PostgreSQL engine created")
except Exception as exc:
    log.error("PostgreSQL engine failed: %s", exc)

# ── MongoDB ──────────────────────────────────────────
MONGO_URI = os.getenv("MONGO_URI", "mongodb://localhost:27017")
MONGO_DB_NAME = os.getenv("MONGO_DB_NAME", "vallaris_feature")

mongo_client = None
mongo_db = None
try:
    mongo_client = MongoClient(MONGO_URI, serverSelectionTimeoutMS=10000)
    mongo_client.server_info()  # force connection test
    mongo_db = mongo_client[MONGO_DB_NAME]
    log.info("MongoDB connected — db=%s", MONGO_DB_NAME)
except Exception as exc:
    log.error("MongoDB connection failed: %s", exc)

# ── Cache ────────────────────────────────────────────
CACHE_TTL_HOURS = int(os.getenv("CACHE_TTL_HOURS", "24"))
CACHE_MAX_ENTRIES = int(os.getenv("CACHE_MAX_ENTRIES", "10000"))

# ── Rate Limit ───────────────────────────────────────
RATE_LIMIT = os.getenv("RATE_LIMIT", "10/minute")
