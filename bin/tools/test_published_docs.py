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

    def test_picoclaw_compose_profile_has_mcp_example(self) -> None:
        compose = (ROOT / "compose.yaml").read_text()
        self.assertIn('profiles: ["picoclaw"]', compose)
        self.assertIn("127.0.0.1:8630", compose)
        example = (ROOT / "deploy" / "picoclaw" / "mcp.json.example").read_text()
        self.assertIn("127.0.0.1:8630/mcp", example)
        self.assertNotIn("password", example.lower())
        self.assertNotIn("token", example.lower())
        docs = (ROOT / "docs" / "picoclaw.md").read_text()
        self.assertIn("search", docs)
        self.assertIn("throttled", docs)

    def test_readme_read_path_is_go(self) -> None:
        plan = (ROOT / "PLAN.md").read_text()
        self.assertIn("get.go", plan)
        self.assertIn("CI fallback", plan)
        design = (ROOT / "docs" / "design.md").read_text()
        self.assertIn("internal/brain/rank", design)
        self.assertIn("They do not exec Python", design)

    def test_openapi_mcp_from_same_handlers(self) -> None:
        plan = (ROOT / "PLAN.md").read_text()
        self.assertIn("D20", plan)
        self.assertIn("/openapi.json", (ROOT / "README.md").read_text())
        self.assertIn("/mcp", (ROOT / "README.md").read_text())
        skill = (ROOT / "skills" / "brain" / "SKILL.md").read_text()
        self.assertIn("/mcp", skill)
        self.assertFalse((ROOT / "skills" / "db-yaml").exists())
        self.assertTrue((ROOT / "skills" / "postgres" / "SKILL.md").is_file())

    def test_cgo_zig_and_index_profile(self) -> None:
        plan = (ROOT / "PLAN.md").read_text()
        self.assertIn("D21", plan)
        self.assertIn("zig cc", plan)
        dockerfile = (ROOT / "Dockerfile").read_text()
        self.assertIn("bin/cgo/zcc", dockerfile)
        self.assertIn("FROM debian:bookworm-slim AS api", dockerfile)
        self.assertIn("FROM python:3.12-slim AS index", dockerfile)
        api = dockerfile[dockerfile.index("FROM debian:bookworm-slim AS api") :]
        self.assertNotIn("pip install", api)
        compose = (ROOT / "compose.yaml").read_text()
        self.assertIn('profiles: ["index"]', compose)
        self.assertIn("target: api", compose)

    def test_reasoner_docs_name_real_hf_ids_cpu_sidecar(self) -> None:
        docs = (ROOT / "docs" / "reasoner.md").read_text()
        for hf in (
            "Qwen/Qwen3.5-9B",
            "Qwen/Qwen3.6-27B",
            "prism-ml/Bonsai-27B-gguf",
        ):
            self.assertIn(hf, docs)
        self.assertIn("no official qwen3.6-9b", docs.lower())
        self.assertIn("OLLAMA_NUM_GPU", docs)
        self.assertIn("rss_mb", docs)
        self.assertIn("vram_mb", docs)
        self.assertIn("3/3", docs)
        self.assertIn("Do not claim 9B is better at tools", docs)
        self.assertNotIn("Qwen/Qwen3.6-9B", docs)
        plan = (ROOT / "PLAN.md").read_text()
        self.assertIn("D18", plan)
        self.assertIn("Qwen/Qwen3.5-9B", plan)
        compose = (ROOT / "compose.yaml").read_text()
        self.assertIn('profiles: ["reasoner"]', compose)
        self.assertIn("OLLAMA_NUM_GPU", compose)
        self.assertIn("127.0.0.1:11435", compose)
        dockerfile = (ROOT / "Dockerfile").read_text()
        self.assertNotIn(".gguf", dockerfile.lower())
        self.assertNotIn(".safetensors", dockerfile.lower())
        api = dockerfile[dockerfile.index("FROM debian:bookworm-slim AS api") :]
        self.assertNotIn("COPY models", api)
        self.assertNotIn("qwen", api.lower())

    def test_readme_search_escalates_web(self) -> None:
        text = (ROOT / "README.md").read_text()
        self.assertIn("--no-web", text)
        self.assertIn("D17", (ROOT / "PLAN.md").read_text())
        skill = (ROOT / "skills" / "brain" / "SKILL.md").read_text()
        self.assertIn("`web` block", skill)

    def test_docs_do_not_claim_hop_walks(self) -> None:
        paths = [
            ROOT / "README.md",
            ROOT / "docs" / "design.md",
            ROOT / "skills" / "brain" / "SKILL.md",
            ROOT / "skills" / "diataxis-docs" / "SKILL.md",
            ROOT / "docs" / "runbook.md",
            ROOT / "docs" / "README.md",
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

    def test_docs_are_portable_diataxis(self) -> None:
        index = (ROOT / "docs" / "README.md").read_text()
        self.assertIn("type: reference", index)
        for d in ("D3", "D6", "D14", "D15", "D17", "D18"):
            self.assertIn(d, index)
        runbook = (ROOT / "docs" / "runbook.md").read_text()
        self.assertIn("type: howto", runbook)
        self.assertIn("bin/brain/search.go", runbook)
        self.assertIn("bin/brain/index.go", runbook)
        self.assertNotIn("search.ops.io", runbook)
        self.assertNotIn("/mnt/", runbook)
        self.assertNotIn("/home/", runbook)
        readme = (ROOT / "README.md").read_text()
        self.assertIn("docs/runbook.md", readme)
        self.assertNotIn("search.ops.io", readme)
