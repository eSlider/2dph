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
        kblib.ensure_indexes(self.conn)
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
        kblib.ensure_indexes(self.conn)
        result = kblib.hybrid_search(self.conn, make_emb(1.0), [], 5)
        self.assertTrue(result)
        self.assertIn("rrf", result[0])
        self.assertEqual(result[0]["text"], "the quick brown fox")

    def test_upsert_keeps_hnsw_queryable(self):
        """Upsert while HNSW exists must not kill vector search."""
        kblib.upsert_leaf(self.conn, text="seed leaf", root="info",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(0.2))
        kblib.ensure_indexes(self.conn)
        self.assertIn("Leaf_vec", kblib.leaf_index_names(self.conn))
        kblib.upsert_leaf(self.conn, text="added after index", root="facts",
                          confidence="confirmed", source="a.md x b.md",
                          source_rev="r1", how="test", loc="/tmp", type_="fact",
                          embedding=make_emb(0.9))
        hits = kblib.query_vector(self.conn, make_emb(0.9), 5)
        self.assertTrue(hits)
        self.assertIn("Leaf_vec", kblib.leaf_index_names(self.conn))

    def test_add_after_indexes_keeps_fts_queryable(self):
        """Incremental add after FTS+HNSW must find the new leaf on both indexes."""
        kblib.upsert_leaf(self.conn, text="seed fox leaf", root="info",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(0.1))
        kblib.ensure_indexes(self.conn)
        ids = kblib.add_leafs(self.conn, [{
            "text": "added zebra after index",
            "root": "facts",
            "confidence": "confirmed",
            "source": "a.md x b.md",
            "source_rev": "r1",
            "how": "test",
            "loc": "/tmp",
            "type": "fact",
            "embedding": make_emb(0.9),
        }])
        self.assertEqual(len(ids), 1)
        fts = kblib.query_fts(self.conn, "zebra", 5)
        self.assertTrue(fts)
        self.assertIn("zebra", fts[0]["text"])
        self.assertEqual(fts[0]["root"], "facts")
        vec = kblib.query_vector(self.conn, make_emb(0.9), 5)
        self.assertTrue(any("zebra" in h["text"] for h in vec))
        fox = kblib.query_fts(self.conn, "fox", 5)
        self.assertTrue(fox)
        self.assertIn("fox", fox[0]["text"])

    def test_add_facts_and_info_one_transaction(self):
        """D12: facts and info land in the same transaction."""
        kblib.ensure_indexes(self.conn)
        ids = kblib.add_leafs(self.conn, [
            {
                "text": "tx fact leaf two-source",
                "root": "facts",
                "confidence": "confirmed",
                "source": "compose.yml x docker ps",
                "source_rev": "r1",
                "how": "test",
                "loc": "/tmp",
                "type": "fact",
                "embedding": make_emb(0.4),
            },
            {
                "text": "tx info narrative",
                "root": "info",
                "confidence": "confirmed",
                "source": "note.md",
                "source_rev": "r1",
                "how": "test",
                "loc": "/tmp",
                "type": "reference",
                "embedding": make_emb(0.5),
            },
        ])
        self.assertEqual(len(ids), 2)
        stats = kblib.stats(self.conn)
        self.assertEqual(stats["by_root"].get("facts"), 1)
        self.assertEqual(stats["by_root"].get("info"), 1)
        self.assertTrue(kblib.query_fts(self.conn, "two-source", 5))
        self.assertTrue(kblib.query_fts(self.conn, "narrative", 5))

    def test_drop_vector_then_create_raises_clear_error(self):
        """DROP INDEX leaves ghost catalog; create_fts_and_vector must raise."""
        kblib.upsert_leaf(self.conn, text="seed", root="info",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(0.1))
        kblib.ensure_indexes(self.conn)
        self.conn.execute("DROP INDEX IF EXISTS Leaf.Leaf_vec")
        with self.assertRaises(RuntimeError) as ctx:
            kblib.create_fts_and_vector(self.conn, force=True)
        msg = str(ctx.exception)
        self.assertIn("CREATE_VECTOR_INDEX failed", msg)
        self.assertIn("--rebuild", msg)

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

    def test_as_of_keeps_x_drops_y(self) -> None:
        """OQ5/#36: as of 2025-01-01 → works-at-X, not works-at-Y."""
        kblib.upsert_leaf(
            self.conn, text="Andrey works at X", root="facts",
            confidence="confirmed", source="crm.md x contract.md",
            source_rev="r1", how="test", loc="/tmp", type_="fact",
            embedding=make_emb(0.5),
            valid_from="2024-03-01", valid_to="2025-07-15",
        )
        kblib.upsert_leaf(
            self.conn, text="Andrey works at Y", root="facts",
            confidence="confirmed", source="offer.md x payroll.md",
            source_rev="r1", how="test", loc="/tmp", type_="fact",
            embedding=make_emb(0.6),
            valid_from="2025-07-16", valid_to="",
        )
        kblib.ensure_indexes(self.conn)
        hits = kblib.query_fts(self.conn, "Andrey works", 10)
        kept = kblib.filter_as_of(hits, "2025-01-01")
        texts = [h["text"] for h in kept]
        self.assertTrue(any("works at X" in t for t in texts), texts)
        self.assertFalse(any("works at Y" in t for t in texts), texts)
        later = kblib.filter_as_of(hits, "2025-08-01")
        later_texts = [h["text"] for h in later]
        self.assertTrue(any("works at Y" in t for t in later_texts), later_texts)
        self.assertFalse(any("works at X" in t for t in later_texts), later_texts)

    def test_active_at_pure(self) -> None:
        self.assertTrue(kblib.active_at("2024-03-01", "2025-07-15", "2025-01-01"))
        self.assertFalse(kblib.active_at("2025-07-16", "", "2025-01-01"))
        self.assertTrue(kblib.active_at("", "", "2025-01-01"))


if __name__ == "__main__":
    unittest.main()
