import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))

from contradict import (  # noqa: E402
    RULE_AUTHORITY,
    RULE_SINGLE,
    RULE_TEMPORAL,
    RULE_TWO_SOURCE,
    RULE_UNRESOLVED,
    adjudicate,
    check_fact_row,
    parse_source_field,
)


def src(i, kind, stale=False):
    return {"id": i, "kind": kind, "stale": stale}


class TestContradict(unittest.TestCase):
    def test_two_vs_two_stays_hypothesis(self):
        r = adjudicate({
            "text": "svc listens on 443",
            "yes": [src("docker-ps", "runtime"), src("compose", "config")],
            "no": [src("docker-old", "runtime"), src("compose-old", "config")],
        })
        self.assertFalse(r["confirmed"])
        self.assertEqual(r["rule"], RULE_UNRESOLVED)
        self.assertEqual(r["winner"], "")

    def test_temporal_freshness(self):
        r = adjudicate({
            "text": "svc listens on 443",
            "yes": [src("docker-ps", "runtime"), src("compose", "config")],
            "no": [src("old-readme", "narrative", True), src("old-wiki", "narrative", True)],
        })
        self.assertTrue(r["confirmed"])
        self.assertEqual(r["rule"], RULE_TEMPORAL)
        self.assertEqual(r["winner"], "yes")

    def test_authority_pairing(self):
        r = adjudicate({
            "text": "svc listens on 443",
            "yes": [src("docker-ps", "runtime"), src("compose", "config")],
            "no": [src("readme", "narrative"), src("wiki", "narrative")],
        })
        self.assertTrue(r["confirmed"])
        self.assertEqual(r["rule"], RULE_AUTHORITY)
        self.assertEqual(r["winner"], "yes")

    def test_two_source_and_single(self):
        two = adjudicate({
            "text": "arc-1 runs Matrix",
            "yes": [src("compose", "config"), src("docker-ps", "runtime")],
        })
        self.assertTrue(two["confirmed"])
        self.assertEqual(two["rule"], RULE_TWO_SOURCE)
        one = adjudicate({"text": "maybe", "yes": [src("readme", "narrative")]})
        self.assertFalse(one["confirmed"])
        self.assertEqual(one["rule"], RULE_SINGLE)

    def test_parse_source_field(self):
        yes, no = parse_source_field("docker ps x compose.yml vs old.md x wiki.md")
        self.assertIn(" x ", yes)
        self.assertIn(" x ", no)

    def test_check_fact_row_allows_hypothesis_vs(self):
        p = check_fact_row(
            "L1", "a.md x b.md vs c.md x d.md", "var/", "audit", "hypothesis",
        )
        self.assertEqual(p, [])
        p = check_fact_row("L2", "a.md x b.md", "var/", "audit", "confirmed")
        self.assertEqual(p, [])
        p = check_fact_row("L3", "a.md x b.md vs c.md x d.md", "var/", "audit", "confirmed")
        self.assertTrue(any("vs-contradiction" in x for x in p))
        p = check_fact_row("L4", "only-one.md", "var/", "audit", "hypothesis")
        self.assertTrue(any("a x b vs" in x for x in p))

    def test_audit_contradict_cli_unresolved(self):
        import json
        import subprocess
        from pathlib import Path
        root = Path(__file__).resolve().parents[2]
        payload = json.dumps({
            "text": "svc 443",
            "yes": [src("a", "runtime"), src("b", "config")],
            "no": [src("c", "runtime"), src("d", "config")],
        })
        proc = subprocess.run(
            [sys.executable, str(root / "bin" / "facts" / "audit"), "contradict", "--json"],
            input=payload, capture_output=True, text=True, check=False,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        out = json.loads(proc.stdout)
        self.assertTrue(out["ok"])
        self.assertEqual(out["contradictions"][0]["rule"], RULE_UNRESOLVED)
        self.assertFalse(out["contradictions"][0]["confirmed"])


if __name__ == "__main__":
    unittest.main()
