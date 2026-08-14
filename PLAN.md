# PLAN — 2dph (deductionphile)

A brain that loves facts and deduction. Evidence-first knowledge graph + hybrid
RAG over the operational Brain/ops/eSlider stack. Built like Sherlock
Holmes: nothing is asserted unless it has proof.

Status: **v1 in** (epic [#16](https://git.produktor.io/eSlider/2dph/issues/16) closed).
v2 board: milestone [v2](https://git.produktor.io/eSlider/2dph/milestone/13) —
OCR [#6](https://git.produktor.io/eSlider/2dph/issues/6) in,
[#29](https://git.produktor.io/eSlider/2dph/issues/29) OQ1 in,
[#30](https://git.produktor.io/eSlider/2dph/issues/30) OQ3 in.
Gap: [docs/roadmap.md](docs/roadmap.md).

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
| D6 | graph engine | **LadybugDB**. Go is the service (`bin/brain/search.go`, `bin/brain/serve.go` in-process, `internal/brain`). Read path is Go + Zig CGO (D21). Python `bin/kb/{get,stats,eval}` is the CI fallback when Zig/libs are not fetched. Incremental write is Python `bin/kb/add` (`bin/brain/add.go`). Bulk rebuild stays `compose --profile index` until the Go write path is safe. |
| D7 | db access | `db-yaml`/`psql-yq`-style, read-only, YAML out. OnlyOffice Postgres via SSH tunnel (`127.0.0.1:5433`). |
| D8 | evidence | detective method: ≥2 independent sources or `(not confirmed)`. 2-source auto-pair docker ps × compose × ssh-config × docs. |
| D9 | facts/goal model | Who / What / How / Where / When + evidence + confidence on every edge. |
| D10 | versioning | everything is a leaf with `sha256 + observed_at + source_rev`; `File-[:HAS_VERSION]->Commit-[:AUTHORED]->Person`. Stale = `source_rev` < git HEAD. |
| D11 | strong/weak | `root` column: `facts` (strong) vs `info` (weak). Answer is `confirmed` only from facts root. |
| D12 | transactional | facts and info split by root but **written in the same Ladybug transaction (ACID)** on every write. |
| D13 | portfolio | start graph `(Person:eslider)-[:HAS]->(Portfolio)`, associate other natural/juristic persons later. |
| D14 | tooling style | `bin/{subject}/{method}.go` shebang (e.g. `bin/brain/search.go`). Shared code in `internal/`. One root `go.mod` + `go.work`. No `bin/*/main.go`, no nested modules. |
| D15 | repo | Gitea [`eSlider/2dph`](https://git.produktor.io/eSlider/2dph) is origin + [issues](https://git.produktor.io/eSlider/2dph/issues). GitHub `eSlider/2dph` is the public clone (PRs + Actions CI). No direct `main` pushes. TDD → PR → CI green → merge. |
| D16 | contradictions | ≥2 yes vs ≥2 no → hypothesis → `(not confirmed)` until a rule fires. Order: **temporal_freshness** (fresh ≥2 vs stale minority), then **authority_pairing** (runtime/config A×B beats narrative C). Store as `a x b vs c x d` on hypothesis leafs. `bin/facts/audit contradict`. [#29](https://git.produktor.io/eSlider/2dph/issues/29). |
| D17 | assertion gate | Fact-check every *claim* (facts → info → live → web), not every edit. `bin/brain/search.go` adds a `web` block when there is no facts hit (`throttled`/`skipped`/`refused` ≠ absence). `--root` and `--no-web` stay local. Missing graph ≠ “does not exist”. |
| D18 | reasoner | Pluggable OpenAI-compatible URL (`REASONER_BASE_URL`). RAM: `Qwen/Qwen3.5-9B`. Quality: `prism-ml/Bonsai-27B-gguf` or `Qwen/Qwen3.6-27B`. No official Qwen3.6-9B. CPU bake-off: `bin/reasoner/bakeoff.go` + compose profile `reasoner` (`OLLAMA_NUM_GPU=0`, `:11435`). PicoClaw is compose profile `picoclaw`; tools are `search`/`get`/`audit`. Weights are not copied into the 2dph image. Agent lever/loop: [#15](https://git.produktor.io/eSlider/2dph/issues/15). |
| D19 | git history | [go-git](https://github.com/go-git/go-git) via `bin/git/import.go`. No subprocess of the git binary. Conversion prints commit leafs; brain write is `bin/brain/index.go`. |
| D20 | agent API | OpenAPI + MCP are generated from the same `internal/httpapi.Ops` table as `bin/brain/serve.go` handlers. `GET /openapi.json`, `POST /mcp` (JSON-RPC tools/list + tools/call). Tool names match OpenAPI paths (`search`/`get`/`stats`/`audit`/`ingest`). |
| D21 | CGO | Ladybug/tokenizers CGO is compiled with **Zig** (`bin/cgo/zcc` → `zig cc -target …-linux-gnu`), not gcc. `bin/cgo/zig` pins Zig 0.14.1 + liblbug 0.19.1 + libtokenizers 1.27.0. Compose `target: api` has no CPython; write/rebuild is profile `index`. |
| D22 | analytics | **duckdb-go** in-process (`internal/duckstats`, `bin/qa/stats.go`) for quantiles/JSONL. Links with **gcc/g++**, not Zig. Ladybug stays the graph; web-search cache stays modernc sqlite. Slice small structured docs with **mikefarah/yq**, not kislyuk/jq. [#30](https://git.produktor.io/eSlider/2dph/issues/30). |

## Architecture

```
2dph/
  PLAN.md / AGENTS.md
  docs/                     published docs (this conversation → docs/ as md)
  skills/                   in-project skills (web-search, postgres, brain, picoclaw, diataxis-docs)
  bin/
    facts/extract.go audit.go crm.go  # D14 shebang; Python implementation
    kb/index                Python bulk write (called by bin/brain/index.go)
    kb/add                  Python incremental write (called by bin/brain/add.go)
    brain/index.go          rebuild FTS + HNSW (incl. --with-mail)
    brain/add.go            incremental leaf write (no rebuild)
    brain/get.go stats.go eval.go  # Go read (cgo); Python bin/kb/* CI fallback
    brain/watch.go
    brain/search.go         deduction: facts → info → web-search
    brain/serve.go          HTTP API in-process + OpenAPI/MCP (D20); Zig CGO (D21)
    cgo/zig zcc zc++        CGO toolchain (zig cc, not gcc)
    mail/import.go          JSON → markdown (no brain write)
    markdown/import.go      H2 leaf split (Go); Python bin/md/import fallback
    postgres/query.go       read-only YAML (wraps bin/db/psql-yq)
    git/import.go           go-git history (no git binary; conversion only)
    web/search.go           SearXNG client (throttled ≠ absence)
    reasoner/bakeoff.go     CPU tool-call bake-off (D18; OpenAI tools)
    chats/sync.go import.go facts.go apply.go
                            (libs in internal/chats; no chats index)
    mail/ocr.go             tesseract eng+deu (pdftoppm scans)
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
Edges: `RUNS / USES / FROM_FILE / HAS_VERSION / AUTHORED / ABOUT / ASSOCIATED / SIMILAR_0.85`.
`FROM_FILE` / `HAS_VERSION` / `AUTHORED`: `bin/brain/search.go --hop N` walks
them from each hit (1=File, 2=Commit, 3=Person). Rebuild writes
`Leaf-[:FROM_FILE]->File`; git import writes the rest.

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
- YAML default output, `--json` for machines. Slice with mikefarah/yq.
- Everything that touches the network / DB is read-only, throttled, cached.
- Tests (TDD) gate every commit; `gh` + CI/CD on every push.

## Open questions (v2)

- OQ1: **in** — D16 adjudication: `temporal_freshness` then `authority_pairing`.
  Unresolved 2v2 stays hypothesis. [#29](https://git.produktor.io/eSlider/2dph/issues/29).
- OQ2: OCR — **in**. `pdftotext -layout` first; scans `pdftoppm` + tesseract
  `eng+deu` (`bin/mail/ocr.go`, `internal/ocr`). No gocv, no gosseract CGO
  (D21 Zig owns Ladybug CGO). Optional `OCR_ENGINE=paddle` / compose profile
  `ocr-paddle`. Docling left the default path. [#6](https://git.produktor.io/eSlider/2dph/issues/6).
- OQ3: **in** — duckdb-go (`internal/duckstats`, `bin/qa/stats.go`) for
  quantiles / JSONL count. Not a second graph. [#30](https://git.produktor.io/eSlider/2dph/issues/30).
- OQ4: YAML-first storage for leafs — deferred: JSON is ~10x faster to
  serialize and unambiguous; YAML only where humans edit files.

## Mail pipeline (done)

1. `bin/mail/sync.go` (Go, 8 workers) — paginated Gmail/OnlyOffice download.
   Gmail attachments key off `body.attachmentId`, not MIME `partId`.
2. `bin/mail/import.go --from-raw` — message.json → message.md; PDFs via
   `pdftotext -layout` (~15ms); textless/scanned PDFs `pdftoppm` + tesseract
   `eng+deu`. ICS sidecars
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
4. `bin/facts/audit self` (lexicon internal consistency; `bin/facts/audit.go` is the D14 wrapper)
5. `bin/brain/eval.go` via Zig (recall@5 ≥ 0.95). Python `bin/kb/eval` is an
   explicit fallback, not the CI SoT.
6. `bin/cgo/zig go build -tags system_ladybug` (compile search with zig cc; fetches pinned zig+libs).

Feedback loop: every commit → PR → CI → green/gate → merge. Same discipline as
`db/tech-poc`: contract first where there is an OpenAPI/message shape.

## Execution order

1. scaffold repo (:done after this file + AGENTS.md + .gitignore + ci)
2. gh repo create eSlider/2dph --private + initial commit + CI
3. vendored skill integration (web-search, postgres, brain, diataxis-docs) — no remote links
4. .venv: ladybug + model2vec + mistune
5. schema + tools with TDD (kb + md + facts + brain)
6. ~/.config/brain config
7. corpus extraction (facts/info) — **in**: [#18](https://git.produktor.io/eSlider/2dph/issues/18)
8. verify: web-search smoke, onlyoffice pg, md-db round-trip, eval, audit

## Gap to v1 (epic #16)

Remaining: none for epic #16 (v1). Board:
[epic #16](https://git.produktor.io/eSlider/2dph/issues/16),
milestone [v1 detective brain](https://git.produktor.io/eSlider/2dph/milestone/12).
Narrative: [docs/roadmap.md](docs/roadmap.md).

| Order | Issue | Gap |
|-------|-------|-----|
| 1 | [#14](https://git.produktor.io/eSlider/2dph/issues/14) | **in** — `bin/brain/add.go` / `POST /ingest` write facts+info without deleting `kb.lbug`. Bulk corpus still `--rebuild`. Leftover Python (mail/facts) is not the living-graph blocker. |
| 2 | [#17](https://git.produktor.io/eSlider/2dph/issues/17) | **in** — `--hop N` walks `FROM_FILE` → `HAS_VERSION` → `AUTHORED` (max 3). |
| 3 | [#18](https://git.produktor.io/eSlider/2dph/issues/18) | **in** — `--with-facts` / `--facts-json` land `root=facts`; `--with-chats` indexes `var/chats/md`. WhatsApp sync is out of v1. |
| 4 | [#15](https://git.produktor.io/eSlider/2dph/issues/15) | **in** — lever/loop documented (`search` → `get` → `audit`). |
| 5 | [#19](https://git.produktor.io/eSlider/2dph/issues/19) | **in** — CI recall SoT is `bin/brain/eval.go` via Zig. Python `bin/kb/eval` stays as an explicit fallback. |

Does **not** block epic close: OQ4. OCR [#6](https://git.produktor.io/eSlider/2dph/issues/6), OQ1 [#29](https://git.produktor.io/eSlider/2dph/issues/29), OQ3 [#30](https://git.produktor.io/eSlider/2dph/issues/30) are **in**.