---
name: duckdb
description: >-
  Use https://github.com/duckdb/duckdb-go in-process for columnar analytics
  (quantiles, GROUP BY, JSON/CSV/Parquet/JSONL scans) when that is faster than
  nested Go loops. Not Ladybug. Not the web-search sqlite cache. Use when
  aggregating samples, counting JSONL, or SQL over tabular files.
---

# duckdb-go

Use https://github.com/duckdb/duckdb-go where it makes sense to get better performance in code.

In-process DuckDB (`pkg/duckdb`, `database/sql` driver `duckdb`).
Vectorized SQL over tables, JSONL, CSV, Parquet. CGO with bundled libs
(linux/darwin amd64/arm64). Links with **gcc/g++** (libstdc++), not Zig.
D21 Zig (`bin/cgo/zcc`) is Ladybug/tokenizers only. After
`eval "$(bin/cgo/zig env)"`:

```bash
CC=gcc CXX=g++ CGO_CFLAGS= CGO_LDFLAGS= ./bin/jsonl/stats.go <<< '[1,2,3,4,5]'
CC=gcc CXX=g++ CGO_CFLAGS= CGO_LDFLAGS= go test ./pkg/duckdb
```

| Store | Job |
|-------|-----|
| Ladybug | graph + FTS + HNSW (facts/info) |
| modernc sqlite | web-search KV cache + throttle |
| duckdb-go | OLAP: quantiles, counts, scans of many rows/files |
| mikefarah/yq | small YAML/JSON/XML/CSV/TOML/HCL slice, not bulk |

```bash
./bin/jsonl/stats.go <<< '[1,2,3,4,5]'
./bin/jsonl/stats.go --jsonl path/to/rows.jsonl
```

Do not open Ladybug through DuckDB. Do not put secrets or client PII into
DuckDB files under the repo.
