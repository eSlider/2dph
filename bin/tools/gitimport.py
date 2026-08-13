"""gitimport - Ladybug graph writes for Commit/File/Person (no git binary).

Commit records come from bin/git/import.go (go-git). This module only MERGEs
the version graph File-[:HAS_VERSION]->Commit-[:AUTHORED]->Person.
"""
from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class Commit:
    sha: str
    author: str
    email: str
    date: str
    subject: str
    files: list[str] = field(default_factory=list)


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
