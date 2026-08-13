"""Mail markdown → leafs (no Ladybug). Brain index --with-mail uses this."""
from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import mailleafs  # noqa: E402


class MailLeafsTest(unittest.TestCase):
    def test_message_md_becomes_info_leaf(self) -> None:
        root = Path(tempfile.mkdtemp())
        msg = root / "inbox" / "alice-1"
        msg.mkdir(parents=True)
        (msg / "message.json").write_text(
            json.dumps({"receivedDate": "2026-01-15T10:00:00Z", "subject": "Hello"}),
            encoding="utf-8",
        )
        (msg / "message.md").write_text(
            "---\nroot: info\n---\n\n# Hello\n\nFrom Alice to Bob.\n",
            encoding="utf-8",
        )
        leafs = mailleafs.from_mail_root(root)
        self.assertEqual(len(leafs), 1)
        self.assertIn("Alice", leafs[0]["text"])
        self.assertTrue(leafs[0]["source"].startswith("ooMail:"))
        self.assertEqual(leafs[0]["how"], "mail/import")

    def test_since_filters_by_message_json_date(self) -> None:
        root = Path(tempfile.mkdtemp())
        for name, day in (("old", "2025-01-01"), ("new", "2026-06-01")):
            d = root / "inbox" / name
            d.mkdir(parents=True)
            (d / "message.json").write_text(
                json.dumps({"receivedDate": f"{day}T00:00:00Z"}),
                encoding="utf-8",
            )
            (d / "message.md").write_text(f"# {name}\n\nbody\n", encoding="utf-8")
        leafs = mailleafs.from_mail_root(root, since="2026-01-01")
        self.assertEqual(len(leafs), 1)
        self.assertIn("new", leafs[0]["text"])
