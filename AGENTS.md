# AGENTS.md — 2dph (deductionphile)

Evidence-first brain over the ops/eSlider stack. Facts need proof or they are
`(not confirmed)`.

Read first: [PLAN](PLAN.md) → [docs](docs/).

## Method (detective, no fork)

> ≥2 independent sources of evidence, or the finding is `(not confirmed)`.
> Link the lexicon yaml path that backs each claim.

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
skills/       in-project agent skills (this is the integration target)
bin/          self-describing tools bin/{subject}/{method}
var/          kb.lbug, caches (gitignored)
.venv/        ladybug + model2vec + mistune
```

## Tools

```bash
bin/facts/audit ["self"|"facts"|"info"|"stale"]   # 2-source + staleness gate
bin/kb/search "query" [--hop N] [--repo X]        # deduction search → YAML
bin/md/tables                                     # what the graph holds → YAML
bin/brain/deduce "question"                       # thinking wrapper
```

Never start a shell command with `cd` — use the tool working-directory
parameter. Search before reading whole files.

## Communication

Same tone as the corpus: plain, lists, no hype. Sign-off `Andriy Oblivantsev`.
German C1 where useful. Caveman only for agent chat, never in committed docs.