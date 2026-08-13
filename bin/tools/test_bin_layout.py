"""D14 layout: bin/{subject}/{method}.go, libs in internal/, one go.mod."""
from __future__ import annotations

import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class BinLayoutTest(unittest.TestCase):
    def test_brain_search_shebang_exists(self) -> None:
        p = ROOT / "bin" / "brain" / "search.go"
        self.assertTrue(p.is_file(), "missing bin/brain/search.go")
        first = p.read_text().splitlines()[0]
        self.assertTrue(
            first.startswith("//usr/bin/env go run"),
            f"shebang first line, got {first!r}",
        )

    def test_no_nested_go_mod_under_bin(self) -> None:
        nested = list((ROOT / "bin").rglob("go.mod"))
        self.assertEqual(nested, [], f"nested go.mod files: {nested}")

    def test_rank_lives_in_internal_brain(self) -> None:
        self.assertTrue(
            (ROOT / "internal" / "brain" / "rank" / "rank.go").is_file(),
            "ranking must live in internal/brain/rank (cgo-free)",
        )
        self.assertFalse(
            (ROOT / "bin" / "kbsearch").exists(),
            "bin/kbsearch nested module must be gone",
        )

    def test_no_main_go_under_bin_brain(self) -> None:
        main = ROOT / "bin" / "brain" / "main.go"
        self.assertFalse(main.exists(), "bin/brain/main.go is not a method")
