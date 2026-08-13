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
| D3 | web search | Go client `bin/web/search.go` (`internal/websearch`). SearXNG URL is config (`BRAIN_SEARCH_URL`). Optional Compose profile `searxng` (sanitized settings). Do not run a second copy on a host that already has one. Empty/`throttled` ≠ “nothing exists”. |
| D4 | embeddings | **model2vec** `minishlab/potion-multilingual-128M` instead of embeddinggemma. |
| D5 | parser | **mistune** for MD → leaf extraction (duckdb-md documented as future optional SQL/export layer, not v1). |
| D6 | graph engine | **LadybugDB**. Go is the service (`bin/brain/search.go`, `bin/brain/serve.go` in-process, `internal/brain`). Read path (`get.go` / `stats.go` / `eval.go`) is Go + cgo. Python `bin/kb/{get,stats,eval}` is the CI fallback (GitHub runners have no ladybug cgo). Index/write stays Python until the Go write path is safe. |
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
| D17 | assertion gate | Fact-check every *claim* (facts → info → live → web), not every edit. `bin/brain/search.go` adds a `web` block when there is no facts hit (`throttled`/`skipped`/`refused` ≠ absence). `--root` and `--no-web` stay local. Missing graph ≠ “does not exist”. |
| D18 | reasoner | Pluggable OpenAI-compatible URL. RAM: Qwen3.5-9B. Quality: Bonsai-27B or Qwen3.6-27B. No official Qwen3.6-9B. |
| D19 | git history | [go-git](https://github.com/go-git/go-git) via `bin/git/import.go`. No subprocess of the git binary. Conversion prints commit leafs; brain write is `bin/brain/index.go`. |

## Architecture

```
2dph/
  PLAN.md / AGENTS.md
  docs/                     published docs (this conversation → docs/ as md)
  skills/                   in-project skills (web-search, db-yaml, brain, diataxis-docs)
  bin/
    facts/extract           auto-pair 2 sources → lexicon yaml + graph
    facts/audit             ["self"|"facts"|"info"|"stale"] 2-source + staleness gate
    kb/index                Python write path (called by bin/brain/index.go)
    brain/index.go          rebuild FTS + HNSW (incl. --with-mail)
    brain/get.go stats.go eval.go  # Go read (cgo); Python bin/kb/* CI fallback
    brain/watch.go
    brain/search.go         deduction: facts → info → web-search
    brain/serve.go          HTTP API in-process (internal/httpapi + internal/brain)
    mail/import.go          JSON → markdown (no brain write)
    markdown/import.go      mistune leaves
    postgres/query.go       read-only YAML (wraps bin/db/psql-yq)
    git/import.go           go-git history (no git binary; conversion only)
    web/search.go           SearXNG client (throttled ≠ absence)
    chats/sync.go import.go facts.go apply.go
                            (libs in internal/chats; no chats index)
    md/import               (deprecated; bin/markdown/import.go)
    brain/extract  brain/audit   brain/deduce    (thinking wrapper)
    web/search               (deprecated shim → web/search.go)
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
2. `bin/mail/import.go --from-raw` — message.json → message.md; PDFs via
   `pdftotext -layout` (~15ms) with docling subprocess fallback; ICS sidecars
   Latin-1→UTF-8 normalized.
3. `bin/brain/index.go --rebuild` — fresh rebuild (repo corpus + mail) because ladybug
   corrupts its WAL on bulk-insert into an already-indexed DB. Conversion and
   indexing stay separate for crash safety. `bin/mail/index_mail` is a
   deprecation shim.
4. Result: 17,835 messages → 28,918 info leafs, FTS + HNSW healthy, searchable
   via `bin/brain/search.go`.

## CI/CD pipeline (D15)

`.github/workflows/ci.yml`:

1. go vet + go test ./... (root module; packages without ladybug cgo)
2. `go test ./internal/brain/rank` (cgo-free ranking + flag parser)
3. python -m unittest discover -s bin/tools (includes published-docs SoT)
4. bin/facts/audit self  (lexicon internal consistency)
5. `bin/kb/eval` (recall@5 ≥ 0.95). Local SoT is `bin/brain/eval.go`; CI uses
   the Python twin until the runner has ladybug cgo. Questions live in
   `internal/brain/rank`.
6. md-docs build/lint if docs tooling arrives.

Feedback loop: every commit → PR → CI → green/gate → merge. Same discipline as
`db/tech-poc`: contract first where there is an OpenAPI/message shape.

## Execution order

1. scaffold repo (:done after this file + AGENTS.md + .gitignore + ci)
2. gh repo create eSlider/2dph --private + initial commit + CI
3. vendored skill integration (web-search, db-yaml, brain, diataxis-docs) — no remote links
4. .venv: ladybug + model2vec + mistune
5. schema + tools with TDD (kb + md + facts + brain)
6. ~/.config/brain config
7. corpus extraction (facts/info)
8. verify: web-search smoke, onlyoffice pg, md-db round-trip, eval, audit