---
type: howto
status: current
related:
  - docs/README.md
  - PLAN.md
---

# Run 2dph (portable)

No laptop-absolute paths. Config lives in env files under `$HOME/.config/brain/`
(mode 0600), not in git.

## Toolchain

- Go (see `go.mod`)
- Optional: Docker, Zig CGO via `bin/cgo/zig` (not gcc)
- Optional: poppler (`pdftotext`/`pdftoppm`) + tesseract `eng+deu` for mail OCR
- Optional: ghostscript `gs` — normalizes export-locked / oversized PDFs before
  extraction (strips export-protection, shrinks; original preserved, artifact
  in `var/tmp`). Clean PDFs skip it, so the pdftotext fast path stays fast.
- Optional: **liteparse service** (epic #219) — documents → structured
  JSON/YAML. `docker compose up -d liteparse` (daemon with warmup), invoked via
  `docker compose exec -T liteparse lit parse ...` (~100ms vs ~700ms for
  `docker run`). Tools: `bin/research/convert.go` (→ `var/struct-data/<sha256>.yml`,
  idempotent), `bin/research/bench.go` (p95). Details: `docs/research-liteparse.md`.

```bash
eval "$(bin/cgo/zig env)"   # optional; bin/brain/{search,get,stats,eval,serve}.go auto-wrap zig
go test ./...
```

## Docker images and tags

`ghcr.io/eslider/2dph` is the API image (Go `serve` + Ladybug, Zig CGO) and the
supported way to run 2dph away from the repo checkout. One platform only:
**`linux/amd64`** — no arm64, no multi-arch.

| Tag | What it is | Use |
|-----|------------|-----|
| `vX.Y.Z` | one stable release; treat as immutable | **production** — pin it |
| `latest` | last stable release (moves on each release) | trying it out, demos |
| `api` | edge build of `main` (moves on every merge) | tests only |
| `sha-<12>` | one commit of `main` (first 12 chars of the SHA) | reproduce / bisect |

- Production pins `vX.Y.Z` — neither `api` (it can be broken) nor `latest`
  (an unrelated `docker pull` would upgrade you).
- Upgrade = change the tag, `docker pull`, restart the container. A rollback is
  the previous `vX.Y.Z` — with `latest`/`api` there is nothing to roll back to.
- On a release the same build also carries `api`, so right after a release the
  edge tag points at the released commit; the next merge into `main` moves it
  forward.

### Run

```bash
docker pull ghcr.io/eslider/2dph:vX.Y.Z

docker run --rm -p 127.0.0.1:8630:8630 \
  -v "$PWD/var:/data/var" \
  -e HOME=/data -e KB_HOST=0.0.0.0 -e KBSEARCH_NO_DAEMON=1 \
  --user "$(id -u):$(id -g)" \
  ghcr.io/eslider/2dph:vX.Y.Z serve

curl -sS http://127.0.0.1:8630/health   # {"status":"ok"}
```

Then `GET /openapi.json`, `POST /mcp`, MCP URL `http://127.0.0.1:8630/mcp`.

- `KB_HOST=0.0.0.0` is **required** for a published port: `serve` binds
  `127.0.0.1` by default, so `-p` alone would leave the port unreachable.
- `--user "$(id -u):$(id -g)"` is the CLI equivalent of `KB_UID` in
  `compose.yaml` (#195): `kb.lbug` is opened read-write, so the container uid
  must own the mounted `var/`. Without it (image uid 1001, host `var/` owned by
  uid 1000) the container exits with `OpenDatabase: failed to open database`.
- The image carries no `kb.lbug` and no `etc/` (#208). Mount a `var/` that was
  built by `bin/brain/index.go --rebuild`; an empty `var/` starts healthy but
  with 0 leafs (`/stats` fails with `Table Leaf does not exist` until a DB
  exists). Config, if any, comes from `/data/etc/brain` — `compose.yaml` mounts
  the repo copy read-only.
- Entrypoint verbs: `serve` (default), `search`, `watch`, `index`, `add`,
  `ann`, `mail-graph`, `mail-leaf`, `index-loop`, `mail-sync`, `runner`,
  `shell`.

### Which version is in an image

The image has no `version` subcommand today (the verbs are listed above), so the
release version is read from the standard OCI labels, stamped by the release
pipeline (#9):

```bash
# local image
docker image inspect ghcr.io/eslider/2dph:vX.Y.Z \
  --format '{{index .Config.Labels "org.opencontainers.image.version"}}'

# registry, without pulling
docker buildx imagetools inspect ghcr.io/eslider/2dph:vX.Y.Z \
  --format '{{json .Image.Config.Labels}}'
```

Labels: `org.opencontainers.image.version` (the release, e.g. `v0.24.13`),
`.revision` (the commit SHA), `.source` (repo URL), `.created`. The label must
equal the tag. An empty `version` means the image predates the label job
(today's `api` and `sha-*` images): identify those by `revision`/digest, and
prefer a released tag where one exists.

### How releases are cut

- Releases are **manual**: a `workflow_dispatch` run (input: the line, e.g.
  `0.24`) builds, smoke-tests (`/health`, `/openapi.json`, MCP `tools/list`) and
  pushes `vX.Y.Z` + `latest` + `api` + `sha-<12>` in that single run (#9).
- A push/merge to `main` publishes **only** `api` + `sha-<12>`; it never creates
  a release tag.
- Until the first manual release has run, GHCR has only `api` and `sha-*` — that
  is expected, not a broken pull.
- A dispatch with an explicit `version` re-points an existing digest to
  `vX.Y.Z` + `latest` via `docker buildx imagetools create` (no rebuild, #9).

## Config

| File / env | Purpose |
|------------|---------|
| `$BRAIN_SEARCH_ENV` (default `$HOME/.config/brain/search.env`) | `BRAIN_SEARCH_URL` (SearXNG). Optional Basic Auth. |
| `$HOME/.config/brain/db-profiles.yml` | read-only Postgres profiles (OnlyOffice via tunnel) |

If the host already runs SearXNG, point `BRAIN_SEARCH_URL` at it. Do not start
a second copy (D3). Optional Compose instance:

```bash
SEARXNG_SECRET=$(openssl rand -hex 32) docker compose --profile searxng up -d
```

That binds `127.0.0.1:8888`. JSON format must stay enabled.

## Index then search

Write path is `bin/brain/add.go` for a leaf (or `POST /ingest`). Bulk
corpus rebuild remains `bin/brain/index.go --rebuild` (Compose profile
`index`). Do not DROP INDEX on Ladybug 0.19.

```bash
bin/brain/add.go --text "arc-1 runs Matrix" --root facts --source "compose.yml x docker ps"
bin/brain/index.go --rebuild --with-facts --with-chats
bin/brain/search.go "LadybugDB vector index"     # facts → info → web (D17)
bin/brain/search.go "upstream flag" --no-web
bin/brain/search.go "who works where" --as-of 2025-01-01  # D24 intervals
source <(./bin/shell/complete.go bash)            # D23 flaggy complete
bin/brain/get.go <id> --body
bin/brain/stats.go
```

`--hop N` walks File → Commit → Person from each hit. `--as-of YYYY-MM-DD`
keeps leafs whose `valid_from`/`valid_to` cover that day (empty interval =
legacy always-on). Empty web results are `throttled`, not absence.
Gap to v1: [roadmap](roadmap.md) / [epic #16](https://git.produktor.io/eSlider/2dph/issues/16).

Ladybug 0.19: never `DROP INDEX` FTS/VECTOR (ghost catalog). Fresh indexes =
delete `var/kb.lbug` then `--rebuild`.

## HTTP / MCP

```bash
scripts/stack/start                 # brain :8630, wait until MCP search/get/audit
scripts/stack/status                # YAML: brain / reasoner / picoclaw / mail_sync
scripts/stack/start-mail-sync       # compose ETL: OO+Gmail sync→import (300s; no auto-rebuild)
scripts/stack/start-assistant       # + qwen3.5:9b + PicoClaw agent (ask the brain)
scripts/stack/start-assistant --no-attach
scripts/stack/stop                  # compose stop; volumes kept
```

Same Compose services by hand:

```bash
docker compose up -d brain                       # :8630 Zig CGO serve
docker compose up -d mail-sync                   # ETL loop into kb-var
docker compose --profile index run --rm index    # rebuild
docker compose --profile picoclaw up brain-mcp   # MCP 127.0.0.1:8630
```

`GET /openapi.json`, `POST /mcp`. Agent tool order: `search` → `get` → `audit`.
See [picoclaw.md](picoclaw.md).

### Brain health-check & restart (#180)

The brain wedged once (2026-08-24): a `/ingest` write blocked forever inside
the Ladybug C layer, holding the read/write lock, so every search/get/audit
hung while `/health` still answered `ok` (it never touches the DB). The static
healthcheck let the container sit "healthy" but unresponsive until a manual
restart.

**Health-check** — probe the DB read path, not `/health`:

```bash
# fast (healthy): a real MATCH over kb.lbug
curl -sS --max-time 5 http://127.0.0.1:8630/stats   # -> {"total":9204,"by_root":{...}}
# live check of every agent tool
curl -sS --max-time 10 "http://127.0.0.1:8630/search?q=test&n=1&noweb=1"
```

`/stats` returns quickly only when the read handle is healthy; a wedged brain
times out. The compose `brain` healthcheck uses `/stats` for exactly this
reason (see `compose.yaml`), so docker auto-restarts a deadlocked container
(`restart: unless-stopped`) instead of leaving it "healthy".

**Restart**:

```bash
docker compose up -d brain          # or: docker restart 2dph-brain-1
# verify: process not a zombie, leaf count unchanged
ps -eo pid,ppid,stat,args | grep -E "brain-(serve|search)"   # no <defunct>
curl -sS --max-time 5 http://127.0.0.1:8630/stats            # total unchanged
```

Data lives in `var/kb.lbug` (host path, gitignored) and survives a container
restart unchanged — verify with `sha256sum var/kb.lbug` before/after. A stuck
write never commits, so the on-disk DB is the last successful state. A full
rebuild is only needed after a deliberate `--rebuild` or a corrupt DB, not
after a restart.

## Search benchmarks (#202)

The A/B harness `bin/brain/bench.go` runs a fixed golden set of queries
(`internal/brain/testdata/golden-set.json`, ~50, ru+en, themes
facts/mail/docs/git/ssh) through search and prints latency p50/p95/mean,
recall@5/@10 and CPU/RSS. **No candidate is accepted without a measurement**
(epic #201): the baseline is the current linear scan.

```bash
# baseline against the live brain (HTTP MCP :8630, read-only; the container can be left alone)
./bin/brain/bench.go

# machine-readable report (for issue/CI)
./bin/brain/bench.go --json

# candidate: executable binary with the CLI bin/brain/search.go --json -n N --no-web "q"
./bin/brain/bench.go --candidate /path/to/candidate

# candidate: another serve (:8631) — A/B recall vs baseline + latency ratio
./bin/brain/bench.go --candidate http://127.0.0.1:8631

# locally, without a server: open var/kb.lbug read-only in-process
# (quiesce first: docker compose stop brain — single-writer!)
./bin/brain/bench.go --inproc

# locally against a separate copy of the DB (e.g. a fresh --rebuild --db /tmp/kb.lbug)
./bin/brain/bench.go --inproc --db /tmp/kb.lbug

# server resources: sample the brain container PID instead of our own
./bin/brain/bench.go --pid $(docker inspect -f '{{.State.Pid}}' 2dph-brain-1)
```

Metrics and gates:

| Metric | Meaning | Gate |
|--------|---------|------|
| latency p50/p95/mean, ms | distribution of search duration per query | candidate p50 ≤ baseline p50 × 1.5 |
| recall@5/@10 (fragment) | share of queries whose expected fragment (from the corpus) is in the top-k | baseline recall@5 ≥ 0.95 |
| recall@5/@10 (vs baseline) | candidate does not lose known baseline hits (top-k IDs) | candidate ≥ 0.95 |
| CPU/RSS | user+sys jiffies, RSS before/after, peak (VmHWM) from /proc | for comparison "at the same resources" |

Exit codes: `0` — gates passed, `2` — recall/ratio FAIL, `1` — error
(no golden set, the searcher did not come up, and so on).

Reading: p50/p95 from per-query latency with `--workers 1` (the default) —
a clean single request; `--workers N` measures the server under load.
Reproducibility: two consecutive runs, p50 spread < 10 % (the linear scan is
deterministic; only background load adds spread). Baseline reference:
p50 ≈ 32s on 313k leafs (linear scan, #192); after the candidate lands, the
epic goal is p50 < 500ms, p95 < 2s.

In CI (GitHub, offline): `--rebuild` the corpus → `bench --inproc --json`
(the same schema as the recall gate `bin/brain/eval.go`).

### ANN index in production (#204/#206)

Vector search in the brain goes through an incremental index **outside liblbug**
(liblbug's own HNSW crashes as the graph grows, #192; the fallback is the linear
scan). Algorithm: **IVF** (k-means cells + exact cosine inside the probed
clusters, pure Go, no external dependencies). `coder/hnsw` (HNSW) was dropped on
empirical grounds: on 313k real embeddings from entry only 34% of nodes are
reachable (isolation when neighbours are evicted) → recall@5 drops to 0.16; IVF
holds recall@30 ≥ 0.93 at NList=2000/NProbe=128 (probe measurements #204). The
index lives in `var/state/vector.ann` (+ append-only WAL `vector.ann.wal`).

**Enabled by default** (#206): `vector.ann.enabled: true` — serve and the CLI
search through ANN; when the index is missing or corrupt, search automatically
falls back to the linear scan (it never breaks). `serve` loads the index at
startup (warm start, no rebuild); if the index is empty, serve answers via the
scan and the wave (step `ann-build`) builds it.

```bash
# full build from the DB (~30s extract + ~4min k-means + assign on 313k, NList=2000).
# Source of vectors: the embedding column when present (legacy/ANN-off DB); for a DB
# after --rebuild with ANN on there is no column (issue #248 B3) — build/upsert/ensure
# embed l.text with the model directly (~16k/s, +model ~1.5GB in that process).
KB_BUFFER_POOL=4294967296 ./bin/brain/ann.go build

# wave/manual mode: build if the index is missing or stale (>10% behind
# on the leaf counter), otherwise incremental WAL-upsert of new leafs (<1s, NOT a rebuild)
./bin/brain/ann.go ensure

# incremental upsert of new leafs (append WAL, NOT a rebuild) — for manual runs
./bin/brain/ann.go upsert

# index statistics
./bin/brain/ann.go stats

# CLI search through ANN (falls back to the scan when the index is missing)
./bin/brain/ann.go --json -n 10 "query"

# HTTP search server through ANN (for bench --candidate http://127.0.0.1:8631)
./bin/brain/ann.go api --port 8631
```

**Incrementality** (critical: the wave runs every N minutes, a full rebuild on
every wave is NOT ALLOWED): `ensure` compares the leaf counter in the DB with
the index capacity; steady-state growth (+100 mails on 313k) always goes to the
WAL-upsert (<1s), a full build only when the index is missing or more than 10%
behind (corrupt/partial/after a bulk import). Proven by the test
`TestAnnEnsureIncrementalUpsertNoRebuild` (+100 leafs → rebuild=false,
upsert <1s) and by a production measurement.

A/B (isolates the vector layer: baseline = scan, candidate = ANN, one process,
one DB):

```bash
# quiesce first (single-writer): docker compose stop brain
KB_BUFFER_POOL=4294967296 ./bin/brain/bench.go --inproc --candidate inproc-ann
# reference: ./bin/brain/bench.go --inproc                 (baseline, ~26 min for 50 queries)
# via a separate serve: ./bin/brain/bench.go --candidate http://127.0.0.1:8631
```

**nprobe default = 2000** (decision #206, measured on a live corpus of 313689
vectors, 11 golden-set queries): a full probe gives an ordering of candidates
that matches the exact scan (recall@5 vs baseline = 1.0) at p50 ~470ms
(gate <500ms); lowering it to 256 speeds up search (p50 ~260ms) but recall@5
drops to ~0.82 — the exact top-5 are lost. Choice: correctness first; tuning
(256/512) is a separate task, if p50 < 300ms is needed. Config
(`etc/brain/config.yml`, section `vector.ann`): `enabled`, `index`, `dim` (256),
`nlist` (2000 — k-means cells, ~150 vectors per cell at 313k), `nprobe` (2000).
In compose the whole `etc/brain` is mounted at `/data/etc/brain:ro` (the repo
root in the container is `/data`, #208); without the mount serve runs on the
code default (nprobe=128). Idempotency: a repeated `upsert` does not duplicate
(idCell is a map), a repeated `build` from the same DB yields the same capacity.
A/B reports: #204 (candidate), #206 (production, nprobe 256 vs 2000).

## Disk mail/contacts import (#79)

Sources on `/mnt/8TB` (TB mbox, .eml, VCF/MAB) → corpus pipeline; see
[mail-sources.md](mail-sources.md) for the authoritative source table + status.
PST stays blocked until `libpst-utils` (`readpst`) is installed.

```bash
./bin/mail/convert-mbox.go --in <tb-root> --out var/corpus/mail --source tb-profile --dry-run
./bin/mail/convert-mbox.go --in <tb-root> --out var/corpus/mail --source tb-profile
./bin/mail/import.go --from-eml var/corpus/mail
```

## Sync wave + OO CRM (AGENTS.md, #81)

Deterministic ingest/reconcile wave; every step is a reconcile/upsert, so the
wave is idempotent. `--dry-run` prints the fixed order without executing.
Bulk rebuild is NOT part of the wave (use `bin/brain/index.go --rebuild`).

```bash
go run ./bin/stack/sync.go --dry-run                     # print the wave
go run ./bin/stack/sync.go                               # default wave
go run ./bin/stack/sync.go --only mail,mail-import       # subset, order kept
go run ./bin/stack/sync.go --with-mail --only mail,mail-import,mail-index  # mail → brain index (#199)
go run ./bin/stack/sync.go --with-chats --contacts <vcf> # + chats, contacts
go run ./bin/stack/sync.go --with-chats --git-root <dir> # + chats, git history
```

`--only` takes actual step names (fixed order): `mail`, `mail-import`,
`mail-index`, `chats`, `contact-brain`, `git-brain`, `contact-crm`.
`--with-mail` enables the `mail-index` step (mail leafs of both corpora → brain,
#199); without the flag the step is SKIP, not FAIL.

### chats step (--with-chats, #195)

The `chats` step is one logical step name that runs two sub-commands
sequentially: `bin/chat/sync.go telegram`, then `bin/chat/sync.go linkedin`.
Both download messages to `var/corpus/chats/<platform>/`.

Credentials come from the environment:

| Env var | Purpose |
|---------|---------|
| `TELEGRAM_API_ID` / `TELEGRAM_API_HASH` / `TELEGRAM_PHONE` | Telegram app credentials |
| `TELEGRAM_MCP_DIR` | path to the telegram-mcp checkout (session read from its `.env`) |
| `TELEGRAM_SESSION_STRING` | optional; otherwise read from `TELEGRAM_MCP_DIR/.env` |
| `LINKEDIN_USER_DATA_DIR` | LinkedIn profile dir with session files (default `~/.linkedin-mcp/profile`) |

**SKIP semantics**: a platform whose credentials/session are missing prints
`chats: SKIP …` and exits with code 3 (`pkg/cli.ExitSkip`) — the wave shows
`SKIP`, never `FAIL`. The same convention applies to `mail` sources without
creds (onlyoffice/m365/imap). Tools with credentials present but broken
(config paths, invalid API id) still FAIL loudly.

### var/ permissions (uid 1001 vs 1000)

The brain/mail-sync containers write `var/**` as uid 1001 (Dockerfile
`useradd --uid 1001`) while host tools run as uid 1000. A container write
leaves `var/kb.lbug.wal` and `var/corpus/**` unopenable by the wave
(`OpenDatabase … status 1`). Since compose.yaml now runs the containers as
the host uid (`user: "${KB_UID:-1000}:${KB_GID:-1000}"`), new writes align
automatically; a one-time repair for already-misowned files goes through
docker busybox (no sudo on this host):

```bash
scripts/stack/fix-var-perms            # chown -R var/ to the caller uid:gid
scripts/stack/fix-var-perms --check    # report files NOT owned by the caller
```

Run it after any container write or when a wave step fails on `var/**`.
The wave also quiesces the compose brain container around write steps
(`docker compose stop brain` → writes → `start brain`), so `git-brain` does
not collide with the running serve process holding `kb.lbug`.

### index-sync — gator→graph→index cycle (#292)

The chain `gator sync→etl→pack → 2dph import(graph) → index(kb.lbug)` is
closed by the compose service **`index-sync`** (profile `index`). No systemd.

One cycle: discover the documents hive (`<hive-doc>/source=*/channel=*`) →
stop the `brain` container (Ladybug is single-writer) → import the mail canon →
`brain-index --skip` (docs corpus + indexes) → (gator mode only) `mail-graph
--commit` per channel (idempotent MERGE) → ANN `ensure` → start brain and wait
healthy → write freshness state. A cycle is skipped when `kb.lbug` is already
newer than the newest upstream state and the last run did not error, so brain is
not bounced for nothing.

**ANN index build/maintenance (#204/#206).** The IVF vector index is maintained
by `bin/brain/ann.go ensure`, which `index-sync` runs at the end of every cycle.
The binary is read from `ANN_BIN` (flag `--ann` wins; empty = skip). Compose
sets `ANN_BIN=/usr/local/bin/brain-ann`; the `api-build` stage builds it with
`-tags system_ladybug,brain_ann` and the `api` stage copies it, so no manual
install is needed. `ensure` does a full k-means build only when the index is
missing or stale (>10% behind the DB leaf count); steady-state growth is an
incremental WAL-upsert (<1s, never a rebuild). The index path is
`vector.ann.index`, default `<root>/var/state/vector.ann` (+ `.wal`); in the
container `KB_ROOT=/data` and `./var` is mounted at `/data/var`, so the index
lands in the host `var/state/` on the persistent volume. Empty/absent index is
never fatal: serve/CLI fall back to the linear scan and `/stats` reports
`ann.loaded:false`.

**Mail canon — T10-A (`MAIL_SOURCE`, default `corpus`).** The info-corpus mail
canon is the **local 2dph M365 corpus**, not the gator `parquet/mail` hive: the
Edelweiss boxes are M365/OAuth and are synced by 2dph into `var/corpus/mail`
(gator's mail client is plain IMAP). Selecting the canon keeps a single source
of Leafs (local corpus ids are content-address, gator ids are `message_id`, so
indexing both would duplicate).

- `corpus` (default): `brain-index --with-mail` indexes `var/corpus/mail/m365`
  (plus the legacy `var/mail`); the gator `parquet/mail` leaf+graph steps are
  **skipped**. `--since YYYY-MM-DD` (env `MAIL_SINCE`) bounds the corpus mail if
  set. Corpus-mail mtime drives freshness.
- `gator`: unchanged legacy path — `mail-leaf` over `parquet/mail` plus
  `mail-graph --commit` per channel.
- The gator `parquet/documents` → `doc-leaf` step runs in **both** modes.

**Document body text — T12 (`GATOR_DOC_TEXT`, default off).** `doc-leaf`
indexes metadata only (`title` + `filename` + `oo_path`). With the flag the PDF
is fetched from OnlyOffice by `oo_path` (or a `fileid` in `ref`) and its text is
appended to `Leaf.Text` (capped, default 256 KiB). Enable with
`--document-text` on `doc-leaf`/`index-loop` or `GATOR_DOC_TEXT=1`; requires
`ONLYOFFICE_URL/USER/PASS` and `GATOR_DOC_ROOT_ID` (the Files folder rooting
`oo_path`; optional `GATOR_DOC_ROOT_PREFIX`, default `external`), plus optional
`GATOR_DOC_TEXT_LIMIT`. A fetch/extract failure keeps the metadata leaf. Body
text changes the content hash, so the first enabled run writes a new version;
extraction runs before the `--skip` dedup, so an enabled cycle re-fetches.

```bash
scripts/stack/start-index-sync                 # compose up -d index-sync (loop, 1h)
docker compose --profile index up -d index-sync
docker compose --profile index run --rm index-sync index-loop --once   # one manual cycle
# one-off canon override:
docker compose --profile index run --rm index-sync index-loop --once --mail-source gator
docker compose --profile index logs -f index-sync
```

Requirements:

- API image rebuilt with `mail-graph` + `index-loop` + `brain-ann`
  (`docker compose --profile index build index-sync`); the older
  `ghcr.io/eslider/2dph:api` lacks them. Without `brain-ann` the ANN step
  is skipped and vector search uses the linear scan.
- In `corpus` mode the gator mail hive is not required; `./var` must be mounted
  at `/data/var` so `var/corpus/mail/m365` is visible (it is).
- `GATOR_VAR_HOST=<gator repo>/var/gator` in `.env` (gitignored) — the host
  gator canon is bind-mounted read-only at `/gator`. `GATOR_MAIL_HIVE`
  inside the container is `/gator/parquet/mail` (used only in `gator` mode);
  the same mount covers `parquet/documents` (kind=document), set as
  `GATOR_DOCUMENTS_HIVE` (`/gator/parquet/documents`). An absent/empty document
  tree is skipped.
- `/var/run/docker.sock` is mounted: the cycle stops/starts the sibling
  `brain` container. `index-sync` therefore has Docker control — keep it on
  the local host only. Because the container runs with an explicit `user:`,
  it does not inherit the host's supplementary `docker` group; set
  `DOCKER_GID` (default 991) to the socket's group so the added `group_add`
  grants access, otherwise the quiesce step fails with
  `dial unix /var/run/docker.sock: connect: permission denied`
  (`stat -c '%g' /var/run/docker.sock`).
- The container runs as the host uid (`KB_UID`/`KB_GID`) so `kb.lbug` stays
  writable by host tools (#195).

Freshness is observable: the state file is `<root>/var/state/index-freshness.yml`,
`/stats` returns `index_at`/`import_at`/`kb_mtime`/`pack_mtime`/`stale`/
`stale_after`, and `scripts/stack/status` prints the same (stale from `/stats`
when the brain answers, otherwise `unknown` — the on-disk state stays readable
while the brain is down).

```bash
curl -s localhost:8630/stats | yq '.index_at,.import_at,.kb_mtime,.stale'
scripts/stack/status | yq '.freshness'
ls -la --time-style=full-iso var/kb.lbug      # mtime newer than the gator pack
```

### OnlyOffice CRM tools

OnlyOffice CRM tools need `ONLYOFFICE_URL`/`ONLYOFFICE_USER`/`ONLYOFFICE_PASS`
env. Both are read-only (report) by default; `--write` applies changes.

```bash
# reconcile report — human mail senders vs OO CRM persons (match by email)
go run -tags=onlyoffice_reconcile_contact ./bin/onlyoffice/reconcile-contact.go
go run -tags=onlyoffice_reconcile_contact ./bin/onlyoffice/reconcile-contact.go --write --limit 200

# interaction write — mail → history note on the person's opportunity
go run -tags=onlyoffice_import_interaction ./bin/onlyoffice/import-interaction.go
go run -tags=onlyoffice_import_interaction ./bin/onlyoffice/import-interaction.go --write --limit 50
```

Both scan `var/corpus/mail` by default (`--sources` to override) and build an
email → person index in one pass over the whole CRM. Machine senders
(newsletter/bounce/platform) never become contacts.

## Browser sync (periodic, #163)

Pushes the browser-extracted corpus (`var/corpus/{gmail,linkedin,djinni}`) into
the brain as leafs every 6 h. Thorium down is tolerated: extraction is skipped
and the last-known corpus is still ingested.

```bash
./bin/cron/browser-sync.go                  # one cycle, default paths
./bin/cron/browser-sync.go --skip-extract   # no Thorium probe
./bin/cron/browser-sync.go --interval 6h    # run continuously
sudo scripts/browser-sync-install.sh        # install the 6-hourly systemd timer
```

Log: `var/log/browser-sync.log`. The systemd units
(`scripts/browser-sync.{service,timer}`) use an `@REPO_ROOT@` placeholder; the
install script substitutes the real root at install time.

## Reasoner (optional, D18)

CPU sidecar on `127.0.0.1:11435`. Weights are not in the 2dph image.

```bash
docker compose --profile reasoner up -d reasoner
REASONER_BASE_URL=http://127.0.0.1:11435/v1 ./bin/reasoner/bench.go --json
```

See [reasoner.md](reasoner.md).

## GitHub mirror sync (#194)

Gitea is the canonical home; GitHub is a publish-only **showcase** (`#190`,
`#213`). `main` is the single working line and the public face (Gitea `#213`).
PRs merge into `main` directly; no `release/v1` branch is kept. After each
merge into `main`, sync **only `main`** (plus tags) to GitHub:

```bash
git fetch origin --tags --force
git push github main:main --force-with-lease
git push github --tags --force
```

Verify zero drift on `main`:

```bash
git rev-list --count origin/main..github/main   # 0
git rev-list --count github/main..origin/main   # 0
```

Branch policy (`#213`): both Gitea and GitHub keep `main` only. Old/merged
feature branches are backed up locally under `refs/backup/*` before deletion.

Content guard (`#190`): before pushing to GitHub, confirm the diff is project
content only — no internal paths/hosts/ports, no secrets. Precedent #142/#194:
leaks are purged from GitHub history with `git filter-repo`; Gitea stays the
canonical home and is never rewritten for the mirror.
