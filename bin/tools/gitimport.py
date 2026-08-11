"""gitimport - parse `git log` output and turn commits into brain leafs.

Pure, testable functions. Field grammar (see bin/git/import):

  git log --no-merges --name-only \
      --format='%x1e%H%x1f%an%x1f%ae%x1f%aI%x1f%s'

  0x1e = record separator, 0x1f = field separator.
  Files: newline-separated lines following each record's subject.
"""

from __future__ import annotations

from dataclasses import dataclass, field

REC_SEP = "\x1e"
FIELD_SEP = "\x1f"


@dataclass
class Commit:
    sha: str
    author: str
    email: str
    date: str
    subject: str
    files: list[str] = field(default_factory=list)

    def leaf_text(self, repo: str) -> str:
        head = f"commit {self.sha[:12]} in {repo} — {self.subject}"
        body = [head, f"Author: {self.author} <{self.email}>", f"Date: {self.date}"]
        if self.files:
            body.append("Changing: " + ", ".join(self.files))
        return "\n".join(body)


def parse_log(text: str) -> list[Commit]:
    """Parse `git log` output into Commit records.

    Records are separated by 0x1e. A record is fields joined by 0x1f,
    followed by optional newline-separated file paths inside the next
    segment (git emits blank line + files after each record).
    """
    commits: list[Commit] = []
    # field records and file lists alternate; simpler: split on REC_SEP,
    # each chunk = header line, possibly followed by newline + files.
    for chunk in text.split(REC_SEP):
        chunk = chunk.strip("\n")
        if not chunk:
            continue
        lines = chunk.split("\n", 1)
        header = lines[0].split(FIELD_SEP)
        if len(header) < 5:
            continue
        sha, author, email, date, subject = header[:5]
        files = [ln.strip() for ln in lines[1].splitlines() if ln.strip()] if len(lines) > 1 else []
        commits.append(Commit(sha=sha, author=author, email=email,
                              date=date, subject=subject, files=files))
    return commits


def commits_to_leafs(commits: list[Commit], repo: str) -> list[dict]:
    """Map commits to the leaf shape bin/kb/index expects (source/repo/...)."""
    out: list[dict] = []
    for c in commits:
        out.append({
            "source": f"{repo}@{c.sha}",
            "repo": repo,
            "heading": f"commit {c.sha[:12]} — {c.subject}",
            "text": c.leaf_text(repo),
            "type": "commit",
            "status": "current",
            "related": ",".join(c.files),
        })
    return out


GIT_SCHEMA = (
    "CREATE NODE TABLE IF NOT EXISTS Commit (id STRING, repo STRING, subject STRING, "
    "author STRING, email STRING, date STRING, PRIMARY KEY(id))",
    "CREATE NODE TABLE IF NOT EXISTS Person (id STRING, name STRING, email STRING, PRIMARY KEY(id))",
    "CREATE REL TABLE IF NOT EXISTS HAS_VERSION (FROM File TO Commit)",
    "CREATE REL TABLE IF NOT EXISTS AUTHORED (FROM Commit TO Person)",
)


def ensure_git_schema(conn) -> None:
    for stmt in GIT_SCHEMA:
        conn.execute(stmt)


def index_commits(conn, commits: list[Commit], repo: str) -> int:
    """Write Commit/File/Person nodes + edges, one per commit (idempotent by sha)."""
    ensure_git_schema(conn)
    for c in commits:
        conn.execute(
            "MERGE (c:Commit {id:$sha}) SET c.repo=$repo, c.subject=$subject, "
            "c.author=$author, c.email=$email, c.date=$date",
            parameters={"sha": c.sha, "repo": repo, "subject": c.subject,
                        "author": c.author, "email": c.email, "date": c.date},
        )
        conn.execute(
            "MERGE (p:Person {id:$email}) SET p.name=$name, p.email=$email",
            parameters={"email": c.email, "name": c.author},
        )
        conn.execute("MATCH (c:Commit {id:$sha}), (p:Person {id:$email}) "
                     "MERGE (c)-[:AUTHORED]->(p)",
                     parameters={"sha": c.sha, "email": c.email})
        for path in c.files:
            conn.execute(
                "MERGE (f:File {id:$fid}) SET f.path=$path, f.repo=$repo",
                parameters={"fid": f"{repo}:{path}", "path": path, "repo": repo},
            )
            conn.execute("MATCH (f:File {id:$fid}), (c:Commit {id:$sha}) "
                         "MERGE (f)-[:HAS_VERSION]->(c)",
                         parameters={"fid": f"{repo}:{path}", "sha": c.sha})
    return len(commits)