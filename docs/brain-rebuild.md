# Brain corpus rebuild (runbook)

Optimized corpus (re)index. Parallel embedding, batched writes, resume, and a
live progress/ETA monitor.

## Commands

Build the native binaries (also runs the cgo brain tests):

    bin/stack/serve-brain --build        # or ./bin/cgo/zig go build ...

Fresh rebuild of info + mail + facts with control flags:

    KB_BUFFER_POOL=10737418240 bin-build/brain-index \
        --rebuild --with-mail --with-facts \
        --workers 12 --batch 256 --progress 5 --skip

- `--workers N`   parallel embedding goroutines (default 4; use ~cores on the box)
- `--batch N`     leafs per upsert transaction (default 64)
- `--progress N`  print rate + ETA every N seconds to stderr
- `--skip`        resume: skip leafs whose id is already in the db

Because leaf ids are deterministic (`LeafID(text, source)`), `--skip` makes a
re-run cheap: it filters existing ids before embedding, so it embeds only new
leafs. After a partial/aborted run, re-running with `--skip` skips the
already-written corpus and goes straight to index build.

> Note: `--rebuild` deletes the db, so `--skip` + `--rebuild` always restarts
> fresh. To resume a crashed build (leafs already written, indexes missing),
> run `--skip` **without** `--rebuild` so the db is preserved and only missing
> indexes are built:
>
>     KB_BUFFER_POOL=10737418240 bin-build/brain-index --skip \
>         --with-mail --with-facts --workers 12 --batch 256 --progress 5

Observed rates: embedding is fast (~16k/s, 256-dim); the db **write** phase is
the bottleneck (~100/s, ~40 min for 242k leafs).

Before a fresh rebuild, remove the old db (daemons must be down first):

    bin/stack/serve-brain --stop          # or pkill bin-build/brain-*
    rm -f var/kb.lbug var/kb.lbug.wal

## Buffer pool

`OpenWritable` defaults to a 1 GB buffer pool. Override via `KB_BUFFER_POOL`.
History: a 1 GB-pool run OOM'd at `CREATE_FTS_INDEX`; a 10 GB pool held a full
242,275-leaf embedding (DB ~1.4 GB). Use 10 GB (`10737418240`).

## Control / monitoring

`ProgressReporter` (cgo-free, `internal/brain/corpus_pool.go`) prints
`index: done/total rate/s eta=...` to stderr at `--progress` interval. The worker
pool (`parallelEmbed`) is order-preserving, respects `ctx` cancellation, and
collects embed errors per-result.

## After the index-build phase

    bin/stack/serve-brain                 # builds + starts brain-serve + brain-search

Verify:

    bin-build/brain-search ... 17830      # or the API :8630, sort-by-date
