"""kblib - the 2dph brain core over LadybugDB.

Single embedded graph `var/kb.lbug`. Two roots: facts (assertions backed by
>=2 independent sources) and info (narrative leafs). Hybrid retrieval: BM25
(FTS extension) + HNSW cosine (VECTOR extension) + Cypher graph hops.

All access is read-only unless `--rebuild` is passed to kb/index.
"""
from __future__ import annotations

import hashlib
import json
import time
import zlib
from pathlib import Path

import ladybug

MODEL = "minishlab/potion-multilingual-128M"
EMBED_DIM = 256
ROOT_FACTS = "facts"
ROOT_INFO = "info"
CONF_CONFIRMED = "confirmed"

VAR = Path(__file__).resolve().parents[1] / "var"
DB_PATH = VAR / "kb.lbug"


def sha256_b64(text: str) -> str:
    return hashlib.sha256(text.encode()).hexdigest()


def connect(path: Path | str | None = None, read_only: bool = True) -> tuple[ladybug.Database, ladybug.Connection]:
    db = ladybug.Database(str(path or DB_PATH), read_only=read_only)
    conn = ladybug.Connection(db)
    conn.execute("LOAD EXTENSION FTS")
    conn.execute("LOAD EXTENSION VECTOR")
    return db, conn


def init_schema(conn: ladybug.Connection) -> None:
    conn.execute(
        "CREATE NODE TABLE IF NOT EXISTS Leaf ("
        " id STRING, text STRING, root STRING, confidence STRING, "
        " sha256 STRING, source STRING, source_rev STRING, observed_at STRING, "
        " how STRING, loc STRING, type STRING, embedding FLOAT[256], "
        " PRIMARY KEY(id))"
    )
    conn.execute(
        "CREATE NODE TABLE IF NOT EXISTS File ("
        " id STRING, path STRING, repo STRING, mtime STRING, PRIMARY KEY(id))"
    )
    conn.execute(
        "CREATE REL TABLE IF NOT EXISTS FROM_FILE (FROM Leaf TO File)"
    )
    conn.execute(
        "CREATE NODE TABLE IF NOT EXISTS Host (id STRING, hostname STRING, user STRING, PRIMARY KEY(id))"
    )
    conn.execute(
        "CREATE REL TABLE IF NOT EXISTS RUNS_ON (FROM Leaf TO Host)"
    )


def leaf_id(text: str, source: str) -> str:
    return sha256_b64(f"{source}\0{text}")[:24]


def upsert_leaf(conn: ladybug.Connection, *, text: str, root: str, confidence: str,
                source: str, source_rev: str, how: str, loc: str, type_: str,
                embedding: list[float] | None) -> str:
    lid = leaf_id(text, source)
    obs = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    conn.execute(
        "MERGE (l:Leaf {id:$id}) "
        "SET l.text=$text, l.root=$root, l.confidence=$confidence, "
        "    l.sha256=$sha, l.source=$source, l.source_rev=$rev, l.observed_at=$obs, "
        "    l.how=$how, l.loc=$location, l.type=$type"
        + (", l.embedding=$emb" if embedding else ""),
        parameters={
            "id": lid, "text": text, "root": root, "confidence": confidence,
            "sha": sha256_b64(text), "source": source, "rev": source_rev,
            "obs": obs, "how": how, "location": loc, "type": type_,
            "emb": (embedding if embedding else None),
        },
    )
    return lid


def create_fts_and_vector(conn: ladybug.Connection, force: bool = False) -> None:
    if force:
        conn.execute("DROP INDEX IF EXISTS Leaf.Leaf_fts")
        conn.execute("DROP INDEX IF EXISTS Leaf.Leaf_vec")
    try:
        conn.execute("CALL CREATE_FTS_INDEX('Leaf', 'id', ['text'])")
    except Exception:
        pass
    try:
        conn.execute("CALL CREATE_VECTOR_INDEX('Leaf', 'Leaf_vec', 'embedding', metric := 'cosine')")
    except Exception:
        pass


def query_fts(conn: ladybug.Connection, text: str, limit: int = 10) -> list[dict]:
    r = conn.execute(
        "CALL QUERY_FTS_INDEX('Leaf', 'id', $q) "
        "RETURN node.id, node.text, node.root, score ORDER BY score DESC LIMIT $n",
        parameters={"q": text, "n": limit},
    )
    return [{"id": row[0], "text": row[1], "root": row[2], "score": row[3]} for row in r.get_all()]


def query_vector(conn: ladybug.Connection, embedding: list[float], limit: int = 10) -> list[dict]:
    r = conn.execute(
        "CALL QUERY_VECTOR_INDEX('Leaf', 'Leaf_vec', $q, $n) "
        "RETURN node.id, node.text, node.root, distance ORDER BY distance LIMIT $n",
        parameters={"q": embedding, "n": limit},
    )
    out = []
    for row in r.get_all():
        # distance -> similarity reasonable for cosine
        score = 1.0 - row[3] if row[3] is not None else 0.0
        out.append({"id": row[0], "text": row[1], "root": row[2], "score": score})
    return out


def hybrid_search(conn: ladybug.Connection, embedding: list[float], fts_hits: list[dict],
                  limit: int = 10) -> list[dict]:
    """Merge FTS + vector by reciprocal rank fusion."""
    fused: dict[str, dict] = {}
    for rank, hit in enumerate(fts_hits):
        fused.setdefault(hit["id"], {**hit, "rrf": 0.0})["rrf"] = 1.0 / (60 + rank + 1)
    for rank, hit in enumerate(query_vector(conn, embedding, limit * 3)):
        entry = fused.setdefault(hit["id"], {**hit, "rrf": 0.0})
        entry["rrf"] += 1.0 / (60 + rank + 1)
        entry.setdefault("score", hit.get("score", 0.0))
    ranked = sorted(fused.values(), key=lambda h: h.get("rrf", 0.0), reverse=True)
    return ranked[:limit]


def stats(conn: ladybug.Connection) -> dict:
    r = conn.execute("MATCH (l:Leaf) RETURN l.root, count(*)")
    rows = {row[0]: row[1] for row in r.get_all()}
    total = conn.execute("MATCH (l:Leaf) RETURN count(*)").get_all()[0][0]
    return {"total": total, "by_root": rows, "db": str(DB_PATH), "model": MODEL}


def open_readonly() -> tuple[ladybug.Database, ladybug.Connection]:
    if not DB_PATH.exists():
        raise FileNotFoundError(f"{DB_PATH} missing - run bin/kb/index first")
    db, conn = connect(read_only=True)
    init_schema(conn)
    return db, conn