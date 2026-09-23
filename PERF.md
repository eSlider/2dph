# PERF — 2dph brain memory

Root cause + recipe for the 5.2GB RSS leak (2026-08-19). Source of truth:
`scripts/stack/serve-brain` and `scripts/docker-entrypoint` (same pattern, two places).

## Symptom

Running `serve.go` alone (single process):

- Every search loaded the 512MB embedding matrix in-process.
  `Close()` frees only the tokenizer, not the matrix.
- Spawned ~20 zombie `[serve]` subprocesses (`os.Executable` can't handle
  the `serve` subcommand).
- RSS grew to **5.2GB**.

## Fix (2 processes)

The embed daemon and the API must be separate processes:

1. `brain-search serve 17830` — loads the model **once**, keeps it resident
   (~1.1GB RSS), exposes `/embed`.
2. `brain-serve` — thin HTTP/MCP API (~100MB) that proxies embedding
   requests to the daemon.

Measured: daemon 1.1GB + API 100MB, zero zombies.

## Recipe

- Docker: the entrypoint already does this
  (`scripts/docker-entrypoint` `serve` branch starts `brain-search serve`
  before `brain-serve`).
- Bare host: `scripts/stack/serve-brain` — builds static binaries
  (`bin-build/brain-{serve,search,index}`) with Zig CGO, then starts both
  processes with reuse logic (curl health, keep running daemon/API).

## Search path reloaded the model per request (2026-09-23, #335)

Root cause + recipe for the second, larger leak. `internal/brain/search.go`
`embedQuery` (the no-daemon fallback) called `loadModel()` + `defer
model.Close()` on **every search**; `StaticModel.Close()` freed only the
tokenizer, not the safetensors matrix. So each request retained ~490 MB inuse
(~980 MB allocated: read buffer + matrix) until GC pressure caught up.

Measured live before (container `2dph-brain-1`, DB `kb.lbug` 11 MB):

- 13 sequential searches: RSS **147 MB → 6.8 GB**, swap 0 → 0.5 GB.
- pprof `alloc_space` total 16.98 GB, `loadMatrix` **13.84 GB (81.5%)**
  (`inuse_space` `loadMatrix` 488.6 MB).
- Long run: 13 GB (RSS 1.8 + swap 11.2) over 6 days.

Fix: the process-wide singleton cache from #110 was generalized
(`internal/brain/embedmodel.go`, formerly `ingestmodel.go`) and now backs
/ingest, the /search in-process fallback and ANN extract. `embedQuery` →
`embedInProcess()`; the shared model is never closed (`embedder` has no Close).
`StaticModel.Close()` now releases `mat` for the one-shot CLI tools. pprof is
pinned to loopback and the container has a 2 GiB memory limit
(`deploy.resources.limits.memory`).

Measured live after (same host, `KBSEARCH_NO_DAEMON=1`, `test/stress
--mix search:1 --c 1`): 49,448 sequential searches, 0 errors, p50 **6.0 ms**
(vs 1429 ms before), `alloc_space` `loadMatrix` **977 MB over the whole run =
exactly one load** (before: 13.84 GB over 13 requests). RSS: 143 MB cold →
1.68 GiB after warmup (shared model + 512 MiB pool + ANN), then 1.79 → 1.82
GiB over ~39k further searches (delta ~36 MiB, < 50 MiB); swap flat at
~4.5 MiB (before: 0.5 GiB after 13 requests). Container stays under the 2 GiB
limit. Tests: `internal/brain/embedmodel_test.go`.

## Buffer-pool leak (2026-08-20)

Sustained concurrent searches pinned the whole Ladybug FTS/vector buffer
pool: every `conn.Query`/`conn.Execute` result that was not closed kept its
C buffer pages pinned. After a load burst the pool was permanently full
(`Buffer manager exception: Unable to allocate memory! The buffer pool is
full and no memory could be freed!`) and **every** endpoint — even `/stats`
— returned 502 until a process restart.

Fixed in `internal/brain/*.go`: `defer res.Close()` on every QueryResult
(read + one-shot statements via `qClose`). Regression:
`TestConcurrentSearchesDontExhaustBufferPool` (red without the fix).
Live proof: `test/stress` at c=8/16 sustained now holds 0% errors.

## Tuning

- `KB_BUFFER_POOL` (bytes) sizes the Ladybug buffer pool. Compose caps it at
  512 MiB (was 4 GiB) — the served DB is ~11 MB, so a large pool only wastes
  memory. Rebuild (`--profile index`) uses its own larger pool. Smaller pools
  exhaust faster under concurrency; raise it only for hosts with a large DB.
- `KB_PPROF` (port) enables `net/http/pprof` on the API for
  cpu/heap/goroutine profiles (`go tool pprof`). It binds **127.0.0.1 only**
  (never the API host): leave it unset to disable.
- `deploy.resources.limits.memory` (compose, brain) = 2 GiB hard ceiling so a
  memory regression is contained instead of eating host swap.

## Env

- `KBSEARCH_PORT` (default 17830) — embed daemon port.
- `KBSEARCH_NO_DAEMON=1` — force in-process embedding (only for testing).
- `KB_PORT` (default 8630), `KB_WORKERS` (API worker pool).