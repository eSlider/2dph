"""factsrules - the 2-source pairing rule behind bin/facts/extract.

Pure functions: no subprocess, no filesystem, no database. bin/facts/extract
gathers the observations (docker ps, compose services, ssh config, doc
mentions) and this module decides which pairings are strong enough to become a
`facts` leaf. bin/facts/audit re-checks the rule against a fixture, so the
gate fails when someone loosens it.

The rule (AGENTS.md D8): an assertion needs >=2 *independent* sources or it is
`(not confirmed)`. make_fact() refuses to build a Fact from fewer, so a single
observation cannot reach the facts root by accident.
"""
from __future__ import annotations

from dataclasses import dataclass
from pathlib import PurePath

MIN_SOURCES = 2
HOW = "facts/extract"


@dataclass(frozen=True)
class Fact:
    """One confirmed assertion plus the sources it was paired from."""

    text: str
    sources: tuple[str, ...]
    loc: str
    how: str = HOW

    @property
    def source(self) -> str:
        """Evidence string as stored on the leaf; facts/audit db greps ' x '."""
        return " x ".join(self.sources)

    def as_dict(self) -> dict:
        return {"text": self.text, "source": self.source, "loc": self.loc, "how": self.how}


def make_fact(text: str, sources: list[str], loc: str, how: str = HOW) -> Fact:
    """Build a Fact or refuse. Independent means distinct: the same observation
    named twice is one source, not two."""
    distinct = {s for s in sources if s}
    if len(distinct) < MIN_SOURCES:
        raise ValueError(
            f"fact needs >={MIN_SOURCES} independent sources, got {sorted(distinct)}: {text!r}"
        )
    return Fact(text=text, sources=tuple(sources), loc=loc, how=how)


def pair_container_compose(compose_by_container: dict[str, dict[str, list[str]]]) -> list[Fact]:
    """S1 runtime (docker ps) x S2 declared (the container's *own* compose file).

    Takes {container: {compose_path: [services]}} — candidates are scoped per
    container, because a service name like `db` occurs in many unrelated
    projects and pairing across them would not be a second source at all.
    First matching compose file wins, one fact per container.
    """
    facts: list[Fact] = []
    for name, files in compose_by_container.items():
        for path, services in files.items():
            if name in services:
                fname = PurePath(path).name
                facts.append(make_fact(
                    text=f"container '{name}' is running and declared in {fname}",
                    sources=["docker ps", f"compose:{fname}"],
                    loc=f"{path}:{name}",
                ))
                break
    return facts


def pair_container_repo_compose(running: list[str], repo_services: list[str],
                                compose_name: str) -> list[Fact]:
    """S1 runtime x S2 the repo's own compose file."""
    overlap = sorted(set(repo_services) & set(running))
    return [
        make_fact(
            text=f"container '{name}' is running and declared in compose",
            sources=["docker ps", compose_name],
            loc="docker ps; docker compose config",
        )
        for name in overlap
    ]


def pair_host_docs(ssh_hosts: list[str], doc_terms: set[str], doc_markers: list[str]) -> list[Fact]:
    """S1 ~/.ssh/config x S2 a doc in this repo naming the same host."""
    marker = ", ".join(doc_markers)
    return [
        make_fact(
            text=f"host '{host}' is configured in ~/.ssh/config and referenced in this repo",
            sources=["ssh config", f"docs({marker})"],
            loc=f"~/.ssh/config:{host}",
        )
        for host in ssh_hosts if host in doc_terms
    ]


def pair_container_host(running: list[str], ssh_hosts: list[str]) -> list[Fact]:
    """S1 docker ps x S2 ~/.ssh/config naming the same thing."""
    known = set(ssh_hosts)
    if not known:
        return []
    return [
        make_fact(
            text=f"container '{name}' is running and matches configured host '{name}'",
            sources=["docker ps", "ssh config"],
            loc=f"docker ps:{name}; ~/.ssh/config:{name}",
        )
        for name in running if name in known
    ]


DOC_MARKERS = ["README.md", "PLAN.md", "AGENTS.md"]


def pair_all(*, running: list[str], compose_by_container: dict[str, dict[str, list[str]]],
             repo_services: list[str], repo_compose_name: str,
             ssh_hosts: list[str], doc_terms: set[str],
             doc_markers: list[str] | None = None) -> list[Fact]:
    """Every pairing, deduped by text, order preserved."""
    doc_markers = doc_markers or DOC_MARKERS
    facts = pair_container_compose(compose_by_container)
    if repo_services and running:
        facts += pair_container_repo_compose(running, repo_services, repo_compose_name)
    facts += pair_host_docs(ssh_hosts, doc_terms, doc_markers)
    facts += pair_container_host(running, ssh_hosts)
    return dedupe(facts)


def dedupe(facts: list[Fact]) -> list[Fact]:
    seen: set[str] = set()
    out: list[Fact] = []
    for fact in facts:
        if fact.text in seen:
            continue
        seen.add(fact.text)
        out.append(fact)
    return out
