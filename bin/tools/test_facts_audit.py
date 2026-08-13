"""Tests for bin/facts/audit self.

A gate is only worth having if it can go red, so every check here is exercised
against a fixture tree that violates it *and* one that satisfies it.
"""
import importlib.machinery
import importlib.util
import os
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO / "bin" / "tools"))


def load_audit():
    """bin/facts/audit has no .py suffix; load it by path."""
    loader = importlib.machinery.SourceFileLoader("facts_audit", str(REPO / "bin" / "facts" / "audit"))
    spec = importlib.util.spec_from_loader(loader.name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


audit = load_audit()


def fence(body: str) -> str:
    """Mode claims only count inside code, so fixtures write fenced blocks."""
    return f"```bash\n{body}\n```\n" if body else ""


def write_tool(path: Path, body: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body)
    path.chmod(0o755)


class ToolConventionTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name)
        self.original_root = audit.ROOT
        audit.ROOT = self.tmp

    def tearDown(self):
        audit.ROOT = self.original_root

    def test_tool_without_usage_line_is_flagged(self):
        write_tool(self.tmp / "bin" / "kb" / "index", "#!/usr/bin/env python3\nimport sys\n")
        problems = audit.check_tool_convention()
        self.assertEqual(len(problems), 1)
        self.assertIn("kb/index", problems[0])

    def test_tool_without_shebang_is_flagged(self):
        write_tool(self.tmp / "bin" / "kb" / "index", '"""kb/index - build."""\nimport sys\n')
        self.assertTrue(any("shebang" in p for p in audit.check_tool_convention()))

    def test_well_formed_tool_passes(self):
        write_tool(self.tmp / "bin" / "kb" / "index",
                   '#!/usr/bin/env python3\n"""kb/index - build the brain."""\n')
        self.assertEqual(audit.check_tool_convention(), [])

    def test_go_shebang_form_passes(self):
        write_tool(self.tmp / "bin" / "serve.go",
                   '//usr/bin/env go run "$0" "$@"; exit\n// serve - http server\n')
        self.assertEqual(audit.check_tool_convention(), [])

    def test_vendored_libs_are_not_tools(self):
        write_tool(self.tmp / "bin" / "tools" / "kblib.py", "#!/usr/bin/env python3\nimport os\n")
        self.assertEqual(audit.check_tool_convention(), [])

    def test_non_executable_files_are_ignored(self):
        path = self.tmp / "bin" / "kb" / "notes.md"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("just notes\n")
        os.chmod(path, 0o644)
        self.assertEqual(audit.check_tool_convention(), [])


class DocumentedModesTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name)
        self.original_root = audit.ROOT
        audit.ROOT = self.tmp

    def tearDown(self):
        audit.ROOT = self.original_root

    def write_docs(self, agents: str, plan: str = "", readme: str = "", design: str = "") -> None:
        (self.tmp / "AGENTS.md").write_text(fence(agents))
        (self.tmp / "PLAN.md").write_text(fence(plan))
        (self.tmp / "README.md").write_text(fence(readme))
        (self.tmp / "docs").mkdir(exist_ok=True)
        (self.tmp / "docs" / "design.md").write_text(fence(design))

    def test_documented_but_missing_mode_is_flagged(self):
        self.write_docs('bin/facts/audit ["self"|"db"|"stale"]')
        problems = audit.check_documented_modes()
        self.assertTrue(any("do not exist" in p and "stale" in p for p in problems))

    def test_real_mode_missing_from_docs_is_flagged(self):
        self.write_docs('bin/facts/audit ["self"]')
        self.assertTrue(any("omits real audit modes" in p for p in audit.check_documented_modes()))

    def test_matching_docs_pass(self):
        self.write_docs('bin/facts/audit ["self"|"db"]', 'bin/facts/audit ["self"|"db"]')
        self.assertEqual(audit.check_documented_modes(), [])

    def test_invocation_of_a_missing_mode_in_any_doc_is_flagged(self):
        self.write_docs("", "", "", "bin/facts/audit stale")
        problems = audit.check_documented_modes()
        self.assertTrue(any("docs/design.md" in p and "stale" in p for p in problems), problems)

    def test_invocation_of_a_real_mode_passes(self):
        self.write_docs("", "", "bin/facts/audit self", "bin/facts/audit db")
        self.assertEqual(audit.check_documented_modes(), [])

    def test_prose_mentioning_the_tool_is_not_a_mode_claim(self):
        (self.tmp / "AGENTS.md").write_text("Run bin/facts/audit before pushing.\n")
        (self.tmp / "PLAN.md").write_text("")
        (self.tmp / "README.md").write_text("")
        self.assertEqual(audit.check_documented_modes(), [])


class EvidenceRuleTest(unittest.TestCase):
    def test_the_shipped_rule_satisfies_the_gate(self):
        # Runs against the real factsrules, not a fixture.
        self.assertEqual(audit.check_evidence_rule(), [])


if __name__ == "__main__":
    unittest.main()
