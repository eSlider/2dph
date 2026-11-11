# Brain corpus rebuild (runbook)

Optimized corpus (re)index. Parallel embedding, batched writes, resume, a live
progress/ETA monitor, and cross-chunk dedup before write (issue #248).

## Commands

Build the native binaries (also runs the cgo brain tests):

    scripts/stack/serve-brain --build        # or ./bin/cgo/zig go build ...

Fresh rebuild of info + mail + facts with control flags (one command, without the
two-phase workaround — the pool for the FTS phase is picked automatically, #244):

    bin-build/brain-index \
        --rebuild --with-mail --with-facts \
        --workers 12 --batch 256 --progress 5 --skip

Corpus adapters (P-9.3): `--with-mail` → mail adapter, `--with-chats` →
chats adapter, `--corpus DIR` → docs adapter (additional paths), `--git-root DIR` →
git adapter (repository history). Each corpus is a `contract.Source`
(`internal/corpus`); index writes them with a single `WriteCorpus`.

> Facts require `--with-facts` (or `--facts-json`). Corpus leafs are always
> written as `root=info` (single file source each); the facts layer
> (`root=facts`, ≥2 independent sources) comes from `bin/facts/extract.go` and
> is only ingested on rebuild when the flag is set. A rebuild **without**
> `--with-facts` leaves the facts-root empty even though all leafs are
> `confidence=confirmed` (#181). To repair an existing DB, see
> `docs/facts/ingest.md` (`bin/facts/extract.go` + `bin/facts/promote.go`).

- `--workers N`   parallel embedding goroutines (default 4; use ~cores on the box)
- `--batch N`     leafs per upsert transaction (default 64)
- `--progress N`  print rate + ETA every N seconds to stderr
- `--skip`        resume: skip leafs whose id is already in the db

### Dedup before write (issue #248 A1)

`WriteCorpusChunked` dedups **cross-chunk and always** (not only with
`--skip`): a leaf with a contract `ContentHash` (source|external_id|kind|text)
already seen in this run is neither embedded nor written. This removes the double
stream of the same mail (live `var/corpus/mail` + legacy `var/mail`, #199/#184):
a full rebuild — 307k streamed → ~105k unique, the C side does 3× less
MERGE/undo work. The determinism of the id/order of the final leafs does not change
(duplicates would collapse to the same id on the C side too — now earlier).

Pass 1 (`CountCorpus`, dry-run) counts the same way: `info` = the number of unique
leafs (how many will land in the DB), `streamed` = the raw stream with duplicates.
`--limit` counts streamed leafs, so a dry-run and the actual write with the same limit
give the same number.

Because leaf ids are deterministic (`contract.ContentHash`, P-9.3), `--skip`
makes a re-run cheap: it filters existing ids before embedding, so it embeds
only new leafs. After a partial/aborted run, re-running with `--skip` skips the
already-written corpus and goes straight to index build.

### Embedding column and ANN (issue #248 B3)

When `vector.ann.enabled=true` (prod default #206), rebuild **does not write the
embedding column**: the model is not loaded at all (minus ~1.5GB RSS of the write phase),
leafs and facts are written as text. The vector index is built by the `ann-build`
wave step (`bin/brain/ann.go ensure`) from the text directly — `extractRows` embeds
column-less leafs with the model (legacy DBs with the column go the old way, without the
model). The column is written only when ANN is **disabled** — then the vector
search path is a linear scan over `l.embedding` (search.go `vecScanStmt`).

> Note: `--rebuild` deletes the db, so `--skip` + `--rebuild` always restarts
> fresh. To resume a crashed build (leafs already written, indexes missing),
> run `--skip` **without** `--rebuild` so the db is preserved and only missing
> indexes are built — the index phase is the same in both cases
> (`BuildIndexes`, auto pool):
>
>     bin-build/brain-index --skip \
>         --with-mail --with-facts --workers 12 --batch 256 --progress 5

> Warning: `--rebuild` refuses to run while a brain holds the db open (fd
> holders via /proc, or an API answering on `127.0.0.1:$KB_PORT` when
> rebuilding the repo-default `var/kb.lbug`) — deleting the file under a live
> serve leaves it serving the removed inode and can lock the service out of
> the fresh db. Stop/restart the brain first, pass `--force` to override, or
> set `KB_INDEX_ALLOW_LIVE=1` in environments where the swap+restart flow is
> intended (the compose `index` service already sets this).

Observed rates: embedding is fast (~16k/s, 256-dim); the db **write** phase is
the bottleneck (~100/s, ~40 min for 242k leafs).

Before a fresh rebuild, remove the old db (daemons must be down first):

    scripts/stack/serve-brain --stop          # or pkill bin-build/brain-*
    rm -f var/kb.lbug var/kb.lbug.wal

## Buffer pool (memory, issue #244)

The two phases of a run live on different pools:

- **write phase** (writing leafs) — on the configured `KB_BUFFER_POOL`
  (default 1 GB). An explicit `KB_BUFFER_POOL` remains the lower bound for both phases.
- **FTS phase** (`CREATE_FTS_INDEX`, after writing the leafs) — the handle is closed,
  the DB is reopened with an auto-picked pool `max(1GB, chars×32)`, where chars is
  the total size of the corpus text (`MATCH (l:Leaf) RETURN sum(size(l.text))`).
  Measurements of the "buffer pool full" threshold: real mail leafs 10.3 MB of text →
  64 MB fails / 128 MB passes; synthetic 10.8 MB → 192 MB / 256 MB;
  production 105k leafs (280 MB of text) → 1 GB fails / 10 GB passes.
  The ×32 coefficient gives ~9 GB on the full corpus.

The pool is a page cache, not a preallocation: RSS grows only for the real
working set, so over-provisioning the pool is safe (memory is not reserved).

### C ceiling: write phase ~140 KB/leaf (measurement #244)

Besides the pool, the C side of Ladybug holds ~135-145 KB per written leaf
(independently of the pool: with a 1 GB pool and 30k leafs RSS grew to ~6.3 GB;
embedding does not matter — a measurement without `embedding` gave the same 141 KB/leaf).
The growth is linear and is **not released when the handle is closed in the same process**
(malloc arenas): the full corpus of 105k → ~14-15 GB C-side + the model ~1.5 GB.
A CHECKPOINT every 1024 leafs does not matter. This is liblbug 0.19.1 internals
(undo/version structures of the node), outside Go control.

> Checkpoint (issue #248 B1, measured): liblbug 0.19.1 enables
> `auto_checkpoint` by default with a WAL threshold of 16MB — and go-ladybug v0.17.0
> passes this through as is (the fields `auto_checkpoint`/`checkpoint_threshold` are
> absent in the Go `SystemConfig`, toC starts from the C default). In the write phase WAL
> grows to ~16MB and is checkpointed cyclically into the main DB (observed on
> 15k leafs: wal 0→16.6MB → reset to 2.2MB, the db grows in chunks). An explicit
> `CHECKPOINT` as a Cypher statement is also accepted. It does not affect RSS
> (the growth comes from undo structures, not from WAL) — the main lever of the peak is dedup (A1):
> 3× less MERGE/undo on the C side.

Consequence for the peak RSS: the index phase starts after the write handle is closed
(same process — the memory is retained), so the peak ≈ the C accumulation of the write phase +
the working set of the FTS phase. Earlier (before #244) the runbook prescribed
`KB_BUFFER_POOL=10GB` for the whole run — the write phase additionally inflated
dirty pages towards 10 GB and FTS was built on the same handle on top of the accumulation:
that is what gave the observed 39-41 GB. Now the write phase runs on a 1 GB pool, FTS —
on the auto pool after the handle is closed.

If a lower peak is needed (separate processes for the phases free the C memory
between runs — the two-phase workaround) — that is DevOps control on a full
run; the code gives a correct result in one process as well.

### C ceiling: the orphan table (why the auto pool)

When `CREATE_FTS_INDEX` fails (insufficient pool) Ladybug leaves a partial
internal table `0_id_appears_info` (token rows up to the point of failure).
It is unreachable via `DROP TABLE`/`DROP_FTS_INDEX` (the catalog hides
internal tables; verified on liblbug 0.19.1 and in the Kuzu FTS sources:
`appears_info` is created first, dropped at the end of the rewrite query) and
permanently blocks a repeated `CREATE_FTS_INDEX` on this DB. That is why the pool
of the index phase is picked in advance; if FTS still fails (the host lacks
RAM), the error explicitly names the orphan and the recovery: delete the DB and rebuild or
restore a backup. Criterion #237 "peak RSS ≤ 2-4 GB" formally refers to the
Go part (chunking, done); the C side requires multiple gigabytes both on write
(~140 KB/leaf) and on FTS (a pool of ~9 GB on the full corpus) — these are deliberate
C limitations of Ladybug (issue #244).

`--skip-indexes` still writes only leafs (the first phase of the workaround
for debugging/external indexes); it is not needed in a normal run.

## Control / monitoring

`ProgressReporter` (cgo-free, `internal/brain/corpus_pool.go`) prints
`index: done/total rate/s eta=...` to stderr at `--progress` interval. The worker
pool (`parallelEmbed`) is order-preserving, respects `ctx` cancellation, and
collects embed errors per-result.

## After the index-build phase

    scripts/stack/serve-brain                 # builds + starts brain-serve + brain-search

Verify:

    bin-build/brain-search ... 17830      # or the API :8630, sort-by-date
