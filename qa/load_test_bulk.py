#!/usr/bin/env python3
"""Load test: bulk insert performance (writing facts to the brain)."""
from __future__ import annotations

import json
import time
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "bin" / "tools"))

from kblib import connect, init_schema, upsert_leaf, ensure_indexes
from model2vec import StaticModel


DB_PATH = ROOT / "var" / "kb.lbug"


def run_bulk_tests(count: int = 100, _drop_indexes: bool = True) -> dict:
    results: dict = {}

    print(f"Bulk insert test: {count} leafs")

    # Fresh DB for load test — never DROP INDEX on a live catalog.
    if DB_PATH.exists():
        DB_PATH.unlink()

    db, conn = connect(str(DB_PATH), read_only=False)
    init_schema(conn)

    model = StaticModel.from_pretrained("minishlab/potion-multilingual-128M")

    t0 = time.time()
    for i in range(count):
        text = f"bulk load test fact {i:03d} running container on host"
        emb = model.encode([text])[0].astype(float).tolist()
        upsert_leaf(
            conn,
            text=text,
            root="facts",
            confidence="confirmed",
            source="load-test",
            source_rev="2dph",
            how="load_test_bulk",
            loc=f"test:{i}",
            type_="fact",
            embedding=emb,
        )
    t1 = time.time()

    # Create indexes once after bulk write (DROP+recreate is unsafe on Ladybug 0.19).
    ensure_indexes(conn)

    conn.close()
    db.close()

    elapsed = t1 - t0
    results["total_seconds"] = elapsed
    results["throughput"] = count / elapsed  # leafs/sec
    results["count"] = count
    return results


def main():
    count_str = sys.argv[1] if len(sys.argv) > 1 else "200"
    count = int(count_str)
    drop_str = sys.argv[2] if len(sys.argv) > 2 else "true"
    drop_indexes = drop_str.lower() in ("1", "true", "yes")

    r = run_bulk_tests(count=count, _drop_indexes=drop_indexes)
    print(json.dumps(r, indent=2))


if __name__ == "__main__":
    main()