# PERF — 2dph brain memory

Root cause + recipe for the 5.2GB RSS leak (2026-08-19). Source of truth:
`bin/stack/serve-brain` and `bin/docker-entrypoint` (same pattern, two places).

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
  (`bin/docker-entrypoint` `serve` branch starts `brain-search serve`
  before `brain-serve`).
- Bare host: `bin/stack/serve-brain` — builds static binaries
  (`bin-build/brain-{serve,search,index}`) with Zig CGO, then starts both
  processes with reuse logic (curl health, keep running daemon/API).

## Env

- `KBSEARCH_PORT` (default 17830) — embed daemon port.
- `KBSEARCH_NO_DAEMON=1` — force in-process embedding (only for testing).
- `KB_PORT` (default 8630), `KB_WORKERS` (API worker pool).