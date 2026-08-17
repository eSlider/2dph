"""kblib - the 2dph brain core over LadybugDB.

Single embedded graph `var/kb.lbug`. Two roots: facts (assertions backed by
>=2 independent sources) and info (narrative leafs). Hybrid retrieval: BM25
(FTS extension) + HNSW cosine (VECTOR extension) + Cypher graph hops.

All access is read-only unless `--rebuild` (kb/index) or `kb/add`.
"""
from __future__ import annotations

import hashlib
import json
import os
import sys
import time
import zlib
from pathlib import Path

try:
    import ladybug  # noqa: F401
except ImportError:
    # System python3 has no project deps. Re-exec this script under the repo
    # .venv python so `bin/brain/index.go --rebuild` (-> bin/kb/index) works
    # without activating the venv first. The Docker index image ships ladybug
    # into PATH python3, so this block never fires there.
    _p = Path(__file__).resolve().parent
    while True:
        if (_p / "var").is_dir() or (_p / ".git").is_dir() or (_p / "pyproject.toml").is_file():
            break
        if _p.parent == _p:
            break
        _p = _p.parent
    _venv = _p / ".venv" / "bin" / "python3"
    if _venv.is_file() and sys.argv:
        _script = Path(sys.argv[0]).resolve()
        os.execv(str(_venv), [str(_venv), str(_script)] + sys.argv[1:])
    raise

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
        " how STRING, loc STRING, type STRING, "
        " valid_from STRING, valid_to STRING, "
        " embedding FLOAT[256], "
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
    ensure_interval_columns(conn)


def ensure_interval_columns(conn: ladybug.Connection) -> None:
    """D24: add valid_from/valid_to on older Leaf tables (idempotent ALTER)."""
    for col in ("valid_from", "valid_to"):
        try:
            conn.execute(f"ALTER TABLE Leaf ADD {col} STRING")
        except Exception:
            pass


def normalize_day(s: str) -> str:
    s = (s or "").strip()
    if len(s) >= 10 and s[4] == "-" and s[7] == "-":
        return s[:10]
    return s


def active_at(valid_from: str, valid_to: str, as_of: str) -> bool:
    """D24: fact interval of truth. Empty ends = always; empty as_of = no filter."""
    as_of = normalize_day(as_of)
    if not as_of:
        return True
    fro = normalize_day(valid_from)
    to = normalize_day(valid_to)
    if fro and as_of < fro:
        return False
    if to and as_of > to:
        return False
    return True


def filter_as_of(hits: list[dict], as_of: str) -> list[dict]:
    if not as_of:
        return hits
    return [
        h for h in hits
        if active_at(str(h.get("valid_from") or ""), str(h.get("valid_to") or ""), as_of)
    ]


def leaf_id(text: str, source: str) -> str:
    return sha256_b64(f"{source}\0{text}")[:24]


def upsert_leaf(conn: ladybug.Connection, *, text: str, root: str, confidence: str,
                source: str, source_rev: str, how: str, loc: str, type_: str,
                embedding: list[float] | None,
                valid_from: str = "", valid_to: str = "") -> str:
    lid = leaf_id(text, source)
    obs = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    vf = normalize_day(valid_from)
    vt = normalize_day(valid_to)
    conn.execute(
        "MERGE (l:Leaf {id:$id}) "
        "SET l.text=$text, l.root=$root, l.confidence=$confidence, "
        "    l.sha256=$sha, l.source=$source, l.source_rev=$rev, l.observed_at=$obs, "
        "    l.how=$how, l.loc=$location, l.type=$type, "
        "    l.valid_from=$vf, l.valid_to=$vt"
        + (", l.embedding=$emb" if embedding else ""),
        parameters={
            "id": lid, "text": text, "root": root, "confidence": confidence,
            "sha": sha256_b64(text), "source": source, "rev": source_rev,
            "obs": obs, "how": how, "location": loc, "type": type_,
            "vf": vf, "vt": vt,
            "emb": (embedding if embedding else None),
        },
    )
    return lid


def add_leafs(conn: ladybug.Connection, leafs: list[dict]) -> list[str]:
    """Write facts+info leafs in one transaction. Safe while FTS/HNSW exist.

    Each leaf dict: text, source, optional root/confidence/source_rev/how/loc/type/
    embedding/valid_from/valid_to. Does not delete the database file. Measured on
    Ladybug 0.19: MERGE of new ids (and updates) stays FTS+HNSW queryable; DROP
    INDEX is the fatal path.
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
                    valid_from=str(lf.get("valid_from") or ""),
                    valid_to=str(lf.get("valid_to") or ""),
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


def file_id(repo: str, path: str) -> str:
    """Stable File.id matching gitimport (`repo:path`)."""
    return f"{repo}:{path}" if repo else path


def link_from_file(conn: ladybug.Connection, leaf_id: str, path: str,
                   repo: str = "", mtime: str = "") -> str:
    """MERGE File and Leaf-[:FROM_FILE]->File so --hop 1 can walk."""
    fid = file_id(repo, path)
    conn.execute(
        "MERGE (f:File {id:$id}) SET f.path=$path, f.repo=$repo, f.mtime=$mtime",
        parameters={"id": fid, "path": path, "repo": repo, "mtime": mtime},
    )
    conn.execute(
        "MATCH (l:Leaf {id:$lid}), (f:File {id:$fid}) "
        "MERGE (l)-[:FROM_FILE]->(f)",
        parameters={"lid": leaf_id, "fid": fid},
    )
    return fid


HOP_STMTS = {
    1: "MATCH (l:Leaf {id:$id})-[:FROM_FILE]->(f:File) RETURN f.id, f.path, 1",
    2: ("MATCH (l:Leaf {id:$id})-[:FROM_FILE]->(f:File)-[:HAS_VERSION]->(c:Commit) "
        "RETURN c.id, c.subject, 2"),
    3: ("MATCH (l:Leaf {id:$id})-[:FROM_FILE]->(f:File)-[:HAS_VERSION]->(c:Commit)"
        "-[:AUTHORED]->(p:Person) RETURN p.id, p.name, 3"),
}
HOP_LABELS = {1: "File", 2: "Commit", 3: "Person"}


def hop_walk(conn: ladybug.Connection, leaf_id: str, n: int) -> list[dict]:
    """Walk Leaf → File → Commit → Person up to n hops (max 3)."""
    depth = min(max(int(n), 0), 3)
    out: list[dict] = []
    for d in range(1, depth + 1):
        rows = conn.execute(HOP_STMTS[d], parameters={"id": leaf_id}).get_all()
        for row in rows:
            out.append({
                "id": row[0],
                "label": HOP_LABELS[d],
                "name": row[1],
                "depth": int(row[2]),
            })
    return out


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
        "RETURN node.id, node.text, node.root, score, node.valid_from, node.valid_to "
        "ORDER BY score DESC LIMIT $n",
        parameters={"q": text, "n": limit},
    )
    return [
        {
            "id": row[0], "text": row[1], "root": row[2], "score": row[3],
            "valid_from": row[4] or "", "valid_to": row[5] or "",
        }
        for row in r.get_all()
    ]


def query_vector(conn: ladybug.Connection, embedding: list[float], limit: int = 10) -> list[dict]:
    r = conn.execute(
        "CALL QUERY_VECTOR_INDEX('Leaf', 'Leaf_vec', $q, $n) "
        "RETURN node.id, node.text, node.root, distance, node.valid_from, node.valid_to "
        "ORDER BY distance LIMIT $n",
        parameters={"q": embedding, "n": limit},
    )
    out = []
    for row in r.get_all():
        score = 1.0 - row[3] if row[3] is not None else 0.0
        out.append({
            "id": row[0], "text": row[1], "root": row[2], "score": score,
            "valid_from": row[4] or "", "valid_to": row[5] or "",
        })
    return out


def hybrid_search(conn: ladybug.Connection, embedding: list[float], fts_hits: list[dict],
                  limit: int = 10, as_of: str = "") -> list[dict]:
    """Merge FTS + vector by reciprocal rank fusion; optional D24 as-of filter."""
    fused: dict[str, dict] = {}
    for rank, hit in enumerate(fts_hits):
        fused.setdefault(hit["id"], {**hit, "rrf": 0.0})["rrf"] = 1.0 / (60 + rank + 1)
    for rank, hit in enumerate(query_vector(conn, embedding, limit * 3)):
        entry = fused.setdefault(hit["id"], {**hit, "rrf": 0.0})
        entry["rrf"] += 1.0 / (60 + rank + 1)
        entry.setdefault("score", hit.get("score", 0.0))
        entry.setdefault("valid_from", hit.get("valid_from") or "")
        entry.setdefault("valid_to", hit.get("valid_to") or "")
    ranked = sorted(fused.values(), key=lambda h: h.get("rrf", 0.0), reverse=True)
    return filter_as_of(ranked, as_of)[:limit]


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