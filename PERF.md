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

- `KB_BUFFER_POOL` (bytes) sizes the Ladybug buffer pool (default 1GB).
  Smaller pools exhaust faster under concurrency; keep the default unless
  the host cannot commit 1GB.
- `KB_PPROF` (port) enables `net/http/pprof` on the API for
  cpu/heap/goroutine profiles (`go tool pprof`).

## Env

- `KBSEARCH_PORT` (default 17830) — embed daemon port.
- `KBSEARCH_NO_DAEMON=1` — force in-process embedding (only for testing).
- `KB_PORT` (default 8630), `KB_WORKERS` (API worker pool).