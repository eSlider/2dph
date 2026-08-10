"""SearXNG client that is safe for agents to share.

Three things make this more than a curl wrapper:

  * An empty result set from this instance usually means "throttled", not "no
    matches". Reporting it as absence would make an agent state something false,
    so `classify` never returns a word that sounds like a negative finding.
  * Queries leave the host, so `phi_reason` refuses anything that smells like
    patient data before it reaches an external engine.
  * Results are cached and calls are serialised, because the instance suspends
    engines under load.
"""
from __future__ import annotations

import hashlib
import json
import re
import sqlite3
import time
from pathlib import Path

SNIPPET_CHARS = 150
DEFAULT_LIMIT = 5
MIN_INTERVAL = 10.0
CACHE_TTL = 7 * 24 * 3600
RETRY_BACKOFF = (20.0, 60.0)


# --------------------------------------------------------------------------
# response handling
# --------------------------------------------------------------------------

def classify(payload: dict) -> str:
    """`ok` when at least one engine answered, `throttled` otherwise."""
    return "ok" if payload.get("results") else "throttled"


def project(payload: dict, limit: int = DEFAULT_LIMIT,
            snippet_chars: int = SNIPPET_CHARS) -> dict:
    """Keep the few fields worth spending context on."""
    status = classify(payload)
    results = []
    for rank, item in enumerate(payload.get("results", [])[:limit], start=1):
        snippet = re.sub(r"\s+", " ", item.get("content") or "").strip()
        if len(snippet) > snippet_chars:
            snippet = snippet[:snippet_chars].rstrip() + "..."
        results.append({
            "rank": rank,
            "title": item.get("title", ""),
            "url": item.get("url", ""),
            "snippet": snippet,
            "engine": item.get("engine", ""),
        })
    out = {
        "query": payload.get("query", ""),
        "status": status,
        "results": results,
    }
    unresponsive = [f"{name}: {reason}" for name, reason in
                    payload.get("unresponsive_engines", [])]
    if unresponsive:
        out["unresponsive"] = unresponsive
    if status == "throttled":
        out["note"] = ("no engine answered - this is a throttled instance, "
                       "not evidence that nothing exists")
    return out


# --------------------------------------------------------------------------
# cache key and throttling
# --------------------------------------------------------------------------

def cache_key(query: str, params: dict) -> str:
    norm = " ".join(query.lower().split())
    stable = json.dumps(params, sort_keys=True, ensure_ascii=False)
    return hashlib.sha256(f"{norm}\x00{stable}".encode()).hexdigest()


def wait_for(last: float | None, now: float, interval: float = MIN_INTERVAL) -> float:
    """Seconds to sleep so that calls stay `interval` apart."""
    if last is None:
        return 0.0
    return max(0.0, interval - (now - last))


# --------------------------------------------------------------------------
# PHI guard
# --------------------------------------------------------------------------

PHI_PATTERNS = [
    (re.compile(r"\d{6,}"), "a run of six or more digits looks like an ID"),
    (re.compile(r"\bpersonalnummer\b", re.I), "Personalnummer is staff data"),
    (re.compile(r"\bkv[-\s]?nr\b", re.I), "KV-Nr is an insurance number"),
    (re.compile(r"\bversichertennummer\b", re.I), "insurance number"),
    (re.compile(r"\b[A-Za-zÄÖÜäöüß]+(?:stra(?:ss|ß)e|str\.)\s*\d+", re.I),
     "a street with a house number looks like an address"),
    (re.compile(r"\bgeb(?:urtsdatum)?\.?\s*\d{1,2}[./]\d{1,2}[./]\d{2,4}", re.I),
     "a date of birth"),
]


def phi_reason(query: str) -> str | None:
    """Why this query must not be sent, or None when it is safe."""
    for pattern, reason in PHI_PATTERNS:
        if pattern.search(query):
            return reason
    return None


# --------------------------------------------------------------------------
# cache storage
# --------------------------------------------------------------------------

CACHE_SCHEMA = """
CREATE TABLE IF NOT EXISTS responses (
  key     TEXT PRIMARY KEY,
  fetched REAL NOT NULL,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value REAL NOT NULL
);
"""


def open_cache(path: Path) -> sqlite3.Connection:
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path, timeout=30)
    conn.row_factory = sqlite3.Row
    conn.executescript(CACHE_SCHEMA)
    return conn


def cache_get(conn: sqlite3.Connection, key: str, ttl: float = CACHE_TTL,
              now: float | None = None) -> dict | None:
    now = time.time() if now is None else now
    row = conn.execute("SELECT fetched, payload FROM responses WHERE key = ?",
                       (key,)).fetchone()
    if row is None or now - row["fetched"] > ttl:
        return None
    return json.loads(row["payload"])


def cache_put(conn: sqlite3.Connection, key: str, payload: dict,
              now: float | None = None) -> None:
    now = time.time() if now is None else now
    conn.execute("INSERT OR REPLACE INTO responses (key, fetched, payload) VALUES (?, ?, ?)",
                 (key, now, json.dumps(payload, ensure_ascii=False)))
    conn.commit()


def last_call(conn: sqlite3.Connection) -> float | None:
    row = conn.execute("SELECT value FROM meta WHERE key = 'last_call'").fetchone()
    return row["value"] if row else None


def mark_call(conn: sqlite3.Connection, now: float | None = None) -> None:
    now = time.time() if now is None else now
    conn.execute("INSERT OR REPLACE INTO meta (key, value) VALUES ('last_call', ?)", (now,))
    conn.commit()
