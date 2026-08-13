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

    def add_fact(self, text, evidence):
        return kblib.upsert_leaf(
            self.conn, text=text, root="facts", confidence="confirmed",
            source="rendered", source_rev="r1", how="facts/extract", loc="loc",
            type_="fact", embedding=make_emb(0.5), evidence=evidence,
        )

    def test_evidence_is_stored_as_nodes_and_linked_to_the_leaf(self):
        lid = self.add_fact("chat runs", [
            {"id": "e1", "kind": "runtime", "method": "docker ps",
             "locator": "docker ps:chat", "origin": "docker-daemon"},
            {"id": "e2", "kind": "declared", "method": "compose",
             "locator": "/a.yaml:chat", "origin": "file:/a.yaml"},
        ])
        stored = kblib.evidence_for(self.conn, lid)
        self.assertEqual({e["kind"] for e in stored}, {"runtime", "declared"})
        self.assertEqual({e["origin"] for e in stored}, {"docker-daemon", "file:/a.yaml"})
        self.assertTrue(all(e["locator"] for e in stored))

    def test_evidence_is_shared_between_leafs_not_duplicated(self):
        shared = {"id": "e1", "kind": "runtime", "method": "docker ps",
                  "locator": "docker ps:chat", "origin": "docker-daemon"}
        other = {"id": "e2", "kind": "declared", "method": "compose",
                 "locator": "/a.yaml:chat", "origin": "file:/a.yaml"}
        third = {"id": "e3", "kind": "doc", "method": "readme",
                 "locator": "README.md:1", "origin": "file:README.md"}
        self.add_fact("chat runs", [shared, other])
        self.add_fact("chat is documented", [shared, third])
        total = self.conn.execute("MATCH (e:Evidence) RETURN count(*)").get_all()[0][0]
        self.assertEqual(total, 3)

    def test_audit_query_flags_a_fact_with_one_kind_of_evidence(self):
        weak = self.add_fact("two compose files agree", [
            {"id": "c1", "kind": "declared", "method": "compose",
             "locator": "/a.yaml:chat", "origin": "file:/a.yaml"},
            {"id": "c2", "kind": "declared", "method": "compose",
             "locator": "/b.yaml:chat", "origin": "file:/b.yaml"},
        ])
        strong = self.add_fact("chat runs", [
            {"id": "e1", "kind": "runtime", "method": "docker ps",
             "locator": "docker ps:chat", "origin": "docker-daemon"},
            {"id": "e2", "kind": "declared", "method": "compose",
             "locator": "/a.yaml:chat", "origin": "file:/a.yaml"},
        ])
        flagged = {row["id"]: row for row in kblib.facts_lacking_independence(self.conn)}
        self.assertIn(weak, flagged)
        self.assertNotIn(strong, flagged)
        self.assertEqual(flagged[weak]["kinds"], ["declared"])

    def test_audit_query_flags_a_fact_with_no_evidence_at_all(self):
        bare = kblib.upsert_leaf(
            self.conn, text="trust me", root="facts", confidence="confirmed",
            source="bullshit x bullshit2", source_rev="r1", how="h", loc="l",
            type_="fact", embedding=make_emb(0.5),
        )
        flagged = {row["id"] for row in kblib.facts_lacking_independence(self.conn)}
        self.assertIn(bare, flagged)

    def test_info_leafs_are_not_subject_to_the_evidence_rule(self):
        kblib.upsert_leaf(self.conn, text="just a note", root="info",
                          confidence="confirmed", source="s", source_rev="r1",
                          how="test", loc="/tmp", type_="reference",
                          embedding=make_emb(0.5))
        self.assertEqual(kblib.facts_lacking_independence(self.conn), [])

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