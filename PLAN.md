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
| D2 | skill merging | integrate skills **in this project** `skills/`; skip gitea / brain-dependent skills. |
| D3 | web search | import `web-search`, retire local `searxng-ops`. Vendored here, no remote link. |
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
| D14 | tooling style | `bin/{subject}/{method}.go` shebang (e.g. `bin/brain/search.go`). Shared code in `internal/`. One root `go.mod` + `go.work`. No `bin/*/main.go`, no nested modules. |
| D15 | repo | Gitea [`eSlider/2dph`](https://git.produktor.io/eSlider/2dph) is origin + [issues](https://git.produktor.io/eSlider/2dph/issues). GitHub `eSlider/2dph` is the public clone (PRs + Actions CI). No direct `main` pushes. TDD → PR → CI green → merge. |
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
    web/search               (vendored)
    db/psql-yq               (vendored)
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
- OQ2: OCR pipeline for pdfs/images/docs — mostly solved: poppler pdftotext
  fast-path for born-digital PDFs, docling fallback for the ~5% textless ones.
- OQ3: optional duckdb-md layer for `SELECT … FORMAT MARKDOWN` export/write-back.
- OQ4: YAML-first storage for leafs — deferred: JSON is ~10x faster to
  serialize and unambiguous; YAML only where humans edit files.

## Mail pipeline (done)

1. `bin/mail/sync.go` (Go, 8 workers) — paginated Gmail/OnlyOffice download.
   Gmail attachments key off `body.attachmentId`, not MIME `partId`.
2. `bin/mail/import --from-raw` — message.json → message.md; PDFs via
   `pdftotext -layout` (~15ms) with docling subprocess fallback; ICS sidecars
   Latin-1→UTF-8 normalized.
3. `bin/mail/index_mail` — fresh rebuild (repo corpus + mail) because ladybug
   corrupts its WAL on bulk-insert into an already-indexed DB. Conversion and
   indexing stay separate for crash safety.
4. Result: 17,835 messages → 28,918 info leafs, FTS + HNSW healthy, searchable
   via `bin/kb/search`.

## CI/CD pipeline (D15)

`.github/workflows/ci.yml`:

1. go vet + go test ./... (Go tools; root module)
2. `go test ./rank` in `bin/kbsearch` (cgo-free ranking + flag parser; nested module still needs ladybug for the rest)
3. `go test ./...` in `bin/chats` (Telegram + LinkedIn parsers; nested module)
4. python -m unittest discover (Py tools)
5. bin/facts/audit self  (lexicon internal consistency)
6. bin/kb/eval            (recall@5 ≥ 0.95, gates index regressions)
7. md-docs build/lint if docs tooling arrives.

Feedback loop: every commit → PR → CI → green/gate → merge. Same discipline as
`db/tech-poc`: contract first where there is an OpenAPI/message shape.

## Execution order

1. scaffold repo (:done after this file + AGENTS.md + .gitignore + ci)
2. gh repo create eSlider/2dph --private + initial commit + CI
3. vendored skill integration (web-search, db-yaml, kb-search, agent-cost, diataxis-docs) — no remote links
4. .venv: ladybug + model2vec + mistune
5. schema + tools with TDD (kb + md + facts + brain)
6. ~/.config/brain config
7. corpus extraction (facts/info)
8. verify: web-search smoke, onlyoffice pg, md-db round-trip, eval, audit