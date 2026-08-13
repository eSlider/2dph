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
MIN_EVIDENCE = 2  # independent observations required for root=facts

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
    # Evidence is a node, not a substring of Leaf.source: the audit has to be
    # able to count *kinds* and *origins*, which a flattened string cannot
    # express. value_hash is reserved for re-verification (PLAN OQ6).
    conn.execute(
        "CREATE NODE TABLE IF NOT EXISTS Evidence ("
        " id STRING, kind STRING, method STRING, locator STRING, "
        " origin STRING, value_hash STRING, observed_at STRING, PRIMARY KEY(id))"
    )
    conn.execute(
        "CREATE REL TABLE IF NOT EXISTS SUPPORTS (FROM Evidence TO Leaf)"
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
                embedding: list[float] | None,
                evidence: list[dict] | None = None,
                file_path: str | None = None, repo: str = "") -> str:
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
    for item in evidence or []:
        upsert_evidence(conn, item, lid, obs)
    if file_path:
        link_to_file(conn, lid, file_path, repo, obs)
    return lid


def link_to_file(conn: ladybug.Connection, leaf: str, path: str, repo: str,
                 mtime: str) -> str:
    """Attach a leaf to the file it came from.

    Without these edges the graph is a bag of leafs: `--hop` has nothing to
    walk and the File->Commit->Person history has nothing to hang off.
    """
    fid = sha256_b64(path)[:24]
    conn.execute(
        "MERGE (f:File {id:$id}) SET f.path=$path, f.repo=$repo, f.mtime=$mtime",
        parameters={"id": fid, "path": path, "repo": repo, "mtime": mtime},
    )
    conn.execute(
        "MATCH (l:Leaf {id:$lid}), (f:File {id:$fid}) MERGE (l)-[:FROM_FILE]->(f)",
        parameters={"lid": leaf, "fid": fid},
    )
    return fid


def neighbours_of(conn: ladybug.Connection, leaf_ids: list[str]) -> list[dict]:
    """One hop: the other leafs of the same file, excluding the input set.

    This is the walk `--hop N` repeats; N hops = N rounds from the new
    frontier.
    """
    if not leaf_ids:
        return []
    r = conn.execute(
        "MATCH (l:Leaf)-[:FROM_FILE]->(f:File)<-[:FROM_FILE]-(n:Leaf) "
        "WHERE list_contains($ids, l.id) AND NOT list_contains($ids, n.id) "
        "RETURN DISTINCT n.id, n.text, n.root, n.source",
        parameters={"ids": leaf_ids},
    )
    return [{"id": row[0], "text": row[1], "root": row[2], "source": row[3]}
            for row in r.get_all()]


def upsert_evidence(conn: ladybug.Connection, item: dict, leaf: str, observed_at: str) -> None:
    """Store one Evidence node and link it to the leaf it supports.

    Evidence is shared: the same observation backing two assertions is one
    node with two SUPPORTS edges, so 'how many independent things did we
    actually look at' stays answerable.
    """
    conn.execute(
        "MERGE (e:Evidence {id:$id}) "
        "SET e.kind=$kind, e.method=$method, e.locator=$locator, "
        "    e.origin=$origin, e.value_hash=$vhash, e.observed_at=$obs",
        parameters={
            "id": item["id"], "kind": item["kind"], "method": item.get("method", ""),
            "locator": item.get("locator", ""), "origin": item.get("origin", ""),
            "vhash": item.get("value_hash", ""), "obs": observed_at,
        },
    )
    conn.execute(
        "MATCH (e:Evidence {id:$eid}), (l:Leaf {id:$lid}) MERGE (e)-[:SUPPORTS]->(l)",
        parameters={"eid": item["id"], "lid": leaf},
    )


def evidence_for(conn: ladybug.Connection, leaf: str) -> list[dict]:
    r = conn.execute(
        "MATCH (e:Evidence)-[:SUPPORTS]->(l:Leaf {id:$lid}) "
        "RETURN e.id, e.kind, e.method, e.locator, e.origin, e.observed_at",
        parameters={"lid": leaf},
    )
    return [
        {"id": row[0], "kind": row[1], "method": row[2], "locator": row[3],
         "origin": row[4], "observed_at": row[5]}
        for row in r.get_all()
    ]


def facts_lacking_independence(conn: ladybug.Connection) -> list[dict]:
    """Every facts leaf that is not backed by >=2 independent observations.

    Independent = different kind AND different origin. This is the check that
    `" x " in source` pretended to be: a fact claiming two sources while both
    are compose files, or claiming them with no Evidence at all, shows up here.
    """
    facts = conn.execute(
        "MATCH (l:Leaf) WHERE l.root=$root RETURN l.id, l.text",
        parameters={"root": ROOT_FACTS},
    ).get_all()
    problems: list[dict] = []
    for lid, text in facts:
        found = evidence_for(conn, lid)
        kinds = sorted({e["kind"] for e in found if e["kind"]})
        origins = sorted({e["origin"] for e in found if e["origin"]})
        if len(found) >= MIN_EVIDENCE and len(kinds) >= MIN_EVIDENCE and len(origins) >= MIN_EVIDENCE:
            continue
        problems.append({"id": lid, "text": text, "count": len(found),
                         "kinds": kinds, "origins": origins})
    return problems


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


def drop_indexes(conn: ladybug.Connection) -> None:
    """Drop FTS + vector indexes so bulk MERGEs don't corrupt them.

    Ladybug's FTS index goes inconsistent when rows are inserted while the
    index exists ("document for node offset N is missing during delete").
    Importers that add many leafs must drop indexes first, write, then
    recreate via create_fts_and_vector().
    """
    conn.execute("DROP INDEX IF EXISTS Leaf.Leaf_fts")
    conn.execute("DROP INDEX IF EXISTS Leaf.Leaf_vec")


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
    return db, conn