"""crmfacts - pure helpers for bin/facts/crm (association proofing).

Shared with tools/ unit tests so the corpus-org parser is covered in CI.
"""

import re


def corpus_orgs(raw: str) -> dict[str, dict]:
    """Parse the orgs block of the CV knowledge-mesh YAML into id -> fields.

    Fields kept: label, kind, period, website. Stops at the first sibling
    top-level key (clients, timeline, ...).
    """
    m = re.search(r"^orgs:\n(.*?)\n^(?:clients|timeline|tech_weights|nodes|edges):", raw, re.S | re.M)
    if not m:
        return {}
    orgs: dict[str, dict] = {}
    cur = None
    for line in m.group(1).splitlines():
        lm = re.match(r"^\s*- id:\s*(\S+)", line)
        if lm:
            cur = lm.group(1)
            orgs[cur] = {}
            continue
        fm = re.match(r"^\s+(\w+):\s*(.*)$", line)
        if fm and cur and fm.group(1) in ("label", "kind", "period", "website"):
            orgs[cur][fm.group(1)] = fm.group(2).strip()
    return orgs