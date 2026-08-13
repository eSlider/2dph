"""Every bin/ path named in skills/ must exist on disk."""
from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BIN_PATH = re.compile(r"\b(bin/[A-Za-z0-9_./-]+)")


class SkillsBinPathsTest(unittest.TestCase):
    def test_agent_cost_skill_is_gone(self) -> None:
        self.assertFalse(
            (ROOT / "skills" / "agent-cost").exists(),
            "skills/agent-cost documents bin/agents/cost which does not exist",
        )

    def test_brain_skill_replaces_kb_search(self) -> None:
        self.assertTrue((ROOT / "skills" / "brain" / "SKILL.md").is_file())
        self.assertFalse((ROOT / "skills" / "kb-search").exists())

    def test_skill_bin_paths_exist(self) -> None:
        missing: list[str] = []
        for skill in sorted((ROOT / "skills").rglob("SKILL.md")):
            text = skill.read_text()
            for match in BIN_PATH.findall(text):
                rel = match.rstrip("`'.,")
                if rel.endswith(".go") or Path(rel).suffix == "" or Path(rel).suffix in {".go", ".py"}:
                    p = ROOT / rel
                    if not p.exists():
                        missing.append(f"{skill.relative_to(ROOT)}: {rel}")
        self.assertEqual(missing, [], "SKILL.md names bin/ paths that do not exist")
