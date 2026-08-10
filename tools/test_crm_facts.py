import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import crmfacts  # noqa: E402

FM = """\
schema: 2
meta:
  title: x
orgs:
- id: produktor
  label: ProProdukt SL / produktor.io
  kind: own
  period: 2006–present
  website: https://produktor.io
- id: dyvenia
  label: Dyvenia
  kind: employer
  period: 2023–2025
clients:
- name: One
- name: Two
timeline:
- start: 2001
"""


class CorpusOrgsTest(unittest.TestCase):
    def test_parses_label_kind_period(self):
        orgs = crmfacts.corpus_orgs(FM)
        self.assertEqual(orgs["produktor"]["label"], "ProProdukt SL / produktor.io")
        self.assertEqual(orgs["produktor"]["kind"], "own")
        self.assertEqual(orgs["dyvenia"]["kind"], "employer")

    def test_does_not_leak_clients_into_orgs(self):
        orgs = crmfacts.corpus_orgs(FM)
        self.assertNotIn("One", orgs)
        self.assertNotIn("Two", orgs)
        self.assertNotIn("timeline", orgs)


if __name__ == "__main__":
    unittest.main()