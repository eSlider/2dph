"""Published docs must match live commands (Gitea SoT, brain/search, no fake --hop)."""
from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class PublishedDocsTest(unittest.TestCase):
    def test_readme_points_issues_at_gitea(self) -> None:
        text = (ROOT / "README.md").read_text()
        self.assertIn(
            "https://git.produktor.io/eSlider/2dph/issues",
            text,
            "README must point issues at Gitea",
        )

    def test_plan_d15_names_gitea_origin(self) -> None:
        text = (ROOT / "PLAN.md").read_text()
        self.assertIn("D15", text)
        self.assertIn("git.produktor.io/eSlider/2dph", text)

    def test_readme_primary_search_is_brain(self) -> None:
        text = (ROOT / "README.md").read_text()
        self.assertIn(
            "bin/brain/search.go",
            text,
            "README deduction search must name bin/brain/search.go",
        )

    def test_readme_index_is_brain_not_index_mail(self) -> None:
        text = (ROOT / "README.md").read_text()
        self.assertIn("bin/brain/index.go", text)
        self.assertNotIn(
            "bin/mail/index_mail",
            text,
            "mail index is a brain write; README must name bin/brain/index.go",
        )

    def test_readme_git_import_is_gogit(self) -> None:
        text = (ROOT / "README.md").read_text()
        self.assertIn("bin/git/import.go", text)
        self.assertIn("go-git", text)
        self.assertIn("D19", (ROOT / "PLAN.md").read_text())

    def test_web_search_is_go_not_ops_host(self) -> None:
        readme = (ROOT / "README.md").read_text()
        self.assertIn("bin/web/search.go", readme)
        skill = (ROOT / "skills" / "web-search" / "SKILL.md").read_text()
        self.assertIn("bin/web/search.go", skill)
        self.assertNotIn("search.ops.io", skill)
        self.assertNotIn("search.ops.io", readme)
        compose = (ROOT / "compose.yaml").read_text()
        self.assertIn("searxng", compose)
        self.assertNotIn("search.ops.io", compose)
        settings = (ROOT / "deploy" / "searxng" / "settings.yml").read_text()
        self.assertNotIn("password", settings.lower())
        self.assertIn("json", settings)

    def test_docs_do_not_claim_hop_walks(self) -> None:
        paths = [
            ROOT / "README.md",
            ROOT / "docs" / "design.md",
            ROOT / "skills" / "brain" / "SKILL.md",
            ROOT / "skills" / "diataxis-docs" / "SKILL.md",
        ]
        # Command-style `--hop 1` / `--hop N` plus follow/walk = the old lie.
        # Honest "not implemented" notes must not match.
        lie = re.compile(r"--hop (?:N|1).*(?:follow|walk)", re.I | re.S)
        for path in paths:
            text = path.read_text()
            self.assertIsNone(
                lie.search(text),
                f"{path.relative_to(ROOT)} still claims --hop walks the graph",
            )
