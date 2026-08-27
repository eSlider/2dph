# Facts ingestion (root=facts) — procedure

A fact is only stored under `root=facts` when it is backed by **≥2 independent
sources** (evidence-first, AGENTS.md). Single-source leafs stay in `info` as
`(not confirmed)` narrative.

## Why the facts-root can be empty

`internal/brain/corpus.go` (`WriteCorpus`) writes **every** corpus leaf with
`Root: "info"` — each leaf is derived from one file/commit, so it has exactly
one source and can never be a fact on its own. The facts layer is a **separate
stage**: `bin/facts/extract.go` acquires 2-source facts (running containers ×
compose, ssh hosts × docs) and writes them with `Root: "facts"`.

A rebuild via `bin/brain/index.go --rebuild` only ingests facts when
`--with-facts` / `--facts-json` is passed. If a production `var/kb.lbug` was
rebuilt without that flag, **all** leafs land in `info` and the facts-root is
empty (observed: #181 — 9204 info, 0 facts; all 9201 confirmed leaves are
single-file corpus leafs, so **none** qualify for re-rooting by the ≥2-sources
rule).

## Commands

Refill the facts layer from the ops stack (2-source each):

    ./bin/facts/extract.go                # write facts into var/kb.lbug (root=facts)
    ./bin/facts/extract.go --dry-run      # print proposed facts, write nothing
    ./bin/facts/extract.go --dry-run --json

Promote existing confirmed info leafs that already carry ≥2 independent sources
(evidence-first, idempotent):

    ./bin/facts/promote.go                # write promotions into var/kb.lbug
    ./bin/facts/promote.go --dry-run      # count candidates, write nothing
    ./bin/facts/promote.go --db <path>    # target a specific kb.lbug

`promote.go` scans confirmed `info` leafs, counts sources in the `source`
string (facts/extract convention: sources joined by `" x "`, e.g.
`docker ps x compose:compose.yaml`), and re-roots to `facts` only those with
≥2 distinct sources. Single-source and non-confirmed leafs stay in `info`.
Idempotent: a promoted leaf is no longer `root=info`, so a re-run is a no-op
and never duplicates.

Both tools write only the brain database (under `var/`), never the sources.

## Live-DB runbook (write access as the DB owner)

The running `brain-serve` container holds `var/kb.lbug` and bind-mounts it
read-write; the file is owned by the container uid (1001). Writes must run as
that owner with the brain **stopped** (double-open of the same file kills
Ladybug, and an in-service `/ingest` bulk write can hang the C layer — #181):

    scripts/stack/serve-brain --stop        # stop the serving container
    ./bin/facts/extract.go                  # ingest 2-source facts (root=facts)
    ./bin/facts/promote.go --db var/kb.lbug # promote qualifying info leafs
    scripts/stack/serve-brain               # restart serving

`KB_BUFFER_POOL` may be needed for large DBs (see docs/brain/rebuild.md).
Verify:

    bin-build/brain-stats                   # by_root: facts >= 1, info
    bin-build/brain-search "..." --root facts   # only confirmed evidence
    bin-build/brain-get <id>                # root: facts|info

## Read path

`search`/`get`/`stats`/`audit` already distinguish facts vs info by the `root`
column: `search --root facts` returns only confirmed evidence-linked answers,
`stats by_root` splits the two roots, `audit` groups by `root × confidence`.
No change needed after ingestion — the distinction is data-driven.
