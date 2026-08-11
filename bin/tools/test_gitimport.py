import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import gitimport  # noqa: E402

SAMPLE = (
    "\x1e" + "a1b2c3d" + "\x1f" + "Ada Lovelace" + "\x1f" + "ada@example.com"
    + "\x1f" + "2026-08-10T12:00:00+01:00" + "\x1f" + "feat: first commit"
    + "\n\nREADME.md\nsrc/main.c\n"
    + "\x1e" + "e4f5a6b" + "\x1f" + "Bob Babbage" + "\x1f" + "bob@example.com"
    + "\x1f" + "2026-08-11T09:30:00+01:00" + "\x1f" + "fix: typo"
    + "\n\ndocs/notes.md"
)


class GitparseTest(unittest.TestCase):
    def test_parses_records(self):
        cs = gitimport.parse_log(SAMPLE)
        self.assertEqual(len(cs), 2)

    def test_parses_commit_fields(self):
        cs = gitimport.parse_log(SAMPLE)
        c = cs[0]
        self.assertEqual(c.sha, "a1b2c3d")
        self.assertEqual(c.author, "Ada Lovelace")
        self.assertEqual(c.email, "ada@example.com")
        self.assertEqual(c.date, "2026-08-10T12:00:00+01:00")
        self.assertEqual(c.subject, "feat: first commit")

    def test_parses_changed_files(self):
        cs = gitimport.parse_log(SAMPLE)
        self.assertEqual(cs[0].files, ["README.md", "src/main.c"])
        self.assertEqual(cs[1].files, ["docs/notes.md"])

    def test_ignores_empty(self):
        self.assertEqual(gitimport.parse_log(""), [])

    def test_skip_malformed_record(self):
        self.assertEqual(gitimport.parse_log("\x1eweird\x1e"), [])

    def test_commit_leaf_shape(self):
        leafs = gitimport.commits_to_leafs(gitimport.parse_log(SAMPLE), "sample-repo")
        self.assertEqual(len(leafs), 2)
        lf = leafs[0]
        self.assertEqual(lf["type"], "commit")
        self.assertEqual(lf["repo"], "sample-repo")
        self.assertEqual(lf["source"], "sample-repo@a1b2c3d")
        self.assertIn("Ada Lovelace", lf["text"])
        self.assertIn("README.md", lf["related"])
        self.assertIn("feat: first commit", lf["heading"])


if __name__ == "__main__":
    unittest.main()