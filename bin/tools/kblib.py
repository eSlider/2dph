"""kblib - the 2dph brain core over LadybugDB.

Single embedded graph `var/kb.lbug`. Two roots: facts (assertions backed by
>=2 independent sources) and info (narrative leafs). Hybrid retrieval: BM25
(FTS extension) + HNSW cosine (VECTOR extension) + Cypher graph hops.

All access is read-only unless `--rebuild` (kb/index) or `kb/add`.
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

def _repo_root() -> Path:
    p = Path(__file__).resolve().parent
    while True:
        if (p / "var").is_dir() or (p / ".git").is_dir() or (p / "pyproject.toml").is_file():
            return p
        if p.parent == p:
            return Path(__file__).resolve().parents[2]
        p = p.parent


VAR = _repo_root() / "var"
DB_PATH = VAR / "kb.lbug"


def sha256_b64(text: str) -> str:
    return hashlib.sha256(text.encode()).hexdigest()


def _load_extension(conn: ladybug.Connection, name: str) -> None:
    """Install (download once) and load a ladybug extension."""
    try:
        conn.execute(f"INSTALL {name}")
    except Exception:
        pass  # already installed / offline-ok when present
    conn.execute(f"LOAD EXTENSION {name}")


def connect(path: Path | str | None = None, read_only: bool = True) -> tuple[ladybug.Database, ladybug.Connection]:
    db = ladybug.Database(str(path or DB_PATH), read_only=read_only)
    conn = ladybug.Connection(db)
    _load_extension(conn, "FTS")
    _load_extension(conn, "VECTOR")
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
    conn.execute(
        "CREATE NODE TABLE IF NOT EXISTS Commit (id STRING, repo STRING, subject STRING, "
        "author STRING, email STRING, date STRING, PRIMARY KEY(id))"
    )
    conn.execute(
        "CREATE NODE TABLE IF NOT EXISTS Person (id STRING, name STRING, email STRING, PRIMARY KEY(id))"
    )
    conn.execute(
        "CREATE REL TABLE IF NOT EXISTS HAS_VERSION (FROM File TO Commit)"
    )
    conn.execute(
        "CREATE REL TABLE IF NOT EXISTS AUTHORED (FROM Commit TO Person)"
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


def add_leafs(conn: ladybug.Connection, leafs: list[dict]) -> list[str]:
    """Write facts+info leafs in one transaction. Safe while FTS/HNSW exist.

    Each leaf dict: text, source, optional root/confidence/source_rev/how/loc/type/embedding.
    Does not delete the database file. Measured on Ladybug 0.19: MERGE of new
    ids (and updates) stays FTS+HNSW queryable; DROP INDEX is the fatal path.
    """
    if not leafs:
        return []
    started = False
    try:
        conn.execute("BEGIN TRANSACTION")
        started = True
    except Exception:
        started = False
    ids: list[str] = []
    try:
        for lf in leafs:
            ids.append(
                upsert_leaf(
                    conn,
                    text=str(lf["text"]),
                    root=str(lf.get("root") or ROOT_INFO),
                    confidence=str(lf.get("confidence") or CONF_CONFIRMED),
                    source=str(lf["source"]),
                    source_rev=str(lf.get("source_rev") or "working-tree"),
                    how=str(lf.get("how") or "brain/add"),
                    loc=str(lf.get("loc") or lf.get("source") or ""),
                    type_=str(lf.get("type") or lf.get("type_") or "reference"),
                    embedding=lf.get("embedding"),
                )
            )
        if started:
            conn.execute("COMMIT")
    except Exception:
        if started:
            try:
                conn.execute("ROLLBACK")
            except Exception:
                pass
        raise
    return ids


def leaf_index_names(conn: ladybug.Connection) -> set[str]:
    """Return index names on the Leaf table (e.g. {'id', 'Leaf_vec', '_PK'})."""
    rows = conn.execute("CALL SHOW_INDEXES() RETURN *").get_all()
    return {row[1] for row in rows if row[0] == "Leaf"}


def create_fts_and_vector(conn: ladybug.Connection, force: bool = False) -> None:
    """Create FTS (BM25) + HNSW vector indexes if missing.

    Never DROP INDEX for FTS/VECTOR. Ladybug 0.19 leaves ghost catalog
    entries after DROP (`_0_Leaf_vec_UPPER`, `0_id_docs`), so a later
    CREATE fails with "already exists in catalog" while SHOW_INDEXES
    still omits the index. Swallowing that error made HNSW look "OK"
    until the first QUERY_VECTOR_INDEX.

    `force=True` is accepted for API compatibility but does **not** drop.
    Fresh indexes require deleting `var/kb.lbug` and rebuilding
    (`bin/brain/index.go --rebuild`).
    """
    del force  # API compat; DROP is unsafe — see docstring
    names = leaf_index_names(conn)
    if "id" not in names:
        try:
            conn.execute("CALL CREATE_FTS_INDEX('Leaf', 'id', ['text'])")
        except Exception as e:
            raise RuntimeError(
                "CREATE_FTS_INDEX failed (often ghost catalog after DROP INDEX). "
                "Delete var/kb.lbug and run bin/brain/index.go --rebuild. "
                f"Cause: {e}"
            ) from e
    if "Leaf_vec" not in names:
        try:
            conn.execute(
                "CALL CREATE_VECTOR_INDEX('Leaf', 'Leaf_vec', 'embedding', "
                "metric := 'cosine')"
            )
        except Exception as e:
            raise RuntimeError(
                "CREATE_VECTOR_INDEX failed (often ghost catalog after DROP INDEX "
                "Leaf.Leaf_vec → `_0_Leaf_vec_UPPER already exists in catalog`). "
                "Delete var/kb.lbug and run bin/brain/index.go --rebuild. "
                f"Cause: {e}"
            ) from e
    names = leaf_index_names(conn)
    missing = {"id", "Leaf_vec"} - names
    if missing:
        raise RuntimeError(
            f"Leaf indexes incomplete after create: missing {sorted(missing)}; "
            f"have {sorted(names)}. Delete var/kb.lbug and --rebuild."
        )


def ensure_indexes(conn: ladybug.Connection) -> None:
    """Idempotent: create FTS + HNSW only when missing. Safe after upserts."""
    create_fts_and_vector(conn, force=False)


def drop_indexes(conn: ladybug.Connection) -> None:
    """No-op. Kept for callers; DROP INDEX is fatal on Ladybug 0.19.

    Historical note claimed "drop before bulk MERGE". Measured on 0.19:
    - DROP FTS/VECTOR leaves ghost catalog → CREATE fails permanently until
      `var/kb.lbug` is deleted.
    - MERGE/upsert while **FTS** exists can corrupt FTS
      ("document for node offset N is missing during delete").
    - Upsert while **HNSW** exists stays queryable.

    Bulk rebuilders must delete `var/kb.lbug`, write all leafs (info+facts)
    with no indexes, then `ensure_indexes()` once.
    """
    return


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
        raise FileNotFoundError(f"{DB_PATH} missing - run bin/brain/index.go --rebuild first")
    db, conn = connect(read_only=True)
    return db, conn