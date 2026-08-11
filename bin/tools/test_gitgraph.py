import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import kblib  # noqa: E402
import gitimport  # noqa: E402

SAMPLE = (
    "\x1e" + "a1b2c3d" + "\x1f" + "Ada Lovelace" + "\x1f" + "ada@example.com"
    + "\x1f" + "2026-08-10T12:00:00+01:00" + "\x1f" + "feat: first commit"
    + "\n\nREADME.md\nsrc/main.c\n"
)

COMMIT_PERSON_SCHEMA = (
    "CREATE NODE TABLE IF NOT EXISTS Commit (id STRING, repo STRING, subject STRING, "
    "author STRING, email STRING, date STRING, PRIMARY KEY(id))"
)
PERSON_SCHEMA = (
    "CREATE NODE TABLE IF NOT EXISTS Person (id STRING, name STRING, email STRING, PRIMARY KEY(id))"
)
HAS_VERSION_SCHEMA = "CREATE REL TABLE IF NOT EXISTS HAS_VERSION (FROM File TO Commit)"
AUTHORED_SCHEMA = "CREATE REL TABLE IF NOT EXISTS AUTHORED (FROM Commit TO Person)"


class GitGraphTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.dbpath = os.path.join(self.dir, "kb.lbug")
        self.db, self.conn = kblib.connect(self.dbpath, read_only=False)
        kblib.init_schema(self.conn)
        self.conn.execute(COMMIT_PERSON_SCHEMA)
        self.conn.execute(PERSON_SCHEMA)
        self.conn.execute(HAS_VERSION_SCHEMA)
        self.conn.execute(AUTHORED_SCHEMA)

    def tearDown(self):
        self.conn.close()
        self.db.close()

    def test_index_commits_creates_nodes_and_edges(self):
        cs = gitimport.parse_log(SAMPLE)
        gitimport.index_commits(self.conn, cs, "sample-repo")
        rp = self.conn.execute("MATCH (p:Person) RETURN p.name, p.email").get_all()
        self.assertEqual([tuple(r) for r in rp], [("Ada Lovelace", "ada@example.com")])
        rc = self.conn.execute("MATCH (c:Commit) RETURN c.id, c.repo").get_all()
        self.assertEqual(len(rc), 1)
        self.assertEqual(rc[0][1], "sample-repo")
        # File -[:HAS_VERSION]-> Commit -[:AUTHORED]-> Person
        rf = self.conn.execute(
            "MATCH (f:File)-[:HAS_VERSION]->(c:Commit)-[:AUTHORED]->(p:Person) "
            "RETURN f.path, c.id, p.email").get_all()
        paths = sorted(r[0] for r in rf)
        self.assertEqual(paths, ["README.md", "src/main.c"])
        self.assertTrue(all(r[2] == "ada@example.com" for r in rf))

    def test_index_commits_idempotent(self):
        cs = gitimport.parse_log(SAMPLE)
        gitimport.index_commits(self.conn, cs, "sample-repo")
        gitimport.index_commits(self.conn, cs, "sample-repo")
        n = self.conn.execute("MATCH (c:Commit) RETURN count(*)").get_all()[0][0]
        self.assertEqual(n, 1)
        p = self.conn.execute("MATCH (p:Person) RETURN count(*)").get_all()[0][0]
        self.assertEqual(p, 1)


if __name__ == "__main__":
    unittest.main()