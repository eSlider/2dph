"""Skills must name live commands; every bin/ path in SKILL.md must exist."""
from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BIN_PATH = re.compile(r"(bin/[A-Za-z0-9_./-]+)")


class SkillsTest(unittest.TestCase):
    def test_db_yaml_renamed_to_postgres(self) -> None:
        self.assertFalse(
            (ROOT / "skills" / "db-yaml").exists(),
            "skills/db-yaml must be skills/postgres",
        )
        self.assertTrue((ROOT / "skills" / "postgres" / "SKILL.md").is_file())
        text = (ROOT / "skills" / "postgres" / "SKILL.md").read_text()
        self.assertIn("bin/postgres/query.go", text)
        self.assertNotIn("search.ops.io", text)

    def test_every_bin_path_in_skills_exists(self) -> None:
        missing: list[str] = []
        for path in (ROOT / "skills").rglob("SKILL.md"):
            text = path.read_text()
            for m in BIN_PATH.finditer(text):
                rel = m.group(1).rstrip(")`.,;")
                candidate = ROOT / rel
                if not candidate.exists():
                    missing.append(f"{path.relative_to(ROOT)}: {rel}")
        self.assertEqual(missing, [], "skill bin paths must exist")

    def test_brain_skill_lists_generated_tools(self) -> None:
        tools = (ROOT / "skills" / "brain" / "tools.md").read_text()
        skill = (ROOT / "skills" / "brain" / "SKILL.md").read_text()
        self.assertIn("tools.md", skill)
        for name in ("search", "get", "stats", "audit"):
            self.assertIn(f"`{name}`", tools)

    def test_picoclaw_lists_tool_order(self) -> None:
        skill = (ROOT / "skills" / "picoclaw" / "SKILL.md").read_text()
        agents = (ROOT / "AGENTS.md").read_text()
        self.assertIn("**`search`**", skill)
        self.assertIn("**`get`**", skill)
        self.assertIn("**`audit`**", skill)
        self.assertIn("throttled", skill.lower())
        self.assertIn("not a negative finding", agents)
        self.assertIn("Fact-check every", agents)
