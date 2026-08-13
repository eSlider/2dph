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

    def test_chats_methods_are_shebangs_not_main(self) -> None:
        chats = ROOT / "bin" / "chats"
        self.assertFalse(
            (chats / "main.go").exists(),
            "bin/chats/main.go is a dispatcher, not a method",
        )
        self.assertFalse(
            (chats / "index_cmd.go").exists(),
            "chats index is a brain write hiding under the wrong subject",
        )
        for method in ("sync.go", "import.go", "facts.go", "apply.go"):
            p = chats / method
            self.assertTrue(p.is_file(), f"missing bin/chats/{method}")
            first = p.read_text().splitlines()[0]
            self.assertTrue(
                first.startswith("//usr/bin/env go run"),
                f"{method} shebang, got {first!r}",
            )

    def test_chats_lib_lives_in_internal(self) -> None:
        self.assertTrue(
            (ROOT / "internal" / "chats" / "linkedin.go").is_file(),
            "LinkedIn parser must live in internal/chats",
        )
        self.assertFalse(
            (ROOT / "bin" / "chats" / "linkedin.go").exists(),
            "parser must not stay under bin/chats as a second main",
        )

    def _assert_shebang(self, rel: str) -> None:
        p = ROOT / rel
        self.assertTrue(p.is_file(), f"missing {rel}")
        first = p.read_text().splitlines()[0]
        self.assertTrue(
            first.startswith("//usr/bin/env go run"),
            f"{rel} shebang, got {first!r}",
        )

    def test_brain_methods_are_shebangs(self) -> None:
        for method in ("index.go", "get.go", "stats.go", "eval.go", "watch.go"):
            self._assert_shebang(f"bin/brain/{method}")

    def test_mail_import_is_shebang_not_brain_write(self) -> None:
        self._assert_shebang("bin/mail/import.go")
        index_mail = (ROOT / "bin" / "mail" / "index_mail").read_text()
        self.assertIn(
            "bin/brain/index.go",
            index_mail,
            "index_mail must point at bin/brain/index.go",
        )

    def test_markdown_import_is_shebang(self) -> None:
        self._assert_shebang("bin/markdown/import.go")

    def test_postgres_query_is_shebang(self) -> None:
        self._assert_shebang("bin/postgres/query.go")
