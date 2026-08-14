"""D14 layout: bin/{subject}/{method}.go, libs in internal/, one go.mod."""
from __future__ import annotations

import os
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
        for method in ("index.go", "add.go", "get.go", "stats.go", "eval.go", "watch.go"):
            self._assert_shebang(f"bin/brain/{method}")

    def test_brain_add_is_python_write_not_rebuild(self) -> None:
        self._assert_shebang("bin/brain/add.go")
        text = (ROOT / "bin" / "brain" / "add.go").read_text()
        self.assertIn("cmdbin.ExecFile", text)
        self.assertIn("bin/kb/add", text)
        self.assertNotIn("--rebuild", text)
        py = (ROOT / "bin" / "kb" / "add").read_text()
        self.assertIn("add_leafs", py)
        self.assertIn("--json", py)
        self.assertNotIn("unlink", py.lower())

    def test_brain_get_stats_eval_are_not_python_exec(self) -> None:
        for method in ("get.go", "stats.go", "eval.go"):
            text = (ROOT / "bin" / "brain" / method).read_text()
            self.assertNotIn(
                "ExecFile",
                text,
                f"bin/brain/{method} must call internal/brain, not ExecFile Python",
            )
            self.assertNotIn(
                "cmdbin",
                text,
                f"bin/brain/{method} must not import internal/cmdbin",
            )
            self.assertIn(
                "system_ladybug",
                text.splitlines()[0],
                f"bin/brain/{method} shebang must pass -tags=system_ladybug",
            )
            self.assertIn(
                "github.com/eSlider/2dph/internal/brain",
                text,
            )

    def test_eval_control_questions_live_in_rank(self) -> None:
        rank = (ROOT / "internal" / "brain" / "rank" / "evalq.go").read_text()
        py = (ROOT / "bin" / "kb" / "eval").read_text()
        for frag in ("BM25", "DevOps", "LadybugDB"):
            self.assertIn(frag, rank)
            self.assertIn(frag, py)
        self.assertIn("0.95", rank)

    def test_facts_methods_are_shebangs(self) -> None:
        for method in ("audit.go", "extract.go", "crm.go"):
            self._assert_shebang(f"bin/facts/{method}")
            text = (ROOT / "bin" / "facts" / method).read_text()
            self.assertIn("cmdbin.ExecFile", text)
            self.assertIn(f"bin/facts/{method.removesuffix('.go')}", text)

    def test_mail_import_is_shebang_not_brain_write(self) -> None:
        self._assert_shebang("bin/mail/import.go")
        index_mail = (ROOT / "bin" / "mail" / "index_mail").read_text()
        self.assertIn(
            "bin/brain/index.go",
            index_mail,
            "index_mail must point at bin/brain/index.go",
        )

    def test_markdown_import_is_go_not_python_exec(self) -> None:
        self._assert_shebang("bin/markdown/import.go")
        text = (ROOT / "bin" / "markdown" / "import.go").read_text()
        self.assertNotIn("ExecFile", text)
        self.assertNotIn("cmdbin", text)
        self.assertIn("internal/mdleaves", text)
        self.assertNotIn("kb.lbug", text)

    def test_import_adapters_do_not_write_ladybug(self) -> None:
        for rel in (
            "bin/mail/import.go",
            "bin/mail/import",
            "bin/markdown/import.go",
            "bin/chats/import.go",
            "bin/git/import.go",
        ):
            text = (ROOT / rel).read_text()
            self.assertNotIn("upsert_leaf", text, rel)
            self.assertNotIn("kb.lbug", text, rel)
            self.assertNotIn("var/brain.lbug", text, rel)
        index = (ROOT / "bin" / "brain" / "index.go").read_text()
        self.assertIn("bin/kb/index", index)

    def test_postgres_query_is_shebang(self) -> None:
        self._assert_shebang("bin/postgres/query.go")

    def test_git_import_is_gogit_shebang(self) -> None:
        self._assert_shebang("bin/git/import.go")
        py = (ROOT / "bin" / "git" / "import").read_text()
        self.assertNotIn(
            '["git"',
            py,
            "Python git/import must not subprocess the git binary",
        )
        self.assertIn("bin/git/import.go", py)

    def test_web_search_is_shebang(self) -> None:
        self._assert_shebang("bin/web/search.go")
        py = (ROOT / "bin" / "web" / "search").read_text()
        self.assertIn("bin/web/search.go", py)

    def test_gitimport_py_has_no_git_binary(self) -> None:
        py = (ROOT / "bin" / "tools" / "gitimport.py").read_text()
        self.assertNotIn("subprocess", py)
        self.assertNotIn("git log", py)

    def test_gogit_is_direct_go_mod_require(self) -> None:
        text = (ROOT / "go.mod").read_text()
        first = text.split("require (")[1].split(")")[0]
        self.assertRegex(first, r"github.com/go-git/go-git/v5\s+v")
        for line in first.splitlines():
            if "go-git/go-git" in line:
                self.assertNotIn("indirect", line)

    def test_cgo_uses_zig_not_gcc(self) -> None:
        for rel in ("bin/cgo/zig", "bin/cgo/zcc", "bin/cgo/zc++"):
            p = ROOT / rel
            self.assertTrue(p.is_file(), f"missing {rel}")
            self.assertTrue(
                os.access(p, os.X_OK),
                f"{rel} must be executable",
            )
        zig = (ROOT / "bin" / "cgo" / "zig").read_text()
        self.assertIn("zig cc", zig)
        self.assertIn("0.14.1", zig)
        zcc = (ROOT / "bin" / "cgo" / "zcc").read_text()
        self.assertIn('exec "$ZIG" cc', zcc)
        self.assertNotIn("command -v gcc", zcc)
        search = (ROOT / "bin" / "kb" / "search").read_text()
        self.assertIn("bin/cgo/zig", search)
        self.assertNotIn("command -v gcc", search)

    def test_ci_recall_sot_is_zig_brain_eval(self) -> None:
        ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text()
        self.assertIn("bin/brain/eval.go", ci)
        self.assertIn("system_ladybug,brain_eval", ci)
        self.assertIn("/tmp/brain-eval", ci)
        self.assertIn("KB_ROOT", ci)
        self.assertNotIn("bin/kb/eval", ci)
        self.assertNotIn("gate skipped", ci)
        self.assertIn("./bin/facts/audit self", ci)

    def test_eval_fragments_live_in_default_corpus(self) -> None:
        """CI --rebuild indexes README/PLAN/docs/skills; fragments must be there."""
        corpus = []
        for rel in ("README.md", "PLAN.md", "AGENTS.md"):
            corpus.append((ROOT / rel).read_text())
        for d in ("docs", "skills"):
            for p in (ROOT / d).rglob("*.md"):
                corpus.append(p.read_text())
        blob = "\n".join(corpus)
        for frag in ("BM25", "DevOps", "LadybugDB"):
            self.assertIn(frag, blob, f"{frag} must appear in default index corpus")
