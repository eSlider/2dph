"""Tests for the SearXNG client. No network: two recorded responses stand in.

Run: python3 -m unittest discover -s tools -t .
"""
import sys
import json
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import websearch as ws

FIXTURES = Path(__file__).resolve().parent / "fixtures"
HEALTHY = json.loads((FIXTURES / "healthy.json").read_text())
THROTTLED = json.loads((FIXTURES / "throttled.json").read_text())


class Classify(unittest.TestCase):
    """An empty result set is not evidence of absence.

    The instance answers 200 with `results: []` when it throttles us, so calling
    that "no matches" would make an agent conclude something false.
    """

    def test_healthy_response_is_ok(self):
        self.assertEqual(ws.classify(HEALTHY), "ok")

    def test_empty_response_is_throttled_not_empty(self):
        self.assertEqual(ws.classify(THROTTLED), "throttled")

    def test_status_is_never_the_word_empty(self):
        self.assertNotIn(ws.classify(THROTTLED), ("empty", "no_results"))


class Project(unittest.TestCase):
    def test_keeps_only_the_fields_worth_context(self):
        out = ws.project(HEALTHY, limit=3)
        self.assertEqual(out["status"], "ok")
        self.assertEqual(len(out["results"]), 3)
        self.assertEqual(set(out["results"][0]), {"rank", "title", "url", "snippet", "engine"})

    def test_snippet_is_trimmed(self):
        out = ws.project(HEALTHY, limit=5, snippet_chars=40)
        self.assertTrue(all(len(r["snippet"]) <= 43 for r in out["results"]))

    def test_projection_is_far_cheaper_than_the_raw_payload(self):
        raw = len(json.dumps(HEALTHY))
        small = len(json.dumps(ws.project(HEALTHY, limit=5)))
        self.assertLess(small * 3, raw)

    def test_throttled_projection_carries_the_engine_reasons(self):
        out = ws.project(THROTTLED, limit=5)
        self.assertEqual(out["status"], "throttled")
        self.assertEqual(out["results"], [])
        self.assertTrue(out["unresponsive"])


class CacheKey(unittest.TestCase):
    def test_same_question_same_key(self):
        self.assertEqual(ws.cache_key("Pflegegrad", {}), ws.cache_key("Pflegegrad", {}))

    def test_case_and_padding_do_not_matter(self):
        self.assertEqual(ws.cache_key("  Pflegegrad ", {}), ws.cache_key("pflegegrad", {}))

    def test_parameters_change_the_key(self):
        self.assertNotEqual(ws.cache_key("x", {"lang": "de"}), ws.cache_key("x", {}))

    def test_parameter_order_does_not_change_the_key(self):
        self.assertEqual(ws.cache_key("x", {"a": "1", "b": "2"}),
                         ws.cache_key("x", {"b": "2", "a": "1"}))


class PhiGuard(unittest.TestCase):
    """The query leaves this host, so client data must never reach it."""

    def test_plain_technical_query_passes(self):
        self.assertIsNone(ws.phi_reason("Pflegegrad SGB XI Einstufung"))
        self.assertIsNone(ws.phi_reason("site:ticket.detective.de Toureffizienz"))

    def test_long_digit_run_is_refused(self):
        self.assertIsNotNone(ws.phi_reason("Kunde 4711220385 Adresse"))

    def test_insurance_number_is_refused(self):
        self.assertIsNotNone(ws.phi_reason("KV-Nr A123456789"))

    def test_street_with_house_number_is_refused(self):
        self.assertIsNotNone(ws.phi_reason("Hauptstraße 14 Berlin"))
        self.assertIsNotNone(ws.phi_reason("Lindenstr. 7"))

    def test_personalnummer_is_refused(self):
        self.assertIsNotNone(ws.phi_reason("Personalnummer 12"))

    def test_short_numbers_are_fine(self):
        self.assertIsNone(ws.phi_reason("SGB XI Paragraph 45b"))


class Throttle(unittest.TestCase):
    def test_waits_the_remainder_of_the_interval(self):
        self.assertAlmostEqual(ws.wait_for(last=100.0, now=104.0, interval=10.0), 6.0)

    def test_no_wait_once_the_interval_passed(self):
        self.assertEqual(ws.wait_for(last=100.0, now=130.0, interval=10.0), 0.0)

    def test_no_wait_on_a_first_call(self):
        self.assertEqual(ws.wait_for(last=None, now=130.0, interval=10.0), 0.0)


if __name__ == "__main__":
    unittest.main()
