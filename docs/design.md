# Design — facts, info, deduction

## Two roots, one transaction

`root` — `facts` | `info` — is a column on every node and edge. Both roots
live in the **same** Ladybug file and are written inside the **same
transaction** (D12). Splitting is semantic, not physical:

- `facts` — assertions backed by ≥2 independent sources (`confidence: confirmed`)
  or marked `partial`/`hypothesis` when the second source is missing.
- `info` — narrative/descriptive leafs (how-tos, READMEs, notes). Searchable,
  never asserted as an answer.

ACID: Ladybug commits facts + info atomically; the audit can treat a snapshot
as one consistent state.

## Deduction search

```
bin/kb/search "question"
  1. facts root   — confirmed answers only   → return with evidence links
  2. info  root   — supporting narrative     → snippets, marked (not confirmed)
  3. web-search   — second independent source → upgrade hypothesis to confirmed
```

`--hop N` follows graph edges (sibling leaves under a heading, owning file,
`related:` files, vector-neighbour leaves) — the deduction walk.

## Who / What / How / Where / When + evidence

Every assertion edge carries:

| prop | meaning |
|------|---------|
| who | subject/object persons, services, hosts |
| what | the associated subjects/objects (predicate) |
| how | methods used to read it (compose file? docker ps? ssh config?) |
| where | environment + filesystem path / host |
| when | timestamp / source_rev (git commit, mtime) |
| evidence | ≥2 source refs (compose path, runtime container, config) |
| confidence | confirmed / partial / hypothesis |
| root | facts / info |

## Versioning — nothing is timeless

Content leafs: `sha256`, `observed_at`, `source_rev`, `confidence`. Stale = a
file changed on disk (git HEAD/mtime) after its last observed `source_rev`.
`File-[:HAS_VERSION]->Commit-[:AUTHORED]->Person` records the history of every
content leaf.

A planned `stale` mode will flag leafs whose observed revision is behind the
corpus HEAD. Not implemented — `bin/facts/audit` has `self` and `db` today
(tracked in PLAN.md, open questions).

## Sources (auto-pairing)

Evidence is stored as `Evidence` nodes linked by `(Evidence)-[:SUPPORTS]->(Leaf)`,
never as a substring of `Leaf.source` — a flattened string cannot be counted, and
any shape test over it (`" x " in source`) is satisfied by `"bullshit x bullshit2"`.

Each observation carries a **kind**, a **method**, a **locator** (where to look
again) and an **origin** (the system of record it came from):

| kind | examples |
|------|----------|
| `runtime` | `docker ps`, systemctl, a live query |
| `declared` | compose files, manifests, IaC |
| `netconfig` | `~/.ssh/config`, DNS, firewall |
| `doc` | markdown written by a human |
| `vcs` | commits, authors, tags |
| `external` | CRM, API, web — outside this machine and repo |

**Confirmed = ≥2 observations differing in both `kind` and `origin`.** Differing
in only one is not independence: two compose files are two paths but one kind of
claim, and one file read two ways is still one system of record. A source
without a locator is refused outright — evidence you cannot go back and look at
is not evidence. Single source = hypothesis + `(not confirmed)`. Conflicting
pairings (≥2 yes vs ≥2 no) = hypothesis (OQ1 → v2 resolution).

`bin/tools/factsrules.py` enforces this at write time (`make_fact`),
`bin/facts/audit db` re-checks it over the graph.