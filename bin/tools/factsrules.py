"""factsrules - the 2-source pairing rule behind bin/facts/extract.

Pure functions: no subprocess, no filesystem, no database. bin/facts/extract
gathers the observations, this module decides which pairings are strong enough
to become a `facts` leaf, and bin/facts/audit re-checks the rule.

The rule (AGENTS.md D8): an assertion needs >=2 *independent* sources or it is
`(not confirmed)`. Independence is the hard part, and counting strings does not
establish it: two compose files are two paths but one method, one kind of
claim, and often one author. So a Source is structured, and two sources count
as independent only when they differ in **kind** (what sort of observation)
*and* in **origin** (which system of record produced it).

Each Source also carries a `locator` — evidence you cannot go back and look at
is not evidence. make_fact() refuses anything that fails these rules, so a
weak pairing cannot reach the facts root by accident.
"""
from __future__ import annotations

import hashlib
from dataclasses import dataclass
from pathlib import PurePath

MIN_SOURCES = 2
HOW = "facts/extract"

# Kinds of observation. Two sources of the same kind are the same sort of
# claim, however many files they span.
KINDS = {
    "runtime": "observed running state (docker ps, systemctl, a live query)",
    "declared": "declared configuration (compose, manifests, IaC)",
    "netconfig": "network/host configuration (ssh config, DNS, firewall)",
    "doc": "prose written by a human (markdown, README, notes)",
    "vcs": "version control history (commits, authors, tags)",
    "external": "a system outside this machine and repo (CRM, API, web)",
}


@dataclass(frozen=True)
class Source:
    """One observation backing an assertion.

    kind    - taxonomy entry from KINDS
    method  - how it was observed, human readable ('docker ps', 'compose:a.yaml')
    locator - where to look again ('README.md:12', 'docker ps:chat')
    origin  - the system of record it came from ('docker-daemon', 'file:/x.yaml')
    """

    kind: str
    method: str
    locator: str
    origin: str

    @property
    def id(self) -> str:
        raw = f"{self.kind}\0{self.origin}\0{self.locator}"
        return hashlib.sha256(raw.encode()).hexdigest()[:24]

    def as_dict(self) -> dict:
        return {"id": self.id, "kind": self.kind, "method": self.method,
                "locator": self.locator, "origin": self.origin}


@dataclass(frozen=True)
class Fact:
    """One confirmed assertion plus the independent sources behind it."""

    text: str
    sources: tuple[Source, ...]
    how: str = HOW

    @property
    def source(self) -> str:
        """Human-readable rendering stored on the leaf. Derived, never authored
        by hand, and never what the audit trusts."""
        return " x ".join(s.method for s in self.sources)

    @property
    def loc(self) -> str:
        return "; ".join(s.locator for s in self.sources)

    def evidence(self) -> list[dict]:
        return [s.as_dict() for s in self.sources]

    def as_dict(self) -> dict:
        return {"text": self.text, "source": self.source, "loc": self.loc,
                "how": self.how, "evidence": self.evidence()}


def check_independence(sources: list[Source]) -> list[str]:
    """Why this set of sources does not establish a fact. Empty = it does."""
    problems: list[str] = []
    for src in sources:
        if src.kind not in KINDS:
            problems.append(f"unknown source kind {src.kind!r} (known: {sorted(KINDS)})")
        if not src.locator:
            problems.append(f"source {src.method!r} has no locator to re-check")
        if not src.origin:
            problems.append(f"source {src.method!r} has no origin")
    if len(sources) < MIN_SOURCES:
        problems.append(f"needs >={MIN_SOURCES} sources, got {len(sources)}")
        return problems
    if len({s.kind for s in sources}) < MIN_SOURCES:
        problems.append(
            f"sources share one kind ({sorted({s.kind for s in sources})}); "
            "same kind of observation is corroboration, not independence"
        )
    if len({s.origin for s in sources}) < MIN_SOURCES:
        problems.append(
            f"sources share one origin ({sorted({s.origin for s in sources})}); "
            "one system of record cannot confirm itself"
        )
    return problems


def make_fact(text: str, sources: list[Source], how: str = HOW) -> Fact:
    """Build a Fact or refuse, with the reason."""
    problems = check_independence(sources)
    if problems:
        raise ValueError(f"{text!r}: " + "; ".join(problems))
    return Fact(text=text, sources=tuple(sources), how=how)


def pair_container_compose(compose_by_container: dict[str, dict[str, list[str]]]) -> list[Fact]:
    """runtime (docker ps) x declared (the container's *own* compose file).

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
                    f"container '{name}' is running and declared in {fname}",
                    [
                        Source("runtime", "docker ps", f"docker ps:{name}", "docker-daemon"),
                        Source("declared", f"compose:{fname}", f"{path}:{name}", f"file:{path}"),
                    ],
                ))
                break
    return facts


def pair_container_repo_compose(running: list[str],
                                repo_services_by_file: dict[str, list[str]]) -> list[Fact]:
    """runtime x declared, against this repo's own compose file(s).

    Keyed by file so the locator names the compose that actually declares the
    service, not just the first one on the list.
    """
    facts: list[Fact] = []
    for path, services in repo_services_by_file.items():
        for name in sorted(set(services) & set(running)):
            facts.append(make_fact(
                f"container '{name}' is running and declared in compose",
                [
                    Source("runtime", "docker ps", f"docker ps:{name}", "docker-daemon"),
                    Source("declared", PurePath(path).name, f"{path}:{name}", f"file:{path}"),
                ],
            ))
    return facts


def pair_host_docs(ssh_hosts: list[str], doc_hits: dict[str, list[str]]) -> list[Fact]:
    """netconfig (~/.ssh/config) x doc (a file in this repo naming the host).

    doc_hits maps host -> ['README.md:12', ...]; the locator is kept so the
    claim can be looked up again.
    """
    facts: list[Fact] = []
    for host in ssh_hosts:
        hits = doc_hits.get(host)
        if not hits:
            continue
        doc_locator = hits[0]
        doc_file = doc_locator.rsplit(":", 1)[0]
        facts.append(make_fact(
            f"host '{host}' is configured in ~/.ssh/config and referenced in this repo",
            [
                Source("netconfig", "ssh config", f"~/.ssh/config:{host}", "file:~/.ssh/config"),
                Source("doc", f"docs({doc_file})", doc_locator, f"file:{doc_file}"),
            ],
        ))
    return facts


def pair_container_host(running: list[str], ssh_hosts: list[str]) -> list[Fact]:
    """runtime (docker ps) x netconfig (~/.ssh/config) naming the same thing."""
    known = set(ssh_hosts)
    if not known:
        return []
    return [
        make_fact(
            f"container '{name}' is running and matches configured host '{name}'",
            [
                Source("runtime", "docker ps", f"docker ps:{name}", "docker-daemon"),
                Source("netconfig", "ssh config", f"~/.ssh/config:{name}", "file:~/.ssh/config"),
            ],
        )
        for name in running if name in known
    ]


def pair_all(*, running: list[str], compose_by_container: dict[str, dict[str, list[str]]],
             repo_services_by_file: dict[str, list[str]],
             ssh_hosts: list[str], doc_hits: dict[str, list[str]]) -> list[Fact]:
    """Every pairing, deduped by text, order preserved."""
    facts = pair_container_compose(compose_by_container)
    if repo_services_by_file and running:
        facts += pair_container_repo_compose(running, repo_services_by_file)
    facts += pair_host_docs(ssh_hosts, doc_hits)
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
