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
bin/brain/search.go "question"
  1. facts root   — confirmed answers only   → return with evidence links
  2. info  root   — supporting narrative     → snippets, marked (not confirmed)
  3. web-search   — second independent source → upgrade hypothesis to confirmed
     (`web` block from `bin/web/search.go` when no facts hit; status `throttled`
     is not evidence of absence; `--no-web` / `--root` skip it)
```

`--hop` is not implemented yet (needs File/FROM_FILE edges). The flag is an
error; it is not a graph walk.

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
content leaf. Commit records come from `bin/git/import.go` (go-git, no git
binary); conversion prints leafs, brain write is `bin/brain/index.go`.

`bin/facts/audit stale` flags leafs whose observed revision is behind the
corpus HEAD.

## Sources (auto-pairing)

- A: runtime state — `docker ps` (container running), ports actually bound
- B: declared config — `docker-compose.yml`, `~/.ssh/config`, `homeserver.yaml`
- C: narrative — READMEs, AGENTS.md, docs

Confirmed = A×B or B×C agreement. Single source = hypothesis + `(not confirmed)`.
Conflicting pairings (≥2 yes vs ≥2 no) = hypothesis (OQ1 → v2 resolution).

## Read path

`bin/brain/get.go`, `stats.go`, and `eval.go` call `internal/brain` with cgo
(`system_ladybug`). They do not exec Python. Control questions for recall@5
live in `internal/brain/rank` so CI can test the table without libladybug.
Python `bin/kb/{get,stats,eval}` remain for GitHub Actions until the runner
has ladybug cgo. Index/write is still `bin/kb/index`.