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
[#102](https://git.produktor.io/eSlider/2dph/issues/102) gs PDF normalize in.
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
| D26 | typed config | `internal/config` (dep `github.com/eslider/go-config`). Load stack `etc/brain/config.yml → config.local.yml → .env → process env`, deep merge (maps recurse, scalars last-write-wins, keys lower+alnum). Services read the typed `Config`; legacy `KB_*` / `BRAIN_SEARCH_*` env names mapped transitionally. [#90](https://git.produktor.io/eSlider/2dph/issues/90). |

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
  user `onlyoffice`, db `onlyoffice`, `password_env_file`) + example profiles.
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
   `onlyoffice,gmail`, 300s): sync → import on `new>0`; full rebuild only if
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
| 3 | [#18](https://git.produktor.io/eSlider/2dph/issues/18) | **in** — `--with-facts` / `--facts-json` land `root=facts`; `--with-chats` indexes `var/corpus/chats/md`. WhatsApp sync is out of v1. |
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
