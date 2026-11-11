# PLAN — 2dph (deductionphile)

A brain that loves facts and deduction. Evidence-first knowledge graph + hybrid
RAG over the operational Brain/ops/eSlider stack. Built like Sherlock
Holmes: nothing is asserted unless it has proof.

Status: **v1 in** (epic [#16](https://git.produktor.io/eSlider/2dph/issues/16) closed).
v2 board: milestone [v2](https://git.produktor.io/eSlider/2dph/milestone/13) —
OCR [#6](https://git.produktor.io/eSlider/2dph/issues/6) in,
[#29](https://git.produktor.io/eSlider/2dph/issues/29) OQ1 in,
[#30](https://git.produktor.io/eSlider/2dph/issues/30) OQ3 in,
[#34](https://git.produktor.io/eSlider/2dph/issues/34) D23 in,
[#36](https://git.produktor.io/eSlider/2dph/issues/36) OQ5/D24 in,
[#102](https://git.produktor.io/eSlider/2dph/issues/102) gs PDF normalize in,
[#163](https://git.produktor.io/eSlider/2dph/issues/163) browser-sync in
(`bin/cron/browser-sync.go` + 6-hourly systemd timer, reads
`var/corpus/{gmail,linkedin,djinni}` → POST /ingest; Thorium down tolerated),
[#82](https://git.produktor.io/eSlider/2dph/issues/82) Synapse Matrix (leafs+edges
as a service for pc-agent) in — `bin/brain/synapse-matrix.go`, docs
[docs/brain-synapse.md](docs/brain-synapse.md).
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
| D18 | reasoner | Pluggable OpenAI-compatible URL (`REASONER_BASE_URL`). RAM: `Qwen/Qwen3.5-9B`. Quality: `prism-ml/Bonsai-27B-gguf` or `Qwen/Qwen3.6-27B`. No official Qwen3.6-9B. CPU bake-off: `bin/reasoner/bench.go` + compose profile `reasoner` (`OLLAMA_NUM_GPU=0`, `:11435`). PicoClaw is compose profile `picoclaw`; tools are `search`/`get`/`audit`. Weights are not copied into the 2dph image. Agent lever/loop: [#15](https://git.produktor.io/eSlider/2dph/issues/15). |
| D19 | git history | [go-git](https://github.com/go-git/go-git) via `bin/brain/import-git.go` (lib `internal/gitlog`). No subprocess of the git binary. Reads history and upserts commit leafs into the brain directly (`--dry-run` to preview). |
| D20 | agent API | OpenAPI + MCP are generated from the same `pkg/httpapi.Ops` table as `bin/brain/serve.go` handlers. `GET /openapi.json`, `POST /mcp` (JSON-RPC tools/list + tools/call). Tool names match OpenAPI paths (`search`/`get`/`stats`/`audit`/`ingest`). |
| D21 | CGO | Ladybug/tokenizers CGO is compiled with **Zig** (`bin/cgo/zcc` → `zig cc -target …-linux-gnu`), not gcc. `bin/cgo/zig` pins Zig 0.14.1 + liblbug 0.19.1 + libtokenizers 1.27.0. Compose `target: api` has no CPython; write/rebuild is profile `index`. |
| D22 | analytics | **duckdb-go** in-process (`pkg/duckdb`, `bin/jsonl/stats.go`) for quantiles/JSONL. Links with **gcc/g++**, not Zig. Ladybug stays the graph; web-search cache stays modernc sqlite. Slice small structured docs with **mikefarah/yq**, not kislyuk/jq. [#30](https://git.produktor.io/eSlider/2dph/issues/30). |
| D23 | CLI | **flaggy** (`github.com/integrii/flaggy`, 0 deps). Flags at any position. Wrapper `pkg/cli`. Bash complete: `source <(./bin/shell/complete.go bash)`. No cobra, no stdlib `flag` in Go tools. Search does not intercept the word `completion`. [#34](https://git.produktor.io/eSlider/2dph/issues/34). |
| D24 | fact intervals | Leaf `valid_from` / `valid_to` (YYYY-MM-DD, inclusive; empty = open/legacy). Search `--as-of` / MCP `as_of` keeps facts active that day. Not D16 `temporal_freshness` (source stale vs HEAD). Empty interval = always visible. [#36](https://git.produktor.io/eSlider/2dph/issues/36). |
| D25 | deploy data path | Brain serves from host `var/`, not named volumes. Compose binds `./var:/data/var` (kb.lbug at `/data/var/kb.lbug`), HF model from `var/hf`, Ladybug FTS/VECTOR extensions mounted read-only into `$HOME/.lbdb/extension`. `brain` uses `network_mode: host` so `127.0.0.1:8630` works with any image (KB_HOST-independent). Named volumes `kb-model`/`kb-var` dropped — live data is host `var/` (gitignored). |
| D26 | search A/B (#201/#202) | Nothing is "better" without an A/B. `bin/brain/bench.go` + golden-set (`internal/brain/testdata/golden-set.json`, ~50 queries, Russian+English, facts/mail/docs/git/ssh). Metrics: latency p50/p95/mean, recall@5/@10 (fragment gate like eval.go + A/B vs baseline IDs), CPU/RSS from /proc. Gates: recall@5 ≥ 0.95, candidate p50 ≤ baseline × 1.5. Baseline: linear scan 313k = p50 ~31s. Candidates (ANN/SIMD/FTS-first) — child issues of epic #201. |
| D32 | ANN outside liblbug (#201/#204) | Incremental vector index in `internal/brain/ann`: **IVF** (k-means + exact cosine over probed cells, pure Go; `coder/hnsw`/HNSW discarded — at 313k real vectors only 34% of nodes are reachable, recall@5 0.16). Index `var/state/vector.ann` + append-only WAL (`upsert` = only new leafs, no rebuild). `bin/brain/ann.go` (build/upsert/ensure/stats/api/search), wave step `ann-build` (#206). `queryVector`: index top-k → point lookup of metadata → merge with FTS (rank untouched); fallback to scan when missing/corrupt. Config `vector.ann` (NList=2000, NProbe=128 — package defaults; prod config nprobe=2000, decision #206). A/B: baseline p50 ~31s → candidate p50 ~0.4s, recall@5 vs baseline 1.0 (numbers in #204/#206). |
| D26 | repo layout | `bin/{subject}` = tools only (`{verb}-{object}.go`); executables/orchestration → `scripts/`; data tree `var/{tmp,dist,log,state,cache,hf,kb.lbug,corpus/{git,mail,chats,docs}}`; configs `etc/{subject}` + central `etc/brain/config.yml`. CI guard fails on absolute paths and Python remnants. [#89](https://git.produktor.io/eSlider/2dph/issues/89) |
| D27 | config system | All external sources (paths, URLs, endpoints) via `github.com/eslider/go-config` (`internal/config`): stack `etc/brain/config.yml` → `config.local.yml` → `.env` → process env; one load per process into a strict typed struct; deep merge (maps recurse, scalars last-write-wins, keys lower+alnum); no ad-hoc `os.Getenv`; legacy `KB_*` / `BRAIN_SEARCH_*` env names honored transitionally. [#90](https://git.produktor.io/eSlider/2dph/issues/90), [#91](https://git.produktor.io/eSlider/2dph/issues/91) |
| D28 | no hardcoded paths/URLs | Nothing like `/mnt/…`, `/home/<user>/…`, or pinned host URLs in code/comments/tests; missing config key = explicit error pointing at `etc/brain/config.yml`. CI-enforced. [#89](https://git.produktor.io/eSlider/2dph/issues/89) |
| D29 | typed code | Static typing first: no `map[string]any` at boundaries; loose→strict via go-viper/mapstructure/v2; primitives only in `pkg/utils/*.go`. Hotspots listed in [#92](https://git.produktor.io/eSlider/2dph/issues/92). |
| D30 | owned repos | All `github.com/eSlider/go-*` are owned: Gitea `git.produktor.io/eSlider/*` = working home (issues/releases only there), GitHub = push-mirror; module paths stay `github.com/eSlider/go-*`. Shared template: subpackage-per-domain. Gitea fetch via `replace` in `go.mod`: `go-config@v0.4.0`, `go-onlyoffice@v0.14.0` → `git.produktor.io/eSlider/*` (go.sum carries Gitea hashes only; zero `github.com/eSlider/go-*` downloads). [#101](https://git.produktor.io/eSlider/2dph/issues/101) |
| D31 | concurrency | Public APIs synchronous; ctx-first IO; bounded pools; wg-accounted goroutines; sender closes channels; errgroup for fan-out/fan-in; backpressure over unbounded queues; graceful shutdown; `go test -race` in CI. [#94](https://git.produktor.io/eSlider/2dph/issues/94) |
| D32 | zero-alloc hot paths | Pre-allocation, buffer reuse (`buf[:0]`), `sync.Pool` with reset, `strings.Builder` over `+`, sets as `map[K]struct{}`, structs over maps; proven by `-benchmem`. Applies to hot paths only — no premature optimization. |
| D33 | sync-ETL reimplementation | Pipeline `Source.Fetch(ctx,cursor)→[]Blob→Registry.Decode→Transform→Load`; one Handler per format (eml=emersion/go-message `Walk()`, zip=`archive/zip`, html=`x/net/html` optional, pdf=Ghostscript normalize→pdftotext/tesseract); lazy children, walker safety limits; single implementation per transformer; canonical `Conversation` model on disk (`var/corpus`) → brain graph (`SENT/TO/CC/BCC/REPLY_TO/PART_OF`); URL addressing `scheme://platform/path[#anchor]` with `sha256(URL)[:16]` node IDs. Epic [#88](https://git.produktor.io/eSlider/2dph/issues/88): [#95](https://git.produktor.io/eSlider/2dph/issues/95)–[#102](https://git.produktor.io/eSlider/2dph/issues/102). |
| D34 | synapse edges | User-defined edges between neurons modeled as a `SYNAPTIC (FROM Leaf TO Leaf, type STRING)` rel table (leafs = neurons, edges = synapses); adjacency = both directions; path = `[:SYNAPTIC* ALL SHORTEST 1..N]` (Ladybug exposes only intermediate nodes → rebuild `[from, mid…, to]`). Service `bin/brain/synapse-matrix.go`, auth = token OR loopback bind, config section `synapse`. [#82](https://git.produktor.io/eSlider/2dph/issues/82) |
| D35 | ANN in prod (#206) | The A/B winner (#204) is on by default: `vector.ann.enabled: true` (serve + CLI), **nprobe=2000** — measured on the live corpus of 313689 vectors (11 golden-set queries): p50 471ms recall@5=1.0 vs 256 (p50 262ms recall@5 0.818) — correctness first, tuning later. Wave: step `ann-build` = `ann.go ensure` — a full build only when the index is missing/stale (>10% lag by leaf counter), otherwise incremental WAL-upsert (+100 <1s, never a rebuild per wave). `serve` — warm start: the index loads at startup (WarmANN), if empty/broken — fallback to scan, the build is done by the wave (single-writer, #204). Prod A/B: p50 471ms vs baseline 33.6s (0.014x), recall@5 vs baseline 1.0. |
| D36 | FTS phase of rebuild (#244) | After the write phase `brain-index` closes the handle and reopens the DB with an auto-sized pool `max(1GB, chars×32)` for `CREATE_FTS_INDEX`: on the full corpus (280MB of text) the 1GB pool fails and leaves an orphan `0_id_appears_info`, unreachable via SQL (internal catalog). Threshold measurements: 10.3MB→64/128MB, 10.8MB→192/256MB, prod 280MB→1GB/10GB. The two-phase workaround from rebuild.md is automated (retired as manual). C ceiling (measurement #244): write phase ~140KB/leaf linear, the 1GB pool is not the limit, CHECKPOINT/embedding have no effect, close does not release in the same process; FTS — a pool of ~9GB on the full corpus. Criterion #237 (RSS ≤2-4GB) restated: the Go part — yes; the C side — a documented ceiling (~14-15GB write + ~9GB FTS for 105k). |

## Architecture

```
2dph/
  PLAN.md / AGENTS.md
  docs/                     published docs (this conversation → docs/ as md)
  skills/                   in-project skills (web-search, postgres, brain, picoclaw, diataxis-docs)
  bin/
    brain/search.go         deduction: facts → info → web
    brain/index.go          rebuild FTS + HNSW (incl. --with-mail)
    brain/add.go            incremental leaf write (no rebuild)
    brain/serve.go          HTTP API in-process + OpenAPI/MCP (D20); Zig CGO (D21)
    brain/get.go stats.go eval.go watch.go model.go   # Go read/ops (cgo)
    facts/extract.go audit.go crm.go  # 2-source pairing, confidence, CRM proof
    mail/sync.go import.go convert-mbox.go ocr.go    # mail ETL (Gmail/OO/M365), PDF OCR
    mail/incubator.go       # legacy .eml → doveadm save into the mail-server incubator (#252/epic #250)
    chats/sync.go import.go facts.go apply.go  # conversations (no chats index)
    contacts/*.go           CRM contacts importer
    web/search.go           SearXNG client (throttled ≠ absence)
    git/import.go           go-git history (no git binary; conversion only)
    markdown/import.go      H2 leaf split (Go)
    postgres/query.go       read-only YAML (wraps scripts/db/psql-yq)
    reasoner/bench.go        CPU tool-call bench (D18; OpenAI tools)
    qa/stats.go             DuckDB quantiles / JSONL (D22; not the test taxonomy)
    semver/next.go          next semver from conventional commits (Release job)
    cli/complete.go         flaggy bash/zsh/fish complete (D23)
    cgo/zig zcc zc++        CGO toolchain (zig cc, not gcc)
    stack/start start-assistant start-mail-sync stop status
    db/psql-yq              (vendored)
    ssh-tunnel              onlyoffice pg tunnel 5433
  test/
    system/                 offline-gated system tests (CI default)
    stress/                 live-brain load generator
    integration/            opt-in live-dependency tests
    README.md               how to run each tier
  etc/
    searxng/ picoclaw/      operator config (FHS; moved from deploy/)
  var/kb.lbug               single embedded store (gitignored)
```

## Schema (first pass)

Node tables: `Person, Service, Host, Container, Repo, File, Commit, Leaf`.
`Leaf(embedding FLOAT[N])` — FTS on `text`. Vector search: ANN IVF outside
liblbug (`internal/brain/ann`, #204; the stock liblbug HNSW Leaf_vec crashes
as the graph grows, #192 → built-in scan as fallback).
Edges: `RUNS / USES / FROM_FILE / HAS_VERSION / AUTHORED / ABOUT / ASSOCIATED / SIMILAR_0.85`.
`FROM_FILE` / `HAS_VERSION` / `AUTHORED`: `bin/brain/search.go --hop N` walks
them from each hit (1=File, 2=Commit, 3=Person). Rebuild writes
`Leaf-[:FROM_FILE]->File`; git import writes the rest.

Common props on every node/edge: `root`, `confidence`, `evidence[]`, `how`,
`where`, `when`, `source_rev`. Leaf interval of truth (D24): `valid_from`,
`valid_to`.

## Config

`~/.config/brain/` (0600):

- `search.env` — real `BRAIN_SEARCH_URL/USER/PASS` from `~/.config/ops/npm-bot.env`
- `db-profiles.yml` — real `onlyoffice` profile (SSH tunnel `127.0.0.1:5433`,
  user `onlyoffice`, db `onlyoffice`, `network: host`,
  `password_env_file: ~/.config/ops/onlyoffice.env`) + example profiles.
  `~/.config/ops/onlyoffice.env` is bootstrapped from the VM's
  `/etc/onlyoffice/documentserver/local.json` (dbUser/dbPass), never committed.
- `~/.config/brain/../` — nothing else lives in the repo.

## Tooling conventions

- `bin/{subject}/{method}` — line 2 is a usage comment (mirrors `psql-yq`).
- bash + golang primary (Go shebang); no Python in-tree.
- YAML default output, `--json` for machines. Slice with mikefarah/yq.
- Everything that touches the network / DB is read-only, throttled, cached.
- Tests (TDD) gate every commit; `gh` + CI/CD on every push.

## Open questions (v2)

- OQ1: **in** — D16 adjudication: `temporal_freshness` then `authority_pairing`.
  Unresolved 2v2 stays hypothesis. [#29](https://git.produktor.io/eSlider/2dph/issues/29).
- OQ2: OCR — **in**. `pdftotext -layout` first; scans `pdftoppm` + tesseract
  `eng+deu` (`bin/mail/ocr.go`, `internal/ocr`). No gocv, no gosseract CGO
  (D21 Zig owns Ladybug CGO). Optional `OCR_ENGINE=paddle` / compose profile
  `ocr-paddle`. Docling left the default path. When `pdftotext` yields no text
  layer (scanned / export-locked / oversized), **Ghostscript normalizes the PDF
  first** (`internal/ocr.NormalizePDF`, gs `pdfwrite` → `var/tmp`, original
  preserved, artifact ≤ original): strips export-protection + shrinks so the
  pdftotext fast path / tesseract fallback see a clean PDF. Clean PDFs skip gs
  (happy path stays fast). [#6](https://git.produktor.io/eSlider/2dph/issues/6),
  [#102](https://git.produktor.io/eSlider/2dph/issues/102).
- OQ3: **in** — duckdb-go (`pkg/duckdb`, `bin/jsonl/stats.go`) for
  quantiles / JSONL count. Not a second graph. [#30](https://git.produktor.io/eSlider/2dph/issues/30).
- OQ4: YAML-first storage for leafs — deferred: JSON is ~10x faster to
  serialize and unambiguous; YAML only where humans edit files.
- OQ5: **in** — fact `valid_from` / `valid_to` + `--as-of` / MCP `as_of` (D24).
  Not D16 `temporal_freshness`. [#36](https://git.produktor.io/eSlider/2dph/issues/36).

## Mail pipeline (done)

1. `bin/mail/sync.go` (Go, 8 workers) — paginated Gmail / OnlyOffice / M365
   Graph download. Gmail attachments key off `body.attachmentId`, not MIME
   `partId`. M365 uses client-credentials + delta link (commit after success).
   M365 inline attachments are kept (#312): stored in `attachments/` under
   `inline-<hash8>-<name>`, flagged `isInline`/`contentId` in `message.json`.
2. `bin/mail/import.go --from-raw` — message.json → message.md; PDFs via
   `pdftotext -layout` (~15ms); textless/scanned PDFs `pdftoppm` + tesseract
   `eng+deu`. PDFs without a readable text layer are first normalized with
   Ghostscript (`internal/ocr.NormalizePDF`, gs `pdfwrite`; original preserved,
   gs artifact in `var/tmp`, removed after extraction). ICS sidecars
   Latin-1→UTF-8 normalized.
3. `bin/brain/index.go --rebuild` — fresh rebuild (repo corpus + mail) because ladybug
   corrupts its WAL on bulk-insert into an already-indexed DB. Conversion and
   indexing stay separate for crash safety.
 4. Compose `mail-sync` / `scripts/stack/start-mail-sync` — ETL loop (default
    `onlyoffice,gmail`, 300s): download (existing mailsync binary) → bounded
    runner pipeline import (#98) with per-run stats YAML; full rebuild only if
    `MAIL_SYNC_INDEX=1`. Bot digests (ai-bot) and case wrappers reuse sync/OAuth;
    they do not replace the corpus path.
5. Result: 17,835 messages → 28,918 info leafs, FTS + HNSW healthy, searchable
   via `bin/brain/search.go`.

## CI/CD pipeline (D15)

`.github/workflows/ci.yml`:

1. go vet + go test ./... (root module; packages without ladybug cgo)
2. `go test ./internal/brain/rank` (cgo-free ranking + flag parser)
3. `bin/facts/audit self` (lexicon internal consistency; `bin/facts/audit.go` is the D14 wrapper)
4. `bin/brain/eval.go` via Zig (recall@5 ≥ 0.95) — the CI SoT.
5. `bin/cgo/zig go build -tags system_ladybug` (compile search with zig cc; fetches pinned zig+libs).

Feedback loop: every commit → PR → CI → green/gate → merge. Same discipline as
`db/tech-poc`: contract first where there is an OpenAPI/message shape.

## Execution order

1. scaffold repo (:done after this file + AGENTS.md + .gitignore + ci)
2. gh repo create eSlider/2dph --private + initial commit + CI
3. vendored skill integration (web-search, postgres, brain, diataxis-docs) — no remote links
4. schema + tools with TDD (kb + md + facts + brain)
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
| 3 | [#18](https://git.produktor.io/eSlider/2dph/issues/18) | **in** — `--with-facts` / `--facts-json` land `root=facts`; `--with-chats` indexes `var/corpus/chats/md`. WhatsApp sync is in v1 via [#87](https://git.produktor.io/eSlider/2dph/pulls/87). |
| 4 | [#15](https://git.produktor.io/eSlider/2dph/issues/15) | **in** — lever/loop documented (`search` → `get` → `audit`). |
| 5 | [#19](https://git.produktor.io/eSlider/2dph/issues/19) | **in** — CI recall SoT is `bin/brain/eval.go` via Zig. |

Does **not** block epic close: OQ4. OCR [#6](https://git.produktor.io/eSlider/2dph/issues/6), OQ1 [#29](https://git.produktor.io/eSlider/2dph/issues/29), OQ3 [#30](https://git.produktor.io/eSlider/2dph/issues/30) are **in**.

## 2026-09-03 — L-9.3: formal URL checks: identity/contradiction/excluded-middle/sufficient-reason (gitea #232, epic #229)

Goal: turn the empirical URL recipes (#56) into machine checks of Vinogradov's
laws at the URL level: identity (a canonical URL — one spelling),
contradiction (¬(P∧¬P) on a single URL), excluded middle (a fuzzy claim → not
FACT but open_questions), sufficient reason (verdict accept only on
FACT-/OPEN- premises). The core is cgo-free `internal/facts` (modelled on
Adjudicate/CheckFactRow), the CLI is the subcommand `bin/facts/audit.go formal`
(JSON facts/cards on stdin → JSON flags, exit 0/1).

| Item | Status |
|------|--------|
| `facts.CanonicalURL` — local URL canonicalization per the gator G-8.1 convention (lowercase scheme/host, default-port, utm_*/ref/gh_jid/token, query sorting, no fragment/trailing slash); the gator module is not imported | done |
| `internal/facts/formal.go`: `URLFact` (id/url/claim/attr/neg/conf/from/to) + `CheckURLIdentity` (merge/flag weaken), `CheckURLContradiction` (reject; confirmed only, D24 intersection), `CheckExcludedMiddle` (weaken → open_questions), `CheckSufficientReason` (accept without FACT-/OPEN- premises → reject), `CheckFormal` (aggregate, determinism) | done |
| CLI `bin/facts/audit.go formal` (shebang, hand-formatted): JSON {facts,cards} on stdin → {mode,ok,problems}; example: acceptance "vacancy is active" vs "vacancy closed" → contradiction verdict=reject | done |
| TDD: `canonical_test.go` (cross-channel/utm/determinism/normalization/errors) + `formal_test.go` (identity merge, contradiction acceptance + non-contradictions: same polarity/different attr/weak/disjoint D24, excluded-middle fuzzy→OPEN, sufficient-reason accept premises) | done — `go test -race ./internal/facts` green |
| Compatibility with #56/#231: `go test ./internal/facts` + `bin/facts/audit.go self` green (CI gate) | done |
| Documentation: docs/facts/audit-recipes.md §Formal URL checks (L-9.3 #232) + PLAN.md | done |

Verification: `go test -race ./internal/facts/` green, `go vet ./internal/facts/` clean,
`./bin/facts/audit.go self` ok. Branch `feat/facts-formal-checks#232` off `main`.

## 2026-09-02 — L-9.4: git commits → Commit/Person/AUTHORED graph (gitea #233, epic #229)

Goal: fill Commit/Person/AUTHORED (InitSchema write.go:130-133 already creates
the tables, there was no writer) from the git history of the git.produktor.io
repos — the source of facts "who worked on what, when" for the L-9 audit and
the network project axis #234.
Design: [docs/brain/graph-git-commits.md](docs/brain/graph-git-commits.md).

| Item | Status |
|------|--------|
| Git commit canon: `(repo, sha)`, `Commit.id = <repo>:<sha>` (repo = RepoName origin-remote); reading — `gitlog.Log` (go-git, the same canon as the premises corpus: merges skipped, author = Author, date RFC3339) | done |
| `internal/gitgraph` (cgo-free): Resolve (root scan/--repo), ReadAll, ToInputs (email lowercase — Person.id canon mail D-1 #257, dedup, sort by date ASC), ComputeStats, WeakSubject | done |
| `internal/brain/commitplan.go` (cgo-free): CommitInput, MERGE queries commit/AUTHORED, commitAuthor (Person.id=email); `gitcommit.go` (cgo): UpsertCommit/UpsertCommits (transaction, idempotent) | done |
| CLI `bin/git/graph.go`: dry-run/--commit, --repo/--root/--since/--limit, --skip-existing, live-holder guard, batches; before→after report Commit/Person/AUTHORED | done |
| CLI `bin/git/facts.go` (read-only): fact cards "who worked on which repo when" + audit card (claim/premises/inference/gaps/verdict; weak subject → weaken+OPEN) | done |
| TDD: cgo-free unit (commitplan + gitgraph: canon/mapping/dedup/Resolve/ReadAll/WeakSubject/GroupFacts/BuildFacts) + cgo against live Ladybug (temp DB): write/read-back, repeat = 0 duplicates, Person mail↔git junction, coexistence with Leaf/Message | done |
| Documentation: docs/brain/graph-git-commits.md + audit card template in docs/facts/audit-recipes.md | done |
| Live pilot: 2dph + gator dry-run → --commit into kb.lbug (backup, live-holder guard), idempotency repeat, git-facts spot check | done |

Verification: `go test ./...` + `go vet ./...` green (CI mode);
`bin/cgo/zig go test -tags system_ladybug ./internal/brain/` green (whole package);
`bin/cgo/zig go vet -tags system_ladybug ./internal/brain/` green.
Branch `feat/git-commits#233` off `main`.

## 2026-09-02 — L-9.5: relationship network "who with whom and through whom" (gitea #234, epic #229)

Goal: a network tool on top of the finished graph (mail D-1 #257 + git L-9.4 #233):
Person↔Person via shared mails/threads/recipients (SENT/TO/CC/BCC/REPLY_TO) and
shared projects (AUTHORED Commit.repo), with premises (Message.id/Commit.id) and
a verdict per audit card (accept/weaken); export accept to CRM (ADR-0012 item 4).
Read-only: writes nothing, does not change the schema. Design:
[docs/brain/graph-network.md](docs/brain/graph-network.md).

| Item | Status |
|------|--------|
| `internal/network` (cgo-free): BuildLinks — mail derivations (direct contact sender↔recipient, TO/CC/BCC weight, shared recipient, REPLY_TO dialog, threads) + git (shared project via AUTHORED Commit.repo), premises, period, verdict accept/weaken+OPEN, ranking | done |
| Read-only graph queries `internal/network/query.go` (cgo): LoadRows — Message/Person/SENT/TO/CC/BCC/REPLY_TO edges + Commit/AUTHORED → network rows | done |
| CLI `bin/network/network.go` (read-only): `--person` (required), `--project`, `--since/--until`, `--limit`, `--json`, `--accept-only` (CRM export YAML) | done |
| TDD: cgo-free unit (derivations/weight/verdicts/ranking/premises on synthetic Alice/Bob/Carol) + cgo against live Ladybug (temp DB): LoadRows→BuildLinks end-to-end | done |
| Live pilot: eslider@gmail.com / andriy.oblivantsev@wheregroup.com on the live kb.lbug (read-only) — top links, premises, CRM export accept | done |
| Documentation: docs/brain/graph-network.md + PLAN.md | done |

Verification: `go test ./...` + `go vet ./...` green (CI mode);
`bin/cgo/zig go test -tags system_ladybug ./internal/network/ ./internal/brain/`
green (cgo). Branch `feat/network#234` off `main`.

## 2026-09-02 — D-1.2: Message/Person graph schema + write path (gitea #259, epic #257)

Goal: move the conversation canon ([#99](https://git.produktor.io/eSlider/2dph/issues/99),
`internal/canon`) into a working Ladybug schema kb.lbug: a **Message** node +
**SENT/TO/CC/BCC/REPLY_TO** edges next to the existing Leaf/Person; idempotent
write (MERGE). Design: [docs/brain/graph-gator-mail.md](docs/brain/graph-gator-mail.md)
(D-1.1 #258). As a result, mail in 2dph lives as a graph of entities rather than
as Leaf text from the raw data.

| Item | Status |
|------|--------|
| `InitSchema` additively (`IF NOT EXISTS`): `Message` node (id/thread_id/folder/subject/sent_at/gator_ref/body, PK id) + rel `SENT (Person→Message)`, `TO/CC/BCC (Message→Person)`, `REPLY_TO (Message→Message)`; Person/Leaf/Commit untouched | done |
| `internal/brain/graphplan.go` (cgo-free): `MessageInput` (canon.Message + folder/subject/gator_ref), `planGraph` — unique Persons (From→To→CC→BCC, name of the first occurrence), edges = `canon.Edges()` minus PART_OF/self-REPLY_TO; generation of MERGE queries (person/message/edge), `edgeSchema` | done |
| `internal/brain/graph.go` (cgo): `UpsertMessage`/`UpsertMessages` — a batch in one transaction, MERGE by message_id/email; Person name merge "last sync wins" (SET in MERGE); gator_ref on Message | done |
| TDD: cgo-free unit (persons/edges plan, determinism, MERGE query shapes, sentAtText); cgo tests against live Ladybug (temp DB): write+read-back, repeat run = 0 duplicates, name merge, coexistence with Leaf + EnsureIndexes | done — 8 unit + 4 cgo green |
| Documentation: §8 "D-1.2 implementation" in the design doc (schema + write path) | done |
| Import gator kind=mail → graph (wheregroup 3984) | #260 (next) |

Verification: `bin/cgo/zig go test -race -tags system_ladybug ./internal/brain/` green
(whole package, ~6 min); `go vet ./...` + `go test ./...` green (CI mode);
`go test ./internal/canon/` — 14 tests green (canon #99 unchanged).
Branch `feat/graph-schema#259` off `main` (cc40235).

## 2026-08-18 — Go-only write path sync (gitea #41)

Goal: consolidate the most advanced version here and in sync with GitHub main.
Sources: GitHub main `b1e6953` + arc-1 `/mnt/8TB/projects/ai/2dph` Go-write WIP +
edelweiss `/home/devops/projects/2dph` M365 WIP. Prefer Go; Python only for A/B.

| Step | Status |
|------|--------|
| sync issues via tea (gitea #41 assigned) | done |
| Phase 1: branch `feat/brain-go-writepath` + arc-1/edelweiss WIP | done |
| Phase 2: finish Go write path (write.go/corpus.go/index/add/mailconv) TDD | done |
| yaml-seed secret/noise filter; compose/CI → Go (no Python INSTALL) | done |
| Phase 3: PR → CI green → merge GitHub main; docs (D6 reversal) | done — PR #42, merged `2cea0e7` |
| Phase 4: go-ollama + go-xls PR/merge; verify go-env/go-hocr/go-onlyoffice | done — PR #4 (`325c4e2`), PR #1 (`165d25d`) |
| Phase 5: deploy brain :8630 + wire MCP into opencode.json | done — brain live (7754 leafs), MCP wired; PR #43 merged `a1880de` |

Note: CI found + fixed 3 real bugs in the port (modeldir HF-cache default +
snapshot weight selection, secret/noise filter false positives).

Deploy findings (PR #43): fts-extension install needs writable+execable HOME
(`/data`, not noexec `/tmp`); serve must bind `KB_HOST=0.0.0.0` for compose
publishing; corpus walk must skip permission-denied entries; leaf text needs
UTF-8 sanitization before FTS.

Decisions (2026-08-18): exclude arc-1 junk (external/, docs/chats/, bin/chats.bin,
qa/load_test_*.py); Python fully removed (2026-08-19); M365 WIP same PR;
go-ollama+go-xls now, rest verify.
## 2026-08-22 — Typed config (gitea #90, epic #88)

Goal: replace ad-hoc `os.Getenv` in the service layer with one typed config
loaded by a stack, keeping legacy env names working. Dep
`github.com/eslider/go-config@v0.4.0`; new `internal/config` (`config.go`,
`load.go`, `yaml.go`, `env.go`) — TDD offline fixtures, no network/db.

| Step | Status |
|------|--------|
| internal/config load stack + merge + lower+alnum + legacy mapping (TDD) | done — tests green |
| wire pkg/httpapi, internal/brain, bin/brain/{serve,index,watch}, internal/mailsync, internal/websearch | done — no ad-hoc env reads in those packages |
| committed `etc/brain/config.yml` template + gitignore `config.local.yml` | done |
| legacy env map | KB_ROOT→Root, KB_PORT→Port, KB_HOST→Host, KB_WORKERS→Workers, KB_PPROF→Pprof, KB_SEARCH_CMD→SearchCmd, KBSEARCH_PORT→SearchDaemonPort, KBSEARCH_NO_DAEMON→SearchNoDaemon, KBSEARCH_MODEL→Model, HF_HOME→HFHome, KB_BUFFER_POOL→BufferPool, KBTEST_EPS→Eps, KB_INDEX_ALLOW_LIVE→IndexAllowLive, KB_WATCH_INTERVAL→WatchInterval, KB_WATCH_DIRS→WatchDirs, OO_CLI→OOCLI, BRAIN_SEARCH_{URL,USER,PASS,CACHE,ENV}→Search.* |

Non-goals kept: graph/search semantics unchanged. Pending: OnlyOffice CLI
config wiring from `bin/mail/*` (out of #90 scope) — `internal/mailsync.SetOOCLI`
is ready for it.
## 2026-08-22 — MIME parser swap in mailconv (gitea #95, epic #88)

Goal: replace `jhillyerd/enmime/v2` with `emersion/go-message` in
`internal/mailconv` (parser-equivalent swap, no behaviour change to graph/search).

| Step | Status |
|------|--------|
| fixtures: plain / alternative / mixed / nested(alternative+mixed) / charset(iso-8859-1 QP) `.eml` in `internal/mailconv/testdata/` | done |
| `parseEML` via `message.Read` + `entity.Walk()` recursion; text/plain→TextBody, text/html→HTMLBody, attachment leaves by filename/disposition | done |
| charset via `go-message/charset` (x/text ianaindex/htmlindex) — blank import | done |
| public API stable: `FromEML`/`FromRaw`/`ConvertAttachment`/`Message`/`Attachment` unchanged; `writeMessageJSON`/`writeEMLAttachments` internal | done |
| `go mod tidy` → enmime absent from go.mod | done |
| `go build ./internal/... ./pkg/...` + `go test -race ./internal/mailconv/... ./pkg/...` green | done |
| -benchmem before(env v2.4.1)/after(emersion v0.18.2): ~6–9× faster ns/op, B/op ≈ half, allocs ≈ 40–55% lower | done — bench in `eml_test.go` |

Branch `feat/mime-emersion#95`: c8f7120 (test) → 7015ffc (feat) → 9b4425d (chore/deps).
## 2026-08-22 — mailsync v1 small fixes (gitea #111, epic #88)

Goal: small defects found porting mailsync v1 — `mailconv.FromEML` folder
extraction and the Dockerfile `pkg/` copy.

| Step | Status |
|------|--------|
| test (red): `TestFromEMLFolderNestedLayout` — `<root>/<folder>/<id>/<id>.eml` asserts folder=`<folder>` via `message.json` + `message.md` | done |
| `emlFolder(root,p)`: parent of the `<id>` dir (grandparent of `.eml`) for the nested layout; flat `<root>/*.eml` keeps immediate dir; applied in `FromEML` + `writeMessageJSON` | done |
| Dockerfile `pkg/` copy: release/v1 root `Dockerfile` already `COPY . .` (covers `pkg/cli` for `internal/ocr`); no `deploy/mail-watcher.Dockerfile` in release/v1 (that file lives on main via #107, not an ancestor) — no change needed | already-fixed (verify only) |
| `go test ./internal/mailconv/... ./internal/mailsync/... ./internal/ocr/...` + `./internal/...` green; `gofmt -l` clean; `go vet` clean | done |

Branch `fix/mailsync-v1#111` → PR into `release/v1`.
## 2026-08-23 — PR + merge feat/imap-mailsync-v1 → release/v1 (gitea #112, epic #88)

Goal: land the v1 generic IMAP source (from #106) that was never merged:
`internal/mailsync/{imap,watch}.go` + offline tests + CLI/flags wiring +
`bin/mail/watch` entrypoint + `deploy/mail-watcher.Dockerfile`.

| Step | Status |
|------|--------|
| delta audit vs release/v1 (756f514): missing = imap source files, watch loop, CLI flags (`--once/--brain/--ocr/--local`), `IMAP_*` env parsing, `SyncConfig.IMAP`, `bin/mail/watch`, `deploy/mail-watcher.Dockerfile`, `go-imap/v2`+`go-sasl` deps | done |
| overlap check: brain write-path fixes (`internal/brain/*`, `mailconv.FromEML`) already on release/v1 via #109/#110/#111 — NOT re-ported; old `gmail.go`/`m365.go`/`onlyoffice.go` untouched and compiling | done |
| port: `internal/mailsync/imap.go` + `imap_test.go` + `watch.go`, cli.go/sync.go wiring, `bin/mail/watch/main.go`, Dockerfile, `go.mod`/`go.sum` | done |
| source selection: `--source imap` → `IMAPEnv(envVars)` → `SyncConfig.IMAP` → `newIMAPSources`; `cfg.Raw=true` (always `.eml`); readEnv honours `IMAP_*` | done |
| tests (offline, synthetic Alice/Bob): `SanitizeFolder`, `IMAPEnv`, `parseIMAPMessage` (emersion parser), `hasNoSelect`; no network | done |
| `go test ./internal/...` + `./internal/mailsync/...` green; `go vet` clean; `gofmt -l` clean (shebang-protected files excluded) | done |
| branch `merge/imap-mailsync-v1#112` off `origin/release/v1` → PR into `release/v1`, CI green | done (open, not merged) |

Branch `merge/imap-mailsync-v1#112` → PR into `release/v1`.

## 2026-08-23 — reasoner client → api-client canon (gitea #93, epic #88)

Goal: apply `skills/api-client/SKILL.md` (go-ollama canon) to
`internal/reasoner/client.go`, first candidate. Removes the 16× `map[string]any`
(typed `Tool`/`ToolSchema`).

| Step | Status |
|------|--------|
| test: offline httptest typed client — chat round-trip, tool-call, error/status, ctx cancel, NDJSON doStream | done — `-race` green |
| feat: `Client{hc,cfg}` + `New(cfg)`, `doJSON`/`doStream` core, typed `ChatRequest`/`ChatResponse`, ctx-first | done |
| config: `reasoner.Config` + `LoadEnv()` accessor; bench.go reads `REASONER_BASE_URL` from it | done |
| bench.go: typed client, `ctx`; bake-off behaviour preserved | done |
| `map[string]any` in `internal/reasoner/client.go` → 0 | done |
| PLAN.md #93 status | done |

Notes: rebased onto `release/v1` `36f5ec4` (after #90 landed). `internal/config`
carries no reasoner fields, so a minimal `reasoner.LoadEnv()` accessor (env →
`Config`) is kept; wiring `REASONER_BASE_URL` into `internal/config.Config` is
deferred. Bench shebang is `gofmt`-protected (not reformatted).
## 2026-08-22 — go-config wave 2: tools + tests (gitea #91, epic #88)

Goal: extend `internal/config` to the remaining tools/packages, add a `reasoner`
section and retire the ad-hoc `os.Getenv` config reads (KB_*, BRAIN_SEARCH_*,
REASONER_*, HF_HOME, model, port/workers…).

| Step | Status |
|------|--------|
| `config.ReasonerConfig{BaseURL,Model,Device}` + `Config.Reasoner` section + defaults | done |
| legacy map: REASONER_BASE_URL→Reasoner.BaseURL, REASONER_MODEL→Reasoner.Model, REASONER_DEVICE→Reasoner.Device | done |
| reasoner client config from typed stack: `reasoner.Configure(cfg)` + `FromTyped`; `LoadEnv()` reads `active` | done — `bin/reasoner/bench.go` loads `config.Load` + `Configure`; client tests green |
| `bin/web/search.go`: BRAIN_SEARCH_CACHE/ENV → `cfg.Search.Cache/Env` (HOME fallback kept) | done |
| `bin/brain/model.go`: HF_HOME/KB_ROOT → `cfg.HFHome/cfg.Root` | done |
| `etc/brain/config.yml` template gains `reasoner:` section | done |
| TDD fixtures: `reasoner` deep-merge in `testdata/basic`, legacy + defaults tests, `isolateEnv` covers REASONER_* | done — `go test -race ./internal/... ./pkg/...` green |

Notes: `internal/chat/paths.go` KB_ROOT + `pkg/repo/exec.go` KB_ROOT left as
ad-hoc env reads — chat sources are secret-heavy (no config accessor yet) and
`pkg/` must not import `internal` (overlaps #92 `pkg/utils`). Bench/web shebangs
are `gofmt`-protected (not reformatted).

## 2026-08-22 — pkg/utils + static-typing hotspots (gitea #92, epic #88)

Goal: promote duplicated helpers into public `pkg/utils` with static typing and
kill the `map[string]any` / `interface{}` HTTP and root-resolution hotspots.
`pkg/utils` stays internal-free (D29, primitives only).

| Step | Status |
|------|--------|
| `pkg/utils/root.go` `Root()` — KB_ROOT or walk-up `.git`/`var`, single source of truth; `pkg/repo.Root()` and `internal/chat.Root()` delegate | done |
| `pkg/utils/json.go` `DoJSON`/`GetJSON` — typed request + 2xx check + decode, `opts` for auth; wired into `internal/reasoner.Client.doJSON`, `internal/websearch.Fetch`, `internal/chat.cdpTabs` | done |
| `pkg/utils/str.go` `Snippet`/`Or` — `reasoner.snippet`, `chat.orDefault` removed | done |
| TDD fixtures: `pkg/utils/{root,json,str}_test.go` offline (httptest, t.TempDir) | done — `go test -race ./pkg/... ./internal/...` green, `go vet` clean, CGO ladybug build green |

Notes: `pkg/httpapi` write-side (`writeJSON`/`writeRaw`) is a different direction
(server response, not client decode) and was left as-is. Remaining
`map[string]any`/`interface{}` decode sites in `internal/brain`, `internal/chat`
(MCP/JSONL) and `internal/config` are domain-shaped and out of scope for this
pass.
## 2026-08-22 — Ladybug write-path hardening + soak (gitea #109, epic #88)

Goal: kill the silent in-process Ladybug death on `POST /ingest` (~10% of
writes, ExitCode=0, no Go panic) and prove it with a soak test.

Root cause (release/v1): `HTTP.Ingest` opened a **second** handle on the live
`kb.lbug` (`OpenWritable`) while the serve read handle stayed open — liblbug
dies silently when the same file is double-opened inside one process. The
existing close→reopen swap (`refreshBrain`) also ran unsynchronized against
concurrent handlers: any reader mid-query could execute a statement against a
just-closed `*lbug.Connection` (C use-after-close; frame
`LocalNodeTable::isVisible ← HashIndex::lookupInPersistentIndex`). Error paths
left the serve handle stale for the process lifetime.

| Step | Status |
|------|--------|
| `brainMu sync.RWMutex` guards global db/conn; readers (Get/Stats/Audit, queryFTS/queryVector/attachHops, lookupLeaf/leafStats) hold RLock | done |
| Ingest serialized (`ingestMu`), holds write lock over close→write→reopen; `closeBrainLocked()` runs BEFORE `OpenWritable()` (no same-file double-open) | done |
| Restore on every exit path (`ingestWriteLocked` + deferred restore): write-stage errors no longer leave a stale/nil serve handle | done |
| `embedIngestLeafs` seam — embed step extracted as package var, tests run full Ingest offline (no HF model/daemon); singleton-cache (#110) replaces only its body | done |
| Soak: `TestIngestSoakWritePath` 200 × real `/ingest` + 8 concurrent readers (Stats/Audit/Get/FTS/vector), temp KB_ROOT, offline | red before fix (native segfault), green after |
| Race proofs: `TestIngestSwapRaceSafe`, `TestIngestWriteRestoresServeConnOnError` | green under `-race` |

Verify: `bin/cgo/zig go test -race -tags system_ladybug ./internal/brain/`
green; `go vet` clean; zig build of `bin/brain/search.go` OK. Same-file
double-open stays forbidden inside one process; bulk rebuild keeps its own
process (`bin/brain/index.go`). Out of scope: corpus CLI path (single-goroutine
index process), model singleton cache (#110).

## 2026-08-23 — ETL handler registry + safe walker (gitea #96, epic #88)

Goal: base layer for sync-ETL. A concurrency-safe registry of per-format ETL
handlers plus a hardened file walker (path traversal / symlink-escape /
depth-limit / binary-huge-file skip). Runner (#98) and source-adapters (#97)
build on these.

| Step | Status |
|------|--------|
| `internal/etl/registry.go`: `Registry` (RWMutex) mapping handler key → `Handler{Name() string; Handle(ctx, path) error}`; `Register` rejects duplicate/empty keys, `Lookup`/`Names`(sorted)/`Len` — concurrent-safe | done |
| `internal/etl/walker.go`: `WalkFiles(root, WalkOptions{MaxDepth, MaxBytes, SkipBinary, Exts})` → deterministic sorted `[]File{Path,Rel,Size}`; rejects `..` traversal, never follows symlinked dirs, rejects symlink escapes outside root, honors depth limit, skips huge/binary files | done |
| `internal/markdown.WalkMarkdown` refactored to the hardened walker (Exts filter) | done |
| TDD: `internal/etl/etl_test.go` — register/lookup/dup/sorted/concurrent(-race), deterministic order, traversal rejected, symlink-to-outside rejected, depth limit, large-file skip, binary skip, ext filter, missing-root error | done — `go test -race ./internal/...` green, `go vet` clean, gofmt clean |

Notes: scope kept to registry + walker only; pipeline rewiring is #97/#98.
`internal/mailconv` and `bin/*` walkers stay legacy until a source adapter owns
them (single-implementation rule D33) — cutover tracked in #98.

## 2026-08-25 — source adapters (gitea #97) + bounded ETL runner (gitea #98, epic #88)

Goal: unified `Source` adapter layer + the bounded pipeline runner that wires
`Source → Decode(Registry) → Transform → Load` with backpressure, per-run stats
YAML and graceful shutdown.

| Step | Status |
|------|--------|
| `internal/source`: `Source{Name(); Fetch(ctx,cursor)→[]Blob,Cursor,error}`, `Blob{ID,Kind,Path}`, `Cursor`; `Sync(ctx,src,handle,Options{StatePath})→Stats{Fetched,New,Skipped}` — sha256 seen-set dedup + atomic checkpoints, mid-batch resume | done (#97) |
| `source.Disk`: re-lists a corpus dir of `.eml`, content-hash identity, cursor never advances (driver dedups) | done (#97) |
| `internal/runner`: bounded channels between stages (backpressure), `errgroup.WithContext` first-error cancel, wg-accounted goroutines, graceful shutdown; reuses `source.Sync` + `etl.Registry` | done |
| Runner `Report`: source stats + per-handler ok/failed counts, serialized as stats YAML (`startedAt/duration/source/handlers`) | done |
| `bin/runner/run.go`: CLI daemon over the disk corpus; `mail` handler → `mailconv.FromEMLFile` (per-blob .eml→md); `--interval` loop + SIGINT/SIGTERM graceful shutdown | done |
| Compose `mail-sync` migrates to the runner (`command: ["runner","300"]`); `MAIL_SYNC_SRC/ENV/INDEX` semantics preserved (`INDEX=1` = full brain rebuild); download stays on the existing mailsync binary (source-adapter mapping for onlyoffice/gmail = follow-up) | done |
| TDD: `internal/runner/runner_test.go` — e2e disk source smoke (stats YAML round-trip), bounded in-flight backpressure, graceful shutdown on cancel (-race), unknown-kind error; `internal/mailconv` `FromEMLFile` idempotence | done — `go test -race` green, `go vet` clean, `docker compose config` parses |

## 2026-08-22 — singleton-cache ingest model (gitea #110)

Goal: kill the ~0.9 GiB/`/ingest` leak. `HTTP.Ingest` called `LoadModel()` per
request and `defer model.Close()`; `StaticModel.Close()` frees only the
tokenizer, not the safetensors matrix (measured 3 ingests: RSS 3.5→6.1 GiB;
~900-email backfill = OOM).

| Step | Status |
|------|--------|
| `internal/brain/ingestmodel.go`: `embedder` interface (no Close — structural guarantee), `ingestModelCache` (mutex, failed loads not cached → retry next request), `getIngestModel()` process-wide singleton, `embedLeafs()` extracted from Ingest | done |
| `internal/brain/http.go` Ingest: `LoadModel()+defer Close()` → `getIngestModel()` + `embedLeafs`; shared model never closed per request | done |
| TDD: `internal/brain/ingestmodel_test.go` — single load across consecutive requests, 16-goroutine concurrent load-once, zero Close of shared model, retry-after-loader-error, pre-embedded skip | done — `zig go test -race -tags system_ladybug ./internal/brain/` green, `go vet` clean both tag modes, CGO search build green |

Notes: other `LoadModel()` callers (`bin/brain/add.go`, `index.go`,
`bin/facts/*`) are one-shot CLI processes — unchanged. Diff kept strictly to
model caching; connection lifecycle in the same function is #109 territory.

## 2026-09-23 — shared embedding model on the search path (gitea #335)

Goal: same fix as #110, extended to `/search`. `internal/brain/search.go`
`embedQuery` still called `loadModel()` + `defer model.Close()` on every
request, and `StaticModel.Close()` freed only the tokenizer — so each search
retained ~490 MiB inuse (~980 MiB allocated) until GC, and a live `brain-serve`
grew to 13 GiB (RSS 1.8 + swap 11.2) over the DB's 11 MB.

| Step | Status |
|------|--------|
| `internal/brain/embedmodel.go` (renamed from `ingestmodel.go`): `embedder` (no Close), `embedModelCache`, `getEmbedModel()` process-wide singleton; now backs /ingest, the /search in-process fallback AND ANN extract | done |
| `search.go` `embedQuery` → new `embedInProcess()` (cache, never Close); `serve()` (daemon) uses the shared model without closing; `anntool.go` `annExtractEmbed` drops its private `sync.Once` for the shared cache | done |
| `model.go` `StaticModel.Close()` now releases `mat` too (one-shot CLI callers); shared instance is never closed | done |
| `pkg/httpapi` pprof binds `127.0.0.1` instead of the API host (was `0.0.0.0:1707` via KB_HOST) | done |
| `compose.yaml`: `KB_BUFFER_POOL` 4 GiB → 512 MiB, `deploy.resources.limits.memory: 2g` (container had `HostConfig.Memory=0`) | done |
| TDD: `internal/brain/embedmodel_test.go` — 200 sequential search embeds load once and never Close; real `embedQuery` fallback loads once; `Close` frees the matrix; plus the #110 cache tests | done |

Live before/after (same host, `KBSEARCH_NO_DAEMON=1`, sequential
`test/stress --mix search:1 --c 1`): before, 13 searches grew RSS 147 MiB →
6.8 GiB (swap 0.5 GiB) with `alloc_space` `loadMatrix` 13.84 GiB; after, 49,448
searches kept `alloc_space` `loadMatrix` at 977 MB (one load), p50 6 ms vs
1429 ms, RSS post-warmup delta ~36 MiB (< 50 MiB), swap flat. See `PERF.md`.

## 2026-08-23 — concurrency audit: mailsync, corpuswatch, index, bench (gitea #94)

Audit of the concurrency paths outside `internal/brain` db/conn lifecycle
(#109/#110 `brainMu`), driving the `internal/mailsync` worker pools, the
`corpuswatch` poll loop, the `bin/brain/index.go` bulk rebuild and the
`bin/reasoner/bench.go` bake-off. Goal: data races, unbounded concurrency,
unsynchronized shared state, goroutine leaks — hardening only, no semantic
change. Result per component (all verified with `go test -race`):

| Component | Verdict | Hazard(s) found | Fix |
|-----------|---------|-----------------|-----|
| `internal/mailsync` `Run` worker pool + gmail/m365/onlyoffice sources | clean | none — counters already atomic, failures slice already under mutex, m365 token already channel-mutexed, bounded workers, no leak | none (test seam + race test only) |
| `internal/corpuswatch` poll loop | clean | none — single-goroutine loop, `Stamp` is pure filesystem walk, no shared state | none (race test for `Stamp` concurrency) |
| `bin/brain/index.go` → `WriteCorpus` → `parallelEmbed` + `ProgressReporter` | **hazard fixed** | `ProgressReporter.total` was a plain `int` written from embedding worker goroutines (`Report`) and read under the mutex in `render` → data race | `total` → `atomic.Int64` (`internal/brain/corpus_pool.go`) |
| `bin/reasoner/bench.go` → `internal/reasoner` `Client.Run` | clean | none — client holds only `*http.Client` + immutable `Config`, `Run` keeps accounting in call-local state | none (race test for concurrent bench runs) |

| Step | Status |
|------|--------|
| `internal/brain/corpus_pool.go`: `ProgressReporter.total int` → `atomic.Int64` (race on concurrent `Report`) | done |
| TDD: `internal/brain/progress_race_test.go` — 16-goroutine concurrent `Report` + `Finish` under `-race`; total converges | done |
| TDD: `internal/mailsync/sync_race_test.go` — 8-worker `Run` over a fake `Source` under `-race` (test-only `sources` seam, no prod behavior change) | done |
| TDD: `internal/corpuswatch/stamp_race_test.go` — 16-goroutine concurrent `Stamp` under `-race` | done |
| TDD: `internal/reasoner/client_race_test.go` — 12 concurrent `Client.Run` + 16×10 shared-client `RunPrompt` under `-race` vs httptest | done |

Verification: `go test -race -count=1 ./internal/...` green; `go vet ./internal/...`
clean; touched files gofmt-clean; cgo Zig build of `bin/brain/search.go`
(`-tags system_ladybug`) green. Branch `audit/concurrency#94` off `release/v1`.

## 2026-08-23 — URL content addressing, selectors, granular split (gitea #100)

Goal: stable content addressing for the ETL graph. Every extracted node gets a
canonical URL `scheme://platform/thread/msg/path-segments[#anchor]`, node ID =
`sha256(url)[:16]`, separate content ID = `sha256(body)`; parts reference each
other only via URLs. Content splits granularly BEFORE insertion into typed
Items (paragraph/heading/table/row/cell/image/link/page); a DOM/jQuery-like
selector mini-language addresses any leaf. Scope: addressing + selector +
split helpers with proof-of-use tests. Graph wiring (#99/#101, Ladybug) and
format coverage beyond html/md are out of scope here.

| Step | Status |
|------|--------|
| `internal/address`: `Segment` (parse/render `type[index]`), `New`/`Parse` canonical URL (validated scheme/platform/thread/msg/segments/anchor), `NodeID(url)=sha256(url)[:16]` (32 hex), `ContentID(body)=sha256` (64 hex) | done |
| `internal/selector`: mini-language `p[3] > table[0] > tr[1] td[2]` (`>` direct child, space descendant, optional index = any) — `Parse` (strict grammar errors) + `Apply(root)` over a typed Item tree | done |
| `internal/items`: `Kind` named int enum (page→`body`/heading/p/table/tr/td/img/a), typed `Item{Kind,Seg,Path,URL,Body,Src,Alt,Href,Children}`; `SplitHTML` (via `golang.org/x/net/html`) and `SplitMarkdown` (pipe-tables + standalone link/image) assign per-type indices and canonical URLs; content split happens BEFORE insertion | done |
| TDD: `internal/address` round-trips + validation + id derivation; `internal/selector` parse/apply + edge cases (bad grammar, missing index, no-match); `internal/items` html/md fixtures (testdata/); `internal/items/e2e_test.go` — split→select→verify URL+node/content id, parsed back to same leaf | done |

Verification: `go test -race -count=1 ./internal/...` green; `go vet ./internal/...`
clean; new packages gofmt-clean. Branch `feat/content-url-addressing#100` off
`release/v1`. No end-to-end graph write (that's #99/#101 + Ladybug).

## 2026-08-23 — secret scan in CI of all repos (gitea #142)

Goal: prevent new secret leaks at the PR/push stage (after #140 — 8 HIGH files in matrix).

| Step | Status |
|------|--------|
| Tool choice: **gitleaks** (official image `zricethezav/gitleaks`) — diff/commit scan, fail-on-leak, generic+private-key rules | done |
| CI job `secret-scan` (pull_request + push, scans only the diff of new commits) in 2dph (ci.yml), go-onlyoffice (test.yml), go-config (secret-scan.yml), matrix (.gitea/workflows/secret-scan.yml) | done — PR 2dph#143, go-onlyoffice#5, go-config#1, matrix#6 |
| git pre-push + pre-commit hooks `scripts/githooks/` (+ install.sh), block push/commit on a finding | done |
| Skills git-workflow + devops: rule "scan before any push"; synchronized to ai-fabric (release/v2) | done |
| Verification: the scan catches real leaks in matrix #140 (14 findings, exit 1); pre-push blocks a push that carries a secret | done |

## 2026-08-23 — Conversation canon: model + disk + manifest + graph (gitea #99, epic #88)

Goal: one canonical model for mail and chats — `Message{ID,ThreadID,Platform,
From,ReplyTo,To,CC,BCC,SentAt,Body,Attachments(lazy)}`; stored on disk under
`var/corpus/{mail,chats}` as JSON + sha256 manifest (idempotent upsert); graph
`(:Person)-[:SENT]->(:Message)-[:TO|CC|BCC]->(:Person)`, `[:REPLY_TO]`, `[:PART_OF]`
paragraphs. First mergeable increment: schema core + disk + manifest + round-trip
and a golden test (one `.eml` and one telegram export produce the same graph schema).

| Step | Status |
|------|--------|
| `internal/canon` (evidence layer, package-domain): `Person`/`Attachment`(lazy `Open(ctx)`, no body buffering)/`Message`/`Edge`/`EdgeType` (SENT/TO/CC/BCC/REPLY_TO/PART_OF), `Edges()`, `GraphSchema()` | done |
| `Store{root}`: `Write` (idempotent upsert by sha256 of the body), `Read` (skips corrupt files), `LoadManifest`/`SaveManifest` (atomic); layout `root/<platform>/<thread>/<id>.json`; path segments via `pkg/utils.SafeSegment` | done |
| `FromMail(r)` on emersion/go-message: envelope (From/To/Cc/Bcc via mail.Header.AddressList), ThreadID from References/Message-Id, ReplyTo from In-Reply-To, body text/plain (fallback text/html → mailconv.StripHTML), attachments lazy (metadata) | done |
| `FromChat(platform,thread,text,from,ts,replyTo)`: Person id = `platform:handle`, ts parsing (RFC3339), ReplyTo chain | done |
| TDD (offline, fixtures synthetic Alice/Bob/example.com): round-trip write→read→canon for mail and chat; idempotent repeated write; manifest = sha256(JSON); layout; Edges/schema; golden — mail and telegram produce the same `GraphSchema()` | done — 13 tests, `-race` green |
| docs/PLAN.md: section #99 (this block) | done |

Left over (outside the first increment): converters for the existing sources (gmail/onlyoffice
`mailconv.Message`, `mailsync.Message`, whatsapp/linkedin export) → canon; graph write
into Ladybug (`(:Person)/:Message/:Paragraph` nodes + `SENT|TO|CC|BCC|REPLY_TO|PART_OF`
edges) — will be a separate increment on #99; migration of `bin/*/import` to the canon.

Verification: `go build ./...` green; `go vet ./internal/canon/... ./pkg/utils/...`
clean; `go test -race ./internal/canon/... ./pkg/utils/...` green. Branch
`feat/conversation-canon#99` off `release/v1`.

## 2026-08-25 — OO Mail: HTML template + render check before sending (gitea #76)

Goal: from wave-1 the mails went out as plain text in one line; now a branded
HTML template + a render-check gate that blocks send until the draft passes
assert (paragraphs, signature, logo cid, chat-line without duplicates).

| Step | Status |
|------|--------|
| `internal/oohtml` (new package-domain): branded produktor.io template (palette #76: cream `#FAF5EA`, ink `#0A0A0A`, navy `#142039`/`#0E1626`, CTA `#F2C849`, primary `#143A6F`; system-ui; inline styles; logo `cid:produktor-logo.gif` without remote URL) | done |
| `RenderCheck(html, TemplateData)` — typed assert: paragraphs ≥2, greeting, offer+CTA, signature+site, logo cid, no duplicate text blocks (`Code` enum) | done |
| `Mailer.Send(ctx, SendParams)` — TDD gate: `Build` → `SaveMailDraft`(HTML) → `GetMailMessage` back → `RenderCheck` → `SendMail` only on a clean check; otherwise error "render check failed, send blocked" | done |
| `BuildMessage(w, …)` — MIME `multipart/related` via `emersion/go-message`: HTML part + inline `image/gif` with `Content-ID` = logo (cid embedding, not remote fetch) | done |
| Logo assets `etc/onlyoffice/assets/produktor-logo.{png,gif,-64.gif}` | ready |
| TDD offline (fixtures synthetic Alice/Bob/example.com; fakeStore instead of the network): 10 tests, `-race` green | done |

Verification: `go vet ./...` clean; `go test -race ./internal/oohtml/` green (10/10).
Branch `feat/ooo-mail-html-template#76` off `release/v1`. Left out of scope:
wiring into the OnlyOffice CLI (`oo mails send`) and the actual send with an attached
cid logo through the OnlyOffice draft API.

## 2026-08-25 — CRM association two-source rule (gitea #55, epic #52)

Goal: `bin/facts/prove-crm` (the issue references it as `bin/facts/crm`) must
prove person→company AND company→project as `root=facts` only when both
sources (corpus SoT `eslider/cv/projects/knowledge-mesh-seed.yaml` × ooCRM
`graph.json`) agree; one-sided ones are a mismatch, never a fact.

| Step | Status |
|------|--------|
| merge rule moved out of `main()` into `internal/facts.CRMAssocFacts` (single implementation, rule #10): person→company (companies_with_persons) + company→project (projects_contacts) | done |
| false-match fix: generic tokens (`client`, `gmbh`, `ag`, `systems`, `platform`, …) no longer drive the company match (`significantTokens` stopwords) — "Markets Platform (client)" no longer matches "Client not named yet" | done |
| dedup of mismatches (previously the employer-kind loop duplicated "not found") + report of CRM-only companies and of projects without a company | done |
| TDD offline on synthetic fixtures: two-source rule + all mismatch paths + generic-token regression, `-race` green | done |

Verification: `go test -race ./internal/facts/` green (crm/company-merge tests).
Against the real SoT (dry-run): proven=0; 9 corpus orgs are not in the CRM; 4 CRM-only
companies (Capitole, Client not named yet, Teksystems, talabat); 198 projects
without a company — `projects_contacts[].companies` in `graph.json` is empty for all
of them (the data is filled by the external `graph.py`, outside the repo). company→project is not provable
yet because of this data gap; person→company is not provable — CRM companies with persons do not
match the corpus orgs. Branch `feat/crm-assoc#55`.

## 2026-08-25 — facts lookup + contradiction-as-not-confirmed test plan (gitea #58, epic #52)

Goal: prove the `search → get → audit` operator flow on ONE known deal and the
contradiction-as-`(not confirmed)` path (D16), offline with synthetic fixtures.

| Step | Status |
|------|--------|
| offline test: `search` (fuse FTS+vector, root filter) → `get` (leaf by id → source/confidence) → `audit` (`facts.CheckFactRow` + `facts.Adjudicate`) confirms two-source facts on synthetic deal `acme-2026` (Alice/Bob/example.com) | done |
| offline test: 2v2 contradiction leaf (`a x b vs c x d`, no rule fires) stays `(not confirmed)`/hypothesis until audited; never a confirmed fact; deduction escalates | done |
| operator flow documented for bot/assistant (`search`/`get`/`audit`; confirmed vs `(not confirmed)`) — `docs/facts/audit-recipes.md` | done |

Verification: `go vet ./internal/facts/... ./internal/brain/rank/...` clean;
`go test -race ./internal/brain/rank/ ./internal/facts/` green
(`TestDealSearchGetAuditConfirmedFlow`, `TestDealContradictionNotConfirmedUntilAudited`).
Blocker #53 closed; a live `get` against `var/kb.lbug` + OnlyOffice CRM stays
out of scope (the offline plan proves the cgo-free logic). Branch `test/facts-lookup#58`.

## 2026-08-25 — facts-root ingestion: repair empty facts layer (gitea #181)

Goal: the production brain had **0 facts-root** leafs — all 9204 in `info`
(9201 confirmed). Root cause: `bin/brain/index.go --rebuild` was run without
`--with-facts`; `internal/brain/corpus.go` writes every corpus leaf as
`root=info` (single file source), and the 2-source facts layer is a separate
`bin/facts/extract.go` stage. Audit: all 9201 confirmed info leafs are
single-source corpus leafs → **0** qualify for re-rooting by the ≥2-sources rule.

| Step | Status |
|------|--------|
| `internal/brain` facts-promotion core: `SourceCount` (source string, `" x "` separator), `PromoteEligible` (≥2 sources + confirmed → facts), `PromoteFacts`/`EligibleInfoLeafs` (DB scan, idempotent upsert) | done |
| TDD tests: `TestSourceCount`, `TestPromoteEligible`, `TestPromoteFactsPromotesAndIdempotent`, `TestPromoteFactsReadPathDistinguishes` — `go test -race ./internal/brain/` green | done |
| `bin/facts/promote.go` CLI (`--dry-run`, `--db`); `bin/facts/extract.go --dry-run` = 18 two-source facts | done |
| live DB: writes blocked (var/ owned 1001, no sudo; in-service `/ingest` bulk write hung the C layer → container restart). Full ingestion demonstrated on a faithful copy: before facts=0 info=9204 → after facts=18 info=9204 | done |
| docs: `docs/facts/ingest.md` procedure + live runbook; `docs/brain/rebuild.md` facts note | done |

Verification: `go vet ./internal/brain/ ./bin/facts/` clean;
`go test -race ./internal/brain/ -run 'TestSourceCount|TestPromoteEligible|TestPromoteFacts'` green.
Live write to `var/kb.lbug` requires the brain stopped + DB-owner (uid 1001/root)
write access — procedure in `docs/facts/ingest.md`. Branch `feat/facts-ingest#181`.

## 2026-08-25 — mail/contacts import gap from /mnt/8TB (gitea #79)

Goal: close the import gap for the disk sources of mail/contacts on /mnt/8TB
that were not in the pipeline (#68/#74 imported only var/mail). Wave-1
(#84) already imported mbox/.eml: tb-andriy-profile 35,368 · tb-backup-128g
39,435 · tb-2010-zip 1,289 · contacts-eml 125 (Krylov excluded). This step —
move the mbox splitter into a testable layer + prove the import with tests.

| Step | Status |
|------|--------|
| source inventory confirmed on disk (read-only): TB profile Mail/ImapMail, both PSTs (265 KB), Backup E-Mails, zip-2010, VCF/MAB — table of truth in `docs/mail-sources.md` | done |
| mbox splitter moved out of the build-tag script into `internal/mailconv` (single implementation): `SplitMbox`, `SplitMboxDir`, `MboxMessage`, `LooksMbox`; `bin/mail/convert-mbox.go` — a thin wrapper | done |
| TDD: `TestSplitMbox`, `TestMboxMessage` (mbox→Message + source tag), `TestSplitMboxDirIdempotent` (repeat run without duplicates, content addressing sha256:16), `TestFromEMLFlatRoot` (contacts-eml flat root) — `go test -race ./internal/mailconv/` green | done |
| PST: `readpst` is NOT installed → the source stays BLOCKED; the closure path (libpst-utils → readpst -e → .eml → FromEML) is documented, not faked | done |
| docs: `docs/mail-sources.md` (table of truth), runbook section, no config/abs paths in the commits | done |

Verification: `go vet ./internal/mailconv/ ./bin/mail/` clean;
`go test -race ./internal/mailconv/ ./internal/source/ ./internal/mailsync/
./internal/canon/ ./pkg/contact/` green. Policy exclusions (#79): Krylov,
Drafts/Templates/Trash/Junk/Spam/Unsent, *.msf. The mail-send path was not touched.
Branch `feat/mail-import-gap#79`.

## 2026-08-25 — docs/skills: sync to the current agent toolset (gitea #81)

Goal: a fresh agent following AGENTS.md runs the sync wave, reconcile report,
interaction write — without asking in chat. Git ops truth: git = HTTPS+token,
tea CLI for issues/PR (the SSH deploy-key :222 is broken on write, #73 closed
obsoleted-by).

| Step | Status |
|------|--------|
| AGENTS.md: git-ops truth (#81/#73), go-onlyoffice split "primitives→lib, flow→2dph", Tools block with sync wave + reconcile + interaction, source map #79 + exclusion policy | done |
| skills: `api-client` — go-onlyoffice split; `etl-handler` — sync wave + OO flow | done |
| `docs/runbook.md` — section "Sync wave + OO CRM" with verified commands | done |
| verify: sync `--dry-run` prints the wave; reconcile/import-interaction `--help` and corpus scan work; OO write — only with env credentials | done |

Verification: `go run ./bin/stack/sync.go --dry-run` ok;
`go run -tags=onlyoffice_reconcile_contact ./bin/onlyoffice/reconcile-contact.go --help`
and `go run -tags=onlyoffice_import_interaction ./bin/onlyoffice/import-interaction.go --help`
ok (flags `--sources/--write/--limit` match the docs). Found during
verification: `--only` in `bin/stack/sync.go` accepts step names
(`mail,mail-import,chats,contact-brain,git-brain,contact-crm`), not logical
groups — the flag help line lies about it; documented as-is, bug — to the PO
(#81 comment). Branch `docs/skills-sync#81`.

## 2026-08-25 — HNSW Leaf_vec crash → in-process cosine scan (gitea #192)

Goal: liblbug 0.19.0 SIGSEGV on insert into the HNSW Leaf_vec (NULL deref in
simsimd_cos_f32, OnDiskHNSWIndex::shrinkForNode) once the graph exceeds ~1300 nodes — our
canon with ~28k leafs was under that crash (rc=139, silent restart). Port of the mdx-1 fix
(medex/brain-stack 6e0f379). Bug A (close/reopen race) is already closed for us by #109.

| Step | Status |
|------|--------|
| `rank/query.go`: VecStmt removed (comment with rationale) | done |
| `search.go`: queryVector → brute-force cosine scan over `l.embedding` (vecScanStmt), cosineToQuery + sqrt64; math/sort imports | done |
| `write.go`: EnsureIndexes creates only the FTS `id`; Leaf_vec is not created/required, an existing one is not dropped | done |
| TDD: `vector_test.go` — cosine known-vectors, zero-norm → 0 (not NaN), float64 elements, truncation | done — `zig go test -tags system_ladybug ./internal/brain/` green (except pre-existing modeldir env failures), vet clean, gofmt clean |
| reindex live kb.lbug (`bin/brain/index.go --rebuild`) — waits for PO confirmation | pending |
| PLAN.md block | done |

Verification: `bin/cgo/zig go test -tags system_ladybug -skip TestModelDir
./internal/brain/` ok; `go test ./internal/brain/rank/` ok. The pre-existing
`TestModelDir*` failures (the HF model is already local in models/, environment) are not
related to this change. Branch `fix/hnsw-cosine-scan#192`.

## 2026-08-26 — whole mail corpus in the brain (gitea #199)

Goal: both corpora — the live `var/corpus/mail` and the legacy `var/mail` (#184) — in the
brain; wave step `mail-index`. Decision B: `LoadMailLeafs` extended to
multiple roots, dedup by sha256 content address (the same mail in
both corpora → one leaf), without copying data.

| Step | Status |
|------|--------|
| `internal/brain/mail_corpus.go`: `MailRoots` + `LoadMailLeafs` multi-root + dedup; extracted from `corpus.go` (cgo-free) | done |
| `bin/brain/index.go --with-mail` → `brain.MailRoots(root)` | done |
| `bin/stack/sync.go`: `mail-index` step (after mail-import, `--with-mail`), brain runner with the Zig CGO tags `brain_index` | done |
| TDD: `mail_corpus_test.go` (multi-root, dedup, since, limit, attachment) + `sync_test.go` (mail-index plan, runner tags) | done — `go test -race ./internal/brain/... ./bin/stack/... ./internal/mailconv/...` green |
| Full index `index.go --skip --with-mail` into the existing kb.lbug | done — 304 544 leafs, +7 712 new (the previous index was partial after corrupt-199); total info 313 786 |
| Idempotency (repeat `--skip --with-mail`) | done — 0 new |
| Search of real mails (e2e-outbound, PST subject) | done — count ≥ 1 |
| docs: AGENTS.md / runbook `--only` list + `--with-mail` | done |
| Known limitation: vector search is a linear scan (#192), ~32s at 313k leafs | open — separate issue for the optimization |

Branch `feat/mail-corpus-brain#199`, PR #200 → release/v1.

## 2026-08-25 — Outlook PST import: readpst -e → .eml → pipeline (gitea #185)

Goal: close the remainder of #79 — two PST files (Andriy + a copy in the 128Gb backup).
`readpst -e` → a set of `.eml` → the existing `mailconv.FromEML` (source tag `pst/`).
Converter by PO decision: readpst from the `pst-utils` package (Ubuntu 24.04; formerly
`libpst-utils`).

| Step | Status |
|------|--------|
| readpst on the host: the system install `apt-get install pst-utils` is blocked (no passwordless sudo) — installed locally into `var/dist/readpst` (dpkg-deb -x, `pst.readpst` in config.local.yml) | done (blocker recorded in the issue) |
| `internal/source/pst.go`: adapter `source.PST` (#97 pattern) — `readpst -e` into a wipeable scratch (`var/tmp/pst/<label>`), policy #79 via `mailconv.SkipFolder` (German names: Entwürfe/Vorlagen/Gelöschte Objekte/Junk-E-Mail/Postausgang), content-addressed copy into the corpus `Out/<label>/<folder>/<sha256:16>/<sha256:16>.eml`; `ImportPST`/`PlanPST` (sync + `mailconv.FromEML`), `--dry-run` plan | done |
| normalization of the `LibPST-iamunique-<n>` MIME boundary (readpst injects a random token on every run) — otherwise content IDs are unstable and a repeat run breeds duplicates | done (found on real data, TDD test) |
| `bin/mail/import-pst.go`: thin CLI wrapper (flaggy, pst.* config, ctx-first, idempotency, state `var/state/pst.json`) | done |
| config: `Config.PST` + `pst:` section in `etc/brain/config.yml` (sources/readpst/out/state; missing sources = explicit error pointing at the config) | done |
| TDD offline (synthetic Alice/Bob/example.com, fake readpst via a test-seam hook): .eml→canon conversion (folder=Posteingang), idempotent repeat (0 duplicates), state checkpoint, dry-run, policy exclusions, resolveReadPST (override→PATH→var/dist→error), identical archives (dedup by seen-set) | done — `go test -race ./internal/source/ ./internal/mailconv/ ./internal/config/` green |
| Run of the real PSTs (both files are byte-identical, sha256 matches): 1 unique mail (Outlook greeting 2005, `Posteingang`) × 2 labels; repeat run new=0 skipped=2, no duplicates; readpst also extracted 78 VCF + 6 ICS (imported by other pipelines, #68) | done |
| docs: `docs/mail-sources.md` (PST statuses → imported, PST import section), PLAN.md (this block) | done |

Verification: `go build ./...` (except pre-existing build-tag packages bin/{reasoner,onlyoffice,postgres} — main without a tag), `go vet ./...`, `go test -race ./...` green; gofmt clean (shebang files excluded). Corpus: `var/corpus/mail/pst/{pst-andriy,pst-backup-128g-vorlagen}` + state `var/state/pst.json` (1 seen-id). Branch `feat/pst-import#185`.

## 2026-09-02 — D-1.3: import gator wheregroup → graph (gitea #260, epic #257)

Goal: pilot import of the **wheregroup** channel (3984 mails, gator canon kind=mail)
into kb.lbug as Message/Person nodes + SENT/TO/CC/BCC/REPLY_TO edges per the write path
of D-1.2 (#259) and the connector of D-1.1 (#258). Prove the link "gator canon → 2dph
graph" before gmail (D-1.4 #261). The read path is untouched; the var/mail raw data is not read.

| Item | Status |
|------|--------|
| `pkg/duckdb`: `Exec`/`QueryRows` — arbitrary SELECT over read_parquet as JSON-compatible rows (gator pattern; LIST(STRUCT)→[]any from map, DECIMAL→float64) | done |
| `internal/mailgraph` (cgo-free): typed Row (message_id/folder/from/to/cc/bcc/in_reply_to/subject/date/body/content_hash), `MapRow`/`MapRows` (email lowercase, date RFC3339/TIMESTAMP, NULL lists), `ReadSQL` (latest per message_id by observed_at + tombstone deleted excluded + channel), `ResolveThreads` (thread_id = root of the in_reply_to chain, dangling → external root, cycle → fallback), `SortByDate` (parents before replies), `ToInputs` → `brain.MessageInput`, `ComputeStats` (folder layout/REPLY_TO/unique emails) | done |
| config: `Config.Gator.MailHive` (`gator.mailhive`, template etc/brain/config.yml + config.local.yml, flag `--hive` / env `GATOR_MAIL_HIVE`) | done |
| `bin/mail/graph.go` (cgo+system_ladybug+mail_graph): `--channel` + dry-run by default / `--commit`, `--db`, `--skip-existing` (incremental), live-holder guard (--force), batch transactions, nodes/edges report after the write | done |
| TDD: cgo-free unit (MapRow null/register/date formats, Input/gator_ref `kind=mail#v-<hash8>`, SortByDate, ResolveThreads root/dangling/cycle, Stats order-independent, ReadSQL latest/tombstone/escaping) + pkg/duckdb roundtrip parquet | done — green |
| cgo integration (temp Ladybug + synthetic hive parquet): full cycle → nodes/edges, repeat = 0 duplicates, `--skip-existing` = 0 new, dangling REPLY_TO is not created | done — `TestImportCycle` (gcc ladybug+duckdb; CI step added, zig duckdb ABI does not hold) |
| live run wheregroup 3984 → `var/kb.lbug` (backup `kb.lbug.pre-d13-260.bak`, compose brain stopped for the write) | done — Message 0→3984, Person 0→167, edges SENT 3984 / TO 4855 / CC 302 / BCC 2 / REPLY_TO 639 (after the repeat; the first run 630 — 9 edges were created later, parent later than the reply by date) |
| the repeat run is idempotent; `--skip-existing` = 0 written | done |
| spot check: folder INBOX 3143 / Sent 479 / INBOX/Unmatched 362 (cross-check with gator), Person andriy.oblivantsev@wheregroup.com, gator_ref `kind=mail#v-<hash8>`, REPLY_TO chains, thread_id filled | done |

Verification: `go vet ./internal/...` + `go test -race ./internal/mailgraph/ ./internal/config/ ./pkg/duckdb/ ./internal/canon/` green; cgo `go test -tags system_ladybug ./internal/mailgraph/` green (gcc); CLI dry-run/commit/repeat on live. Branch `feat/graph-import-wheregroup#260` off `main`.

## 2026-09-03 — N-1.1: "people/companies vs service accounts" filter for accept networks (gitea #268, epic #267)

Goal: a deterministic classifier of a network address (L-9.5 #234) —
`person | company | service` — so that the CRM export (`--accept-only`) does not pull
service accounts/subsystems/mailing lists (GitLab/PayPal/LinkedIn/
markets-platform/chiliproject@trac), while real people and company context
get through. Basis — the sender heuristics of `mailconv.IsMachineSender`
(junkDomains/junkLocalParts). Rules: docs/brain/crm-network-filter.md.

| Item | Status |
|------|--------|
| `internal/mailconv/classify.go` (cgo-free): `ClassifySender(ParsedAddress)` → kind; cascade: human relays (linkedin hit-reply/inmail-hit-reply) → person; mailconv junk domains (shared `isJunkDomain`, one implementation) + network service domains (gitlab/paypal/markets-platform/xing/djinni/wellfound/…); local automata (noreply/mailer-daemon/robot/mailing lists/security); tool names of subsystems (chiliproject/projeqtor/trac/wiki/…) in the local and left domain label; `*-request`/`*-owner`/`lists.*` lists; name heuristic (Title/ALLCAPS/initials/particles, org/role words); fallback first.last@domain; otherwise company | done |
| `internal/network`: field `Link.Kind` in BuildLinks (additive, does not break the #234 API) | done |
| `bin/network`: `--exclude-services` + `--accept-only` excludes kind=service; kind in text/YAML/JSON | done |
| TDD: classifier unit (domains/patterns/boundaries noreply@people, robots.txt in the name, relays) + network (Kind is set, services are not in the accept export) | done — green |
| Live pilot on kb.lbug (read-only): wheregroup top-60 accept = 44 person / 5 company / 11 service; eslider@ 1240 accept = 317 person / 494 company / 429 service; top services of eslider@ and chiliproject@trac — service, Astrid Emde/Ruby Rodriguez/Celine Schlindwein — person | done (table on #268) |
| Documentation: docs/brain/crm-network-filter.md (rules/OPEN boundaries) + graph-network.md + PLAN.md | done |

Verification: `go test ./internal/mailconv/ ./internal/network/` + `go vet ./...`
green; cgo `bin/cgo/zig go vet -tags system_ladybug,network_cli ./bin/network/`
green. Branch `feat/network-human-filter#268` off `main`.

## 2026-09-03 — N-1.2: OO connector: import the network accept manifest into the CRM (gitea #269, epic #267)

Goal: a write tool — the manifest of accept links (bin/network --accept-only,
network.Manifest) → a Person contact in the OnlyOffice CRM (email/name/source tag
`2dph:network:<target>`/about with a summary + premises), idempotent by email;
service accounts (N-1.1 #268) skipped; dry-run/report-only writes nothing.

| Item | Status |
|------|--------|
| `internal/network/manifest.go`: type `network.Manifest` (crmDoc/crmLink from bin/network, L-9.5 #234) — the shared contract of producer and connector; bin/network prints it | done |
| `internal/ooimport` (cgo-free): `ParseManifest` → `BuildPlan` (person/company→Person mapping: name via mailconv.SplitPersonName, fallback the local part of the email; kind=service/no email → skipped; default tag `2dph:network:<target-email>`; about: `{msgs} mails / {threads} threads / {replies} replies, period {period}; premises: mail/commit … (+N more); source: <manifest>`) → `Run` (BuildContactEmailIndex one pass → matched skip / create: CreatePerson + AddContactInfo email primary + CreateContactTag/AddContactTag; rollback of what was created on an email/tag failure; dry-run/limit) | done |
| CLI `bin/onlyoffice/import-network.go` (shebang, tag onlyoffice_import_network): `--manifest <file|->` (required), `--dry-run`/`--write` (mutually exclusive), `--limit N`, `--tag`; report only by default; report created/matched/skipped/failed/pending | done |
| Mapping decisions: linkedin human relays (hit-reply@linkedin.com, real display name) — Person as-is (email=relay, name=person); role mailboxes/companies (info@/alle@, kind=company) — Person with a fallback name (company grouping — N-1.4 #271); kind=service is not included | done (doc: docs/brain/oo-network-import.md) |
| TDD offline: ParseManifest (+producer-consumer yaml roundtrip), BuildPlan (name/email lowercase/tag/exact about string/sort/skip), splitByExisting (repeat = 0 new) | done — green |
| Integration with live OO (`//go:build integration`, creds ONLYOFFICE_URL/USER/PASS, without creds — skip): dry-run writes nothing → write creates a contact email/tag/about → repeat = 0 new/all matched; --limit creates a part, the catch-up run — the rest; the test contact is deleted in t.Cleanup (the tag stays at 0-count — the API does not delete tags) | done — green on office.produktor.io |
| Documentation: docs/brain/oo-network-import.md (mapping/commands/boundaries) + graph-network.md §4 + PLAN.md | done |

Verification: `go test ./internal/network/ ./internal/mailconv/ ./internal/ooimport/`
+ `go vet ./...` green; integration `go test -tags=integration ./internal/ooimport/`
green (live OO); CLI dry-run on a fixture — report-only, writes nothing.
Branch `feat/oo-crm-connector#269` off `main`.

## 2026-09-03 — N-1.4: company grouping by domain + appending tag/about to matched contacts (gitea #271, epic #267)

Goal: following the N-1.3 pilot (#270), bring the wheregroup import up to a
"business contact" structure: (1) group role mailboxes/companies of wheregroup under
a company by the email domain; (2) for existing matched contacts (32 from #270,
incl. the top-5 from #85) **additively** append the tag `2dph:network:<target>` +
an about-premises summary (owner decision #267); (3) dedup — report only
(the pilot found no real duplicates, multi-addresses are YAGNI for the epic).

| Item | Status |
|------|--------|
| `internal/ooimport`: field `Contact.Kind` (manifest link class person/company; service filtered out) — the basis for grouping | done |
| `company.go`: `CompanyRule` (domain→company), `ParseCompanies` (YAML, validation: domain with a dot, not an email, no repeats; sort), `EmailDomain`, `GroupCompanyLinks` (only kind=company → company by domain; person-kind untouched; company without a rule — unmapped; two domains → one company collapse) | done |
| `RunCompanies` (write/dry-run): FindCompany → if missing CreateCompany → GetContact + UpdatePerson with companyId; repeat = 0 changes (found/already); role mailbox not in the CRM — notfound | done |
| `backfill.go`: `BackfillChanges` (tag if absent; about summary only if about is empty/whitespace — we do not overwrite manual edits), `RunBackfill` (hasTag via ListContactsByTag, about/names GetContact; dry-run Would; CreateContactTag idempotent; repeat = 0 Updated) | done |
| CLI `import-network.go`: flags `--backfill`, `--companies <yaml|->` (after Run; both idempotent; reports updated/would/unchanged + found/created/linked/already/unmapped) | done |
| Cosmetics: `mailconv.SplitPersonName` strips the trailing " (Org)" decoration ("Astrid Emde (FOSSGIS e.V.)" → Astrid/Emde; pilot #270: given "Astrid Emde (FOSSGIS"/family "e.V.)") | done |
| TDD offline: mailconv SplitPersonName (decoration/none/nested/all-in-parentheses → fallback), ParseCompanies (sort/trim/errors), GroupCompanyLinks (role mailboxes/sort/unmapped/person-kind untouched), BackfillChanges (additivity: manual about intact, whitespace about — summary) | done — green |
| Integration with live OO (`//go:build integration`, without creds — skip): role mailbox → company created+linked → repeat already=1; matched: tag+about appended, manual about B not overwritten, repeat = 0 Updated | done (office.produktor.io) |
| Documentation: docs/brain/oo-network-import.md (N-1.4: --backfill/--companies, boundaries: person-kind not linked, mapping Name = CRM company display name) + PLAN.md | done |

Verification: `go test ./internal/mailconv/ ./internal/ooimport/ ./internal/network/`
+ `go vet ./...` green; integration `go test -tags=integration ./internal/ooimport/`
green (live OO); CLI `go vet -tags=onlyoffice_import_network ./bin/onlyoffice/`.
Dedup: the pilot found no real Person/company duplicates — merge was not
applied (report only; rule #271 "strictly per explicit list").
Branch `feat/oo-crm-company#271` off `main`.

## 2026-09-03 — P-9.5: service clients over the read contract (gitea #241, epic #239)

Goal: a single client-side layer for reading the brain — a typed client over the read contract
(P-9.4 #240) instead of every consumer calling bin/brain/HTTP/MCP
its own way; plus the "facts gate" (the client does not serve not-confirmed as confirmed).
The client reads only through the contract (HTTP :8630), never opens kb.lbug directly.

| Item | Status |
|------|--------|
| `pkg/brainclient` (cgo-free): `Client` (Config base/token/timeout, transport) + typed `Search`/`Get`/`Stats`/`Audit` → `internal/contract` responses; responses are validated by the contract validators (format + contract_version; an incompatible service is an error) | done |
| Facts gate: `Gate(root, confidence)` (confirmed only facts+confirmed/legacy-empty), `Client.Facts` (search --root facts → confirmed[] / not_confirmed[] with reason), `GateAudit` (histogram of the facts root; not_confirmed>0 = violation). 2v2 contradictions (hypothesis, D16) never land in confirmed | done |
| CLI `bin/brain/client.go` (shebang, brain_client, cgo-free): search/get/stats/audit, --json/--root facts\|info/--as-of/-n/--no-web/--base/--token; implementation in `pkg/brainclient/cli` (Run, covered by tests); audit with not_confirmed>0 → exit 1 | done |
| TDD: gate tables (facts+confirmed/legacy vs hypothesis/partial/info), GateSearch/GateAudit on fixtures; httptest client (typed decode, validation, HTTP errors, auth header, root=facts parameter); CLI wiring (flags, JSON shapes, exit codes) | done — `go test ./pkg/brainclient/...` green |
| Live on :8630 (db 105220 leafs): stats/audit (facts=21 confirmed, not_confirmed=0, exit 0), search --root facts (onlyoffice postgres → 1 confirmed), get --body (source "docker ps x compose:..."), info hits in default mode marked (not confirmed) | done |
| Documentation: docs/brain/read-contract.md §"Client layer (P-9.5)" (SDK/CLI, gate, examples search-fact + audit card, cross-repo path via P-9.6) + skills/brain/SKILL.md §"Read through the contract" + PLAN.md | done |

Cross-repo (gator/cv): moving the read contract and the client into a public go-* module —
P-9.6 (ADR, epic #239); until then external consumers read by the formats of
read-contract.md via HTTP/MCP/CLI.

Verification: `go vet ./...` + `go test ./pkg/brainclient/...` green
(CLI logic in pkg/brainclient/cli is part of the regular ./...; bin/brain — shebang
mains behind exclusive tags, per D14 compiled file by file) —
bin/brain/client.go is built and run with `go run -tags=brain_client`;
`./bin/facts/audit.go self` ok. Branch `feat/brain-client#241` off `main`.
