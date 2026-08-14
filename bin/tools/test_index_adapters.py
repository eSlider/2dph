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

    def test_facts_json_and_chats_land_on_rebuild(self) -> None:
        """Gitea #18: facts (2-source) + chats markdown become leafs on rebuild."""
        tmp = Path(tempfile.mkdtemp())
        dbpath = tmp / "kb.lbug"
        chats = tmp / "chats"
        chats.mkdir()
        (chats / "alice.md").write_text(
            "# Chat\n\n## Alice and Bob\n\nhello from chats fixture unique-chat-token\n",
            encoding="utf-8",
        )
        facts_path = tmp / "facts.json"
        facts_path.write_text(json.dumps([{
            "text": "container 'brain' unique-fact-token is running and declared in compose.yaml",
            "source": "docker ps x compose.yaml",
            "loc": "compose.yaml:brain",
            "how": "facts/extract",
        }]), encoding="utf-8")
        venv_py = ROOT / ".venv" / "bin" / "python"
        py = str(venv_py) if venv_py.is_file() else sys.executable
        proc = subprocess.run(
            [
                py, str(ROOT / "bin" / "kb" / "index"),
                "--rebuild", "--db", str(dbpath), "--no-defaults",
                "--with-chats", str(chats),
                "--facts-json", str(facts_path),
                "--json",
            ],
            cwd=ROOT,
            capture_output=True,
            text=True,
            env=os.environ.copy(),
            check=False,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        msg = json.loads(proc.stdout)
        self.assertGreaterEqual(msg.get("facts_leafs", 0), 1)
        self.assertGreaterEqual(msg.get("chat_leafs", 0), 1)
        self.assertTrue(dbpath.exists())
        sys.path.insert(0, str(ROOT / "bin" / "tools"))
        import kblib
        db, conn = kblib.connect(dbpath, read_only=True)
        try:
            stats = kblib.stats(conn)
            self.assertGreaterEqual(stats["by_root"].get("facts", 0), 1)
            fts = kblib.query_fts(conn, "unique-chat-token", 5)
            self.assertTrue(fts, "chats markdown must be FTS-searchable")
            fact_hits = kblib.query_fts(conn, "unique-fact-token", 5)
            self.assertTrue(any(h.get("root") == "facts" for h in fact_hits))
            src = conn.execute(
                "MATCH (l:Leaf {root:'facts'}) RETURN l.source"
            ).get_all()
            self.assertTrue(any(" x " in str(r[0]) for r in src))
        finally:
            conn.close()
            db.close()
