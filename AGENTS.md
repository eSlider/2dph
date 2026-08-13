# AGENTS.md — 2dph (deductionphile)

Evidence-first brain over the ops/eSlider stack. Facts need proof or they are
`(not confirmed)`.

Read first: [PLAN](PLAN.md) → [docs](docs/).

## Method (detective, no fork)

> ≥2 independent sources of evidence, or the finding is `(not confirmed)`.
> Link the lexicon yaml path that backs each claim.

Independent means **different kind and different origin**, not two strings:
`runtime | declared | netconfig | doc | vcs | external` × the system of record
it came from. Two compose files are one kind and prove nothing together. Every
observation needs a locator (`README.md:89`) so the claim can be re-checked.
Enforced in `bin/tools/factsrules.py`, re-checked by `bin/facts/audit db` over
`(Evidence)-[:SUPPORTS]->(Leaf)`.

- `facts` root = assertions backed by ≥2 independent sources (docker ps ×
  compose × ssh-config × docs).
- `info` root = descriptive/narrative leafs, searchable, never asserted as fact.
- Search is deduction: `facts` → `info` → `web-search` (second independent
  source). An answer is `confirmed` only if it comes off the facts root.

## Hard rules

1. **Secrets.** `~/.config/brain/`, `.env`, `.secrets/` never read into context,
   printed, or committed. The OnlyOffice password is obtained via the
   tunnel, never written to this repo.
2. **Read-only data sources.** Ladybug `var/kb.lbug` and Postgres are opened
   read-only for queries. Index rebuilds write to `var/` (gitignored).
3. **PII.** `brain-test`, `cs_brain` client data is never read or quoted.
4. **No main pushes.** Feature branches + PR via `gh`; CI must be green.
5. **TDD.** Failing test before tool code. Unit tests run offline against
   fixtures; network/db calls are wrapped.
6. **docs reflect behaviour.** Any change updates `docs/` + `PLAN.md` status.

## Layout

```
PLAN.md       decisions + execution + open questions
docs/         published docs
skills/       in-project agent skills (vendored, no external links)
bin/          self-describing tools bin/{subject}/{method} (shebang)
bin/serve.go  async Go HTTP server entry (self-executing go run shebang)
bin/watch/    corpus watcher Go package (mtimes, no inotify deps)
bin/server/   async Go HTTP server (goroutines, bounded worker pool)
bin/mail/     mail pipeline: sync (Go), import (md), index_mail (rebuild)
bin/tools/    vendored python libs behind bin/* (kblib, yamlout, websearch)
bin/docker-entrypoint  container entrypoint (brain index|search|serve|watch)
compose.yaml  docker composition (root level, not docker/)
Dockerfile    multi-stage: python deps + static Go binaries
var/          kb.lbug, var/mail/*, caches (gitignored)
.venv/        ladybug + model2vec + mistune
```

## Mail pipeline

```bash
bin/mail/sync.go --source onlyoffice,gmail --workers 8 --out var/mail   # raw message.json + attachments
bin/mail/import --from-raw var/mail                                     # message.json → message.md (convert only)
bin/mail/index_mail                                                     # rebuild brain incl. all mail (fresh DB)
```

- `sync` (Go) downloads messages + attachments; Gmail uses paginated list +
  `body.attachmentId` (not partId) for attachments.
- `import` converts body + attachments to markdown. PDFs use poppler
  `pdftotext -layout` fast path (~15ms); textless/scanned PDFs fall back to
  docling (isolated subprocess — its native onnx can segfault the parent).
  Conversion never touches the brain DB (crash safety).
- `index_mail` always rebuilds from scratch (repo corpus + mail). Ladybug
  corrupts its WAL when brand-new leafs are bulk-inserted while FTS/vector
  indexes exist; a fresh DB with indexes created last is the only safe path.
  Keep conversion + indexing separate so a conversion crash can't leave the
  DB mid-transaction.

## Tools

```bash
bin/facts/audit ["self"|"db"]                     # repo invariants | evidence over kb.lbug
bin/facts/crm [--dry-run]                         # proof person↔company/company↔project (ooCRM × corpus SoT)
bin/kb/search "query" [--hop N] [--repo X]        # deduction search → YAML
bin/kb/stats                                      # what the graph holds → YAML
```

Never start a shell command with `cd` — use the tool working-directory
parameter. Search before reading whole files.

## Communication

Same tone as the corpus: plain, lists, no hype. Sign-off `Andriy Oblivantsev`.
German C1 where useful. Caveman only for agent chat, never in committed docs.