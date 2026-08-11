import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import kblib  # noqa: E402


def make_emb(value: float) -> list[float]:
    vec = [0.0] * kblib.EMBED_DIM
    vec[0] = value
    return vec


class KblibTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.dbpath = os.path.join(self.dir, "kb.lbug")
        self.db, self.conn = kblib.connect(self.dbpath, read_only=False)
        kblib.init_schema(self.conn)

    def tearDown(self):
        self.conn.close()
        self.db.close()

    def test_leaf_id_is_stable(self):
        self.assertEqual(kblib.leaf_id("abc", "src"), kblib.leaf_id("abc", "src"))
        self.assertNotEqual(kblib.leaf_id("abc", "src"), kblib.leaf_id("abd", "src"))

    def test_upsert_roundtrip(self):
        kblib.upsert_leaf(self.conn, text="the quick brown fox", root="info",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(1.0))
        kblib.create_fts_and_vector(self.conn, force=True)
        hits = kblib.query_fts(self.conn, "fox", 5)
        self.assertEqual(len(hits), 1)
        self.assertEqual(hits[0]["root"], "info")

    def test_hybrid_ranks_vector_match(self):
        kblib.upsert_leaf(self.conn, text="the quick brown fox", root="info",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(1.0))
        kblib.upsert_leaf(self.conn, text="a lazy dog sleeps", root="info",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(0.0))
        kblib.create_fts_and_vector(self.conn, force=True)
        result = kblib.hybrid_search(self.conn, make_emb(1.0), [], 5)
        self.assertTrue(result)
        self.assertIn("rrf", result[0])
        self.assertEqual(result[0]["text"], "the quick brown fox")

    def test_stats_counts_roots(self):
        kblib.upsert_leaf(self.conn, text="a fact leaf", root="facts",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(0.5))
        kblib.upsert_leaf(self.conn, text="an info leaf", root="info",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(0.5))
        stats = kblib.stats(self.conn)
        self.assertEqual(stats["total"], 2)
        self.assertEqual(stats["by_root"], {"facts": 1, "info": 1})


if __name__ == "__main__":
    unittest.main()