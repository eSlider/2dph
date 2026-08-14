"""qa/system_perf.py is an offline-gated system test (no live brain in CI)."""
from __future__ import annotations

import ast
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class SystemPerfScriptTest(unittest.TestCase):
    def test_script_compiles_and_is_read_only(self) -> None:
        path = ROOT / "qa" / "system_perf.py"
        src = path.read_text()
        compile(src, str(path), "exec")
        self.assertIn("--json", src)
        self.assertIn("qwen3.5:9b", src)
        self.assertIn("--picoclaw", src)
        self.assertIn("BRAIN_URL", src)
        self.assertIn("tools/list", src)
        self.assertIn("tools/call", src)
        self.assertIn("GATE_HEALTH_MS", src)
        self.assertIn("GATE_GET_P50_MS", src)
        self.assertNotIn("kb.lbug", src)
        self.assertNotIn("password", src.lower())
        self.assertNotIn("token", src.lower())

    def test_script_does_not_write_ladybug(self) -> None:
        tree = ast.parse((ROOT / "qa" / "system_perf.py").read_text())
        writes = [
            n.func.attr
            for n in ast.walk(tree)
            if isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute)
            and n.func.attr in {"write_text", "write_bytes", "dump"}
        ]
        self.assertEqual(writes, [], f"system_perf must not write files: {writes}")
