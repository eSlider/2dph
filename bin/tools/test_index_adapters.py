"""Import adapters write files only. Index rebuild is brain/index (D14 / Gitea #7)."""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class IndexAdapterTest(unittest.TestCase):
    def test_dry_run_fixture_corpus_does_not_write_lbug(self) -> None:
        tmp = Path(tempfile.mkdtemp())
        (tmp / "note.md").write_text("# Fixture\n\n## Leaf\n\nhello corpus\n", encoding="utf-8")
        lbug = tmp / "kb.lbug"
        try:
            import ladybug  # noqa: F401
        except ImportError:
            venv_py = ROOT / ".venv" / "bin" / "python"
            if not venv_py.is_file():
                self.skipTest("ladybug missing")
            py = str(venv_py)
        else:
            py = sys.executable
        proc = subprocess.run(
            [py, str(ROOT / "bin" / "kb" / "index"), "--dry-run", "--json", "--corpus", str(tmp)],
            cwd=ROOT,
            capture_output=True,
            text=True,
            env=os.environ.copy(),
            check=False,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        msg = json.loads(proc.stdout)
        self.assertTrue(msg.get("dry_run"))
        self.assertGreaterEqual(msg.get("corpus_total", 0), 1)
        self.assertFalse(lbug.exists(), "dry-run must not create a Ladybug file")
