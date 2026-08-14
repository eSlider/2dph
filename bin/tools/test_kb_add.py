"""Incremental add writes leafs without deleting kb.lbug."""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class KbAddCLITest(unittest.TestCase):
    def test_json_add_does_not_delete_db(self) -> None:
        tmp = Path(tempfile.mkdtemp())
        dbpath = tmp / "kb.lbug"
        py = sys.executable
        venv_py = ROOT / ".venv" / "bin" / "python"
        if venv_py.is_file():
            py = str(venv_py)
        payload = {
            "text": "cli zebra leaf",
            "root": "info",
            "source": "cli-test",
            "confidence": "confirmed",
            "how": "test",
            "loc": str(tmp),
            "type": "reference",
            "embedding": [0.0] * 256,
        }
        payload["embedding"][0] = 0.3
        proc = subprocess.run(
            [py, str(ROOT / "bin" / "kb" / "add"), "--db", str(dbpath), "--json"],
            cwd=ROOT,
            input=json.dumps(payload),
            capture_output=True,
            text=True,
            env=os.environ.copy(),
            check=False,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertTrue(dbpath.exists(), "add must create the db, not skip write")
        out = json.loads(proc.stdout)
        self.assertEqual(out.get("mode"), "add")
        self.assertEqual(len(out.get("ids") or []), 1)
        again = subprocess.run(
            [py, str(ROOT / "bin" / "kb" / "add"), "--db", str(dbpath), "--json"],
            cwd=ROOT,
            input=json.dumps({
                **payload,
                "text": "second moose leaf",
                "source": "cli-test-2",
            }),
            capture_output=True,
            text=True,
            env=os.environ.copy(),
            check=False,
        )
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertTrue(dbpath.exists())
        second = json.loads(again.stdout)
        self.assertEqual(len(second.get("ids") or []), 1)
        self.assertNotEqual(out["ids"][0], second["ids"][0])


if __name__ == "__main__":
    unittest.main()
