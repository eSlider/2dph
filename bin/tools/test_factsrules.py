import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import factsrules  # noqa: E402
from factsrules import Source  # noqa: E402


def runtime(name="chat"):
    return Source(kind="runtime", method="docker ps", locator=f"docker ps:{name}",
                  origin="docker-daemon")


def declared(path="/srv/chat/compose.yaml", name="chat"):
    return Source(kind="declared", method=f"compose:{Path(path).name}",
                  locator=f"{path}:{name}", origin=f"file:{path}")


class SourceTest(unittest.TestCase):
    def test_kind_must_come_from_the_taxonomy(self):
        with self.assertRaises(ValueError):
            factsrules.make_fact("x", [runtime(), Source("vibes", "gut feeling", "n/a", "me")], "loc")

    def test_every_taxonomy_kind_is_documented(self):
        for kind in factsrules.KINDS:
            self.assertTrue(factsrules.KINDS[kind], f"{kind} has no description")


class IndependenceTest(unittest.TestCase):
    def test_two_independent_sources_are_accepted(self):
        fact = factsrules.make_fact("chat runs", [runtime(), declared()], "loc")
        self.assertEqual(len(fact.sources), 2)

    def test_single_source_is_rejected(self):
        with self.assertRaises(ValueError):
            factsrules.make_fact("chat runs", [runtime()], "loc")

    def test_the_same_source_twice_is_not_two_sources(self):
        with self.assertRaises(ValueError):
            factsrules.make_fact("chat runs", [runtime(), runtime()], "loc")

    # The point of the whole exercise: two compose files are two strings but
    # one method and one kind of claim. That is corroboration, not evidence.
    def test_two_sources_of_the_same_kind_are_not_independent(self):
        with self.assertRaises(ValueError) as ctx:
            factsrules.make_fact("chat runs", [
                declared("/srv/a/compose.yaml"),
                declared("/srv/b/compose.yaml"),
            ], "loc")
        self.assertIn("kind", str(ctx.exception))

    def test_two_sources_from_the_same_origin_are_not_independent(self):
        # Same file, read two ways: still one system of record.
        with self.assertRaises(ValueError) as ctx:
            factsrules.make_fact("chat runs", [
                Source("declared", "compose", "/srv/a/compose.yaml:chat", "file:/srv/a/compose.yaml"),
                Source("doc", "readme", "/srv/a/compose.yaml:1", "file:/srv/a/compose.yaml"),
            ], "loc")
        self.assertIn("origin", str(ctx.exception))

    def test_locator_is_required(self):
        # Evidence you cannot go back and look at is not evidence.
        with self.assertRaises(ValueError):
            factsrules.make_fact("chat runs", [
                Source("runtime", "docker ps", "", "docker-daemon"),
                declared(),
            ])


class RenderingTest(unittest.TestCase):
    def test_source_string_keeps_the_x_separator(self):
        fact = factsrules.make_fact("chat runs", [runtime(), declared()], "loc")
        self.assertEqual(fact.source, "docker ps x compose:compose.yaml")

    def test_loc_defaults_to_the_locators(self):
        fact = factsrules.make_fact("chat runs", [runtime(), declared()])
        self.assertIn("docker ps:chat", fact.loc)
        self.assertIn("/srv/chat/compose.yaml:chat", fact.loc)

    def test_evidence_carries_the_structure_into_the_db(self):
        fact = factsrules.make_fact("chat runs", [runtime(), declared()])
        evidence = fact.evidence()
        self.assertEqual(len(evidence), 2)
        self.assertEqual({e["kind"] for e in evidence}, {"runtime", "declared"})
        for item in evidence:
            self.assertTrue(item["id"])
            self.assertTrue(item["locator"])
            self.assertTrue(item["origin"])


class PairingTest(unittest.TestCase):
    def test_container_is_paired_with_its_compose_file(self):
        facts = factsrules.pair_container_compose(
            {"chat": {"/srv/chat/compose.yaml": ["chat", "db"]}}
        )
        self.assertEqual(len(facts), 1)
        self.assertEqual({s.kind for s in facts[0].sources}, {"runtime", "declared"})

    def test_running_container_without_any_second_source_stays_out(self):
        facts = factsrules.pair_all(
            running=["onlyoffice"],
            compose_by_container={"onlyoffice": {"/srv/chat/compose.yaml": ["chat"]}},
            repo_services_by_file={},
            ssh_hosts=["arc-2"],
            doc_hits={"arc-2": ["AGENTS.md:74"]},
        )
        self.assertNotIn("onlyoffice", " ".join(f.text for f in facts))

    def test_host_needs_a_doc_mention_with_a_locator(self):
        paired = factsrules.pair_host_docs(["arc-2"], {"arc-2": ["README.md:12"]})
        self.assertEqual(len(paired), 1)
        self.assertEqual({s.kind for s in paired[0].sources}, {"netconfig", "doc"})
        self.assertIn("README.md:12", paired[0].loc)
        self.assertEqual(factsrules.pair_host_docs(["arc-2"], {}), [])

    def test_container_matching_a_configured_host(self):
        facts = factsrules.pair_container_host(["arc-2"], ["arc-2", "other"])
        self.assertEqual(len(facts), 1)
        self.assertEqual({s.kind for s in facts[0].sources}, {"runtime", "netconfig"})

    def test_no_ssh_hosts_means_no_host_facts(self):
        self.assertEqual(factsrules.pair_container_host(["arc-2"], []), [])

    def test_pair_all_dedupes_by_text(self):
        facts = factsrules.pair_all(
            running=["chat"],
            compose_by_container={"chat": {"/srv/chat/compose.yaml": ["chat"]}},
            repo_services_by_file={"compose.yaml": ["chat"]},
            ssh_hosts=[],
            doc_hits={},
        )
        texts = [f.text for f in facts]
        self.assertEqual(len(texts), len(set(texts)))

    def test_every_produced_fact_is_independently_sourced(self):
        facts = factsrules.pair_all(
            running=["chat", "arc-2", "lonely"],
            compose_by_container={"chat": {"/srv/chat/compose.yaml": ["chat"]}},
            repo_services_by_file={"compose.yaml": ["chat"]},
            ssh_hosts=["arc-2"],
            doc_hits={"arc-2": ["AGENTS.md:74"]},
        )
        self.assertTrue(facts)
        for fact in facts:
            self.assertGreaterEqual(len({s.kind for s in fact.sources}), factsrules.MIN_SOURCES, fact.text)
            self.assertGreaterEqual(len({s.origin for s in fact.sources}), factsrules.MIN_SOURCES, fact.text)


if __name__ == "__main__":
    unittest.main()
