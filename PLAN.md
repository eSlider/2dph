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
| D26 | repo layout | `bin/{subject}` = tools only (`{verb}-{object}.go`); executables/orchestration → `scripts/`; data tree `var/{tmp,dist,log,state,cache,hf,kb.lbug,corpus/{git,mail,chats,docs}}`; configs `etc/{subject}` + central `etc/brain/config.yml`. CI guard fails on absolute paths and Python remnants. [#89](https://git.produktor.io/eSlider/2dph/issues/89) |
| D27 | config system | All external sources (paths, URLs, endpoints) via `github.com/eslider/go-config` (`internal/config`): stack `etc/brain/config.yml` → `config.local.yml` → `.env` → process env; one load per process into a strict typed struct; deep merge (maps recurse, scalars last-write-wins, keys lower+alnum); no ad-hoc `os.Getenv`; legacy `KB_*` / `BRAIN_SEARCH_*` env names honored transitionally. [#90](https://git.produktor.io/eSlider/2dph/issues/90), [#91](https://git.produktor.io/eSlider/2dph/issues/91) |
| D28 | no hardcoded paths/URLs | Nothing like `/mnt/…`, `/home/<user>/…`, or pinned host URLs in code/comments/tests; missing config key = explicit error pointing at `etc/brain/config.yml`. CI-enforced. [#89](https://git.produktor.io/eSlider/2dph/issues/89) |
| D29 | typed code | Static typing first: no `map[string]any` at boundaries; loose→strict via go-viper/mapstructure/v2; primitives only in `pkg/utils/*.go`. Hotspots listed in [#92](https://git.produktor.io/eSlider/2dph/issues/92). |
| D30 | owned repos | All `github.com/eSlider/go-*` are owned: Gitea `git.produktor.io/eSlider/*` = working home (issues/releases only there), GitHub = push-mirror; module paths stay `github.com/eSlider/go-*`. Shared template: subpackage-per-domain. Gitea fetch via `replace` in `go.mod`: `go-config@v0.4.0`, `go-onlyoffice@v0.14.0` → `git.produktor.io/eSlider/*` (go.sum carries Gitea hashes only; zero `github.com/eSlider/go-*` downloads). [#101](https://git.produktor.io/eSlider/2dph/issues/101) |
| D31 | concurrency | Public APIs synchronous; ctx-first IO; bounded pools; wg-accounted goroutines; sender closes channels; errgroup for fan-out/fan-in; backpressure over unbounded queues; graceful shutdown; `go test -race` in CI. [#94](https://git.produktor.io/eSlider/2dph/issues/94) |
| D32 | zero-alloc hot paths | Pre-allocation, buffer reuse (`buf[:0]`), `sync.Pool` with reset, `strings.Builder` over `+`, sets as `map[K]struct{}`, structs over maps; proven by `-benchmem`. Applies to hot paths only — no premature optimization. |
| D33 | sync-ETL reimplementation | Pipeline `Source.Fetch(ctx,cursor)→[]Blob→Registry.Decode→Transform→Load`; one Handler per format (eml=emersion/go-message `Walk()`, zip=`archive/zip`, html=`x/net/html` optional, pdf=Ghostscript normalize→pdftotext/tesseract); lazy children, walker safety limits; single implementation per transformer; canonical `Conversation` model on disk (`var/corpus`) → brain graph (`SENT/TO/CC/BCC/REPLY_TO/PART_OF`); URL addressing `scheme://platform/path[#anchor]` with `sha256(URL)[:16]` node IDs. Epic [#88](https://git.produktor.io/eSlider/2dph/issues/88): [#95](https://git.produktor.io/eSlider/2dph/issues/95)–[#102](https://git.produktor.io/eSlider/2dph/issues/102). |
| D34 | synapse edges | User-defined edges between neurons modeled as a `SYNAPTIC (FROM Leaf TO Leaf, type STRING)` rel table (leafs = neurons, edges = synapses); adjacency = both directions; path = `[:SYNAPTIC* ALL SHORTEST 1..N]` (Ladybug exposes only intermediate nodes → rebuild `[from, mid…, to]`). Service `bin/brain/synapse-matrix.go`, auth = token OR loopback bind, config section `synapse`. [#82](https://git.produktor.io/eSlider/2dph/issues/82) |

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
`Leaf(embedding FLOAT[N])` — FTS on `text`, HNSW vector index on `embedding`.
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
## 2026-08-22 — mailsync v1 мелкие фиксы (gitea #111, epic #88)

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

## 2026-08-23 — ETL-реестр обработчиков + безопасный walker (gitea #96, epic #88)

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

## 2026-08-23 — URL-адресация контента, селекторы, гранулярный сплит (gitea #100)

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

## 2026-08-23 — secret-scan в CI всех репо (gitea #142)

Goal: не допустить новых утечек секретов на этапе PR/push (после #140 — 8 HIGH-файлов в matrix).

| Step | Status |
|------|--------|
| Выбор инструмента: **gitleaks** (официальный образ `zricethezav/gitleaks`) — diff/commit-скан, fail-on-leak, generic+private-key правила | done |
| CI job `secret-scan` (pull_request + push, сканирует только diff новых коммитов) в 2dph (ci.yml), go-onlyoffice (test.yml), go-config (secret-scan.yml), matrix (.gitea/workflows/secret-scan.yml) | done — PR 2dph#143, go-onlyoffice#5, go-config#1, matrix#6 |
| git pre-push + pre-commit хуки `scripts/githooks/` (+ install.sh), блокируют push/commit при находке | done |
| Скилы git-workflow + devops: правило «скан перед любым push»; синхронизированы в ai-fabric (release/v2) | done |
| Верификация: скан ловит реальные утечки matrix #140 (14 findings, exit 1); pre-push блокирует push с секретом | done |

## 2026-08-23 — Conversation-канон: модель + диск + манифест + граф (gitea #99, epic #88)

Goal: единая каноническая модель для почты и чатов — `Message{ID,ThreadID,Platform,
From,ReplyTo,To,CC,BCC,SentAt,Body,Attachments(lazy)}`; хранение на диске под
`var/corpus/{mail,chats}` как JSON + манифест sha256 (идемпотентный upsert); граф
`(:Person)-[:SENT]->(:Message)-[:TO|CC|BCC]->(:Person)`, `[:REPLY_TO]`, `[:PART_OF]`
параграфы. Первый mergeable инкремент: ядро схемы + диск + манифест + round-trip
и golden-тест (один `.eml` и один telegram-экспорт дают одинаковую схему графа).

| Step | Status |
|------|--------|
| `internal/canon` (evidence-слой, пакет-домен): `Person`/`Attachment`(lazy `Open(ctx)`, без буферизации тела)/`Message`/`Edge`/`EdgeType` (SENT/TO/CC/BCC/REPLY_TO/PART_OF), `Edges()`, `GraphSchema()` | done |
| `Store{root}`: `Write` (идемпотентный upsert по sha256 тела), `Read` (скипает битые файлы), `LoadManifest`/`SaveManifest` (атомарный); layout `root/<platform>/<thread>/<id>.json`; сегменты путей через `pkg/utils.SafeSegment` | done |
| `FromMail(r)` на emersion/go-message: envelope (From/To/Cc/Bcc через mail.Header.AddressList), ThreadID из References/Message-Id, ReplyTo из In-Reply-To, тело text/plain (fallback text/html → mailconv.StripHTML), вложения lazy (метаданные) | done |
| `FromChat(platform,thread,text,from,ts,replyTo)`: Person id = `platform:handle`, парсинг ts (RFC3339), ReplyTo-цепочка | done |
| TDD (offline, фикстуры synthetic Alice/Bob/example.com): round-trip write→read→canon для mail и chat; идемпотентный повторный write; манифест = sha256(JSON); layout; Edges/схема; golden — mail и telegram дают одинаковую `GraphSchema()` | done — 13 тестов, `-race` зелёные |
| docs/PLAN.md: секция #99 (этот блок) | done |

Осталось (вне первого инкремента): конверторы существующих источников (gmail/onlyoffice
`mailconv.Message`, `mailsync.Message`, whatsapp/linkedin экспорт) → канон; запись графа
в Ladybug (`(:Person)/:Message/:Paragraph` узлы + `SENT|TO|CC|BCC|REPLY_TO|PART_OF`
рёбра) — будет отдельным инкрементом на #99; миграция `bin/*/import` на канон.

Verification: `go build ./...` green; `go vet ./internal/canon/... ./pkg/utils/...`
clean; `go test -race ./internal/canon/... ./pkg/utils/...` green. Branch
`feat/conversation-canon#99` off `release/v1`.

## 2026-08-25 — OO Mail: HTML-шаблон + render-check до отправки (gitea #76)

Goal: из wave-1 письма уходили plain-text одной строкой; теперь брендированный
HTML-шаблон + гейт рендер-проверки, блокирующий send, пока черновик не пройдёт
assert (абзацы, подпись, logo cid, chat-line без дублей).

| Step | Status |
|------|--------|
| `internal/oohtml` (новый пакет-домен): брендированный шаблон produktor.io (палитра #76: cream `#FAF5EA`, ink `#0A0A0A`, navy `#142039`/`#0E1626`, CTA `#F2C849`, primary `#143A6F`; system-ui; inline-стили; логотип `cid:produktor-logo.gif` без remote-URL) | done |
| `RenderCheck(html, TemplateData)` — типизированный ассерт: paragraphs ≥2, greeting, offer+CTA, signature+site, logo cid, отсутствие дублей текстовых блоков (`Code` enum) | done |
| `Mailer.Send(ctx, SendParams)` — TDD-гейт: `Build` → `SaveMailDraft`(HTML) → `GetMailMessage` back → `RenderCheck` → `SendMail` только при чистой проверке; иначе error «render check failed, send blocked» | done |
| `BuildMessage(w, …)` — MIME `multipart/related` через `emersion/go-message`: HTML-часть + inline `image/gif` с `Content-ID` = логотип (cid-embedding, не remote fetch) | done |
| Лого-ассеты `etc/onlyoffice/assets/produktor-logo.{png,gif,-64.gif}` | готовы |
| TDD offline (фикстуры synthetic Alice/Bob/example.com; fakeStore вместо сети): 10 тестов, `-race` зелёные | done |

Verification: `go vet ./...` clean; `go test -race ./internal/oohtml/` green (10/10).
Branch `feat/ooo-mail-html-template#76` off `release/v1`. Осталось вне объёма:
wiring в OnlyOffice CLI (`oo mails send`) и фактическая отправка с вложенным
cid-логотипом через драфт-API OnlyOffice.
