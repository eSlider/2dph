# PLAN — 2dph (deductionphile)

A brain that loves facts and deduction. Evidence-first knowledge graph + hybrid
RAG over the operational Brain/ops/eSlider stack. Built like Sherlock
Holmes: nothing is asserted unless it has proof.

Status: **in progress** — this file is the plan and the record of decisions.

## What

A single embedded store (LadybugDB, one `.lbug` file) that holds:

- a **property graph** (Cypher) over the ops corpus and portfolio,
- **FTS5** (BM25) and **HNSW vector** indexes (model2vec embeddings),
- every node/edge annotated with `root` (`facts` | `info`), `confidence`
  (`confirmed` | `partial` | `hypothesis`), `evidence[]`, `how`, `where`, `when`.

Search = **deduction**: facts root first, info root second, `web-search` as the
second independent source when local roots cannot confirm. Following the
detective method: **a fact needs ≥2 independent sources or it is
`(not confirmed)`.**

## Decisions (recorded)

| # | Question | Answer |
|---|----------|--------|
| D1 | RAG corpus | ops stack (chat, onlyoffice, gitea/NPM, searchxng, observability, ai-bot, mcp-servers, `~/.ssh/config`) + portfolio. Exclude `office.dev` + jobs/applications. |
| D2 | skill merging | integrate skills **in this project** `skills/`; skip gitea / brain-detective-depe ndent skills. |
| D3 | web search | import `web-search` (agent-skills), retire local `searxng-ops`. |
| D4 | embeddings | **model2vec** `minishlab/potion-multilingual-128M` instead of embeddinggemma. |
| D5 | parser | **mistune** for MD → leaf extraction (duckdb-md documented as future optional SQL/export layer, not v1). |
| D6 | graph engine | **LadybugDB** (Kuzu successor, MIT, embedded, native FTS+vector+Cypher). Python binding for `bin/*`; Go shebang for golang tools. |
| D7 | db access | `db-yaml`/`psql-yq`-style, read-only, YAML out. OnlyOffice Postgres via SSH tunnel (`127.0.0.1:5433`). |
| D8 | evidence | detective method: ≥2 independent sources or `(not confirmed)`. Auto-pair docker ps × compose × ssh-config × docs. |
| D9 | facts/goal model | Who / What / How / Where / When + evidence + confidence on every edge. |
| D10 | versioning | everything is a leaf with `sha256 + observed_at + source_rev`; `File-[:HAS_VERSION]->Commit-[:AUTHORED]->Person`. Stale = `source_rev` < git HEAD. |
| D11 | strong/weak | `root` column: `facts` (strong) vs `info` (weak). Answer is `confirmed` only from facts root. |
| D12 | transactional | facts and info split by root but **written in the same Ladybug transaction (ACID)** on every write. |
| D13 | portfolio | start graph `(Person:eslider)-[:HAS]->(Portfolio)`, associate other natural/juristic persons later. |
| D14 | tooling style | `bin/{subject}/{method}` self-describing: shebang line 1, usage comment from line 2. Go shebang: `///usr/bin/env go run "$0" "$@"; exit`. |
| D15 | repo | GitHub `eSlider/2dph`, private, push/commit via `gh`, TDD + commit every change, CI/CD. |
| D16 | contradictions | ≥2 yes vs ≥2 no → unrelated sources conflict → hypothesis → `(not confirmed)`. Resolution (authority, staleness adjudication) = **v2**, tracked as open question. |

## Architecture

```
2dph/
  PLAN.md / AGENTS.md
  docs/                     published docs (this conversation → docs/ as md)
  skills/                   in-project skills (web-search, db-yaml, kb-search, agent-cost, diataxis-docs, …)
  bin/
    facts/extract           auto-pair 2 sources → lexicon yaml + graph
    facts/audit             ["self"|"facts"|"info"|"stale"] 2-source + staleness gate
    kb/index                build FTS + HNSW from corpus
    kb/search               deduction: facts → info → web-search; --hop N
    kb/get  kb/stats  kb/eval
    md/import  md/select  md/tables  md/gaps     (mistune)
    brain/extract  brain/audit   brain/deduce    (thinking wrapper)
    web/search               (symlink into agent-skills web-search)
    db/psql-yq               (symlink into agent-skills)
    ssh-tunnel               onlyoffice pg tunnel 5433
  var/kb.lbug               single embedded store (gitignored)
  .venv/                    ladybug + model2vec + mistune + numpy
```

## Schema (first pass)

Node tables: `Person, Service, Host, Container, Repo, File, Commit, Leaf`.
`Leaf(embedding FLOAT[N])` — FTS on `text`, HNSW vector index on `embedding`.
Edges: `RUNS / USES / HAS_VERSION / AUTHORED / ABOUT / ASSOCIATED / SIMILAR_0.85`.

Common props on every node/edge: `root`, `confidence`, `evidence[]`, `how`,
`where`, `when`, `source_rev`.

## Config

`~/.config/brain/` (0600):

- `search.env` — real `BRAIN_SEARCH_URL/USER/PASS` from `~/.config/ops/npm-bot.env`
- `db-profiles.yml` — real `onlyoffice` profile (SSH tunnel `127.0.0.1:5433`,
  user `onlyoffice`, db `onlyoffice`, `password_env_file`) + example profiles.
- `~/.config/brain/../` — nothing else lives in the repo.

## Tooling conventions

- `bin/{subject}/{method}` — line 2 is a usage comment (mirrors `psql-yq`).
- bash + python primary; golang via Go shebang when a compiled helper is right.
- YAML default output, `--json` for machines. Slice with `yq`.
- Everything that touches the network / DB is read-only, throttled, cached.
- Tests (TDD) gate every commit; `gh` + CI/CD on every push.

## Open questions (v2)

- OQ1: mutually-contradicting evidence — how to resolve (authority weighting,
  temporal freshness, audit adjudication).
- OQ2: OCR pipeline for pdfs/images/docs (late phase).
- OQ3: optional duckdb-md layer for `SELECT … FORMAT MARKDOWN` export/write-back.

## CI/CD pipeline (D15)

`.github/workflows/ci.yml`:

1. go vet + go test ./... (Go tools)
2. python -m unittest discover + pytest (Py tools)
3. bin/facts/audit self  (lexicon internal consistency)
4. bin/kb/eval            (recall@5 ≥ 0.95, gates index regressions)
5. md-docs build/lint if docs tooling arrives.

Feedback loop: every commit → PR → CI → green/gate → merge. Same discipline as
`db/tech-poc`: contract first where there is an OpenAPI/message shape.

## Execution order

1. scaffold repo (:done after this file + AGENTS.md + .gitignore + ci)
2. gh repo create eSlider/2dph --private + initial commit + CI
3. agent-skills clone + skill integration (web-search, db-yaml, kb-search, agent-cost, diataxis-docs)
4. .venv: ladybug + model2vec + mistune
5. schema + tools with TDD (kb + md + facts + brain)
6. ~/.config/brain config
7. corpus extraction (facts/info)
8. verify: web-search smoke, onlyoffice pg, md-db round-trip, eval, audit