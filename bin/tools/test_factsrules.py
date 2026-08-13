import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import factsrules  # noqa: E402


class MakeFactTest(unittest.TestCase):
    def test_two_independent_sources_are_accepted(self):
        fact = factsrules.make_fact("x runs", ["docker ps", "compose:a.yaml"], "a.yaml:x")
        self.assertEqual(fact.sources, ("docker ps", "compose:a.yaml"))

    def test_single_source_is_rejected(self):
        with self.assertRaises(ValueError):
            factsrules.make_fact("x runs", ["docker ps"], "docker ps:x")

    def test_the_same_source_twice_is_not_two_sources(self):
        with self.assertRaises(ValueError):
            factsrules.make_fact("x runs", ["docker ps", "docker ps"], "docker ps:x")

    def test_source_string_keeps_the_x_separator(self):
        # facts/audit db asserts " x " is present in Leaf.source.
        fact = factsrules.make_fact("x runs", ["docker ps", "ssh config"], "loc")
        self.assertEqual(fact.source, "docker ps x ssh config")
        self.assertEqual(fact.as_dict()["source"], "docker ps x ssh config")


class PairingTest(unittest.TestCase):
    def test_container_is_paired_with_its_compose_file(self):
        facts = factsrules.pair_container_compose(
            {"chat": {"/srv/chat/compose.yaml": ["chat", "db"]}}
        )
        self.assertEqual(len(facts), 1)
        self.assertIn("declared in compose.yaml", facts[0].text)
        self.assertEqual(facts[0].sources, ("docker ps", "compose:compose.yaml"))

    def test_running_container_without_any_second_source_stays_out(self):
        facts = factsrules.pair_all(
            running=["onlyoffice"],
            compose_by_container={"onlyoffice": {"/srv/chat/compose.yaml": ["chat"]}},
            repo_services=[],
            repo_compose_name="compose.yaml",
            ssh_hosts=["arc-2"],
            doc_terms={"arc-2"},
        )
        self.assertNotIn("onlyoffice", " ".join(f.text for f in facts))

    def test_host_needs_a_doc_mention(self):
        paired = factsrules.pair_host_docs(["arc-2"], {"arc-2"}, ["README.md"])
        self.assertEqual(len(paired), 1)
        self.assertEqual(factsrules.pair_host_docs(["arc-2"], set(), ["README.md"]), [])

    def test_container_matching_a_configured_host(self):
        facts = factsrules.pair_container_host(["arc-2"], ["arc-2", "other"])
        self.assertEqual(len(facts), 1)
        self.assertEqual(facts[0].sources, ("docker ps", "ssh config"))

    def test_no_ssh_hosts_means_no_host_facts(self):
        self.assertEqual(factsrules.pair_container_host(["arc-2"], []), [])

    def test_pair_all_dedupes_by_text(self):
        facts = factsrules.pair_all(
            running=["chat"],
            compose_by_container={"chat": {"/srv/chat/compose.yaml": ["chat"]}},
            repo_services=["chat"],
            repo_compose_name="compose.yaml",
            ssh_hosts=[],
            doc_terms=set(),
        )
        texts = [f.text for f in facts]
        self.assertEqual(len(texts), len(set(texts)))

    def test_every_produced_fact_carries_at_least_two_sources(self):
        facts = factsrules.pair_all(
            running=["chat", "arc-2", "lonely"],
            compose_by_container={"chat": {"/srv/chat/compose.yaml": ["chat"]}},
            repo_services=["chat"],
            repo_compose_name="compose.yaml",
            ssh_hosts=["arc-2"],
            doc_terms={"arc-2"},
        )
        self.assertTrue(facts)
        for fact in facts:
            self.assertGreaterEqual(len(set(fact.sources)), factsrules.MIN_SOURCES, fact.text)


if __name__ == "__main__":
    unittest.main()
