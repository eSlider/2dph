//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_ann "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_ann
//
// bin/brain/ann.go - incremental ANN vector index (issue #204), outside
// liblbug (whose own HNSW crashes on grown graphs, #192).
//
//	./bin/brain/ann.go build                     # full snapshot from the DB
//	./bin/brain/ann.go upsert                    # incremental: new leafs only (WAL append, no rebuild)
//	./bin/brain/ann.go stats                     # index stats
//	./bin/brain/ann.go api --port 8631           # HTTP search server (bench --candidate http://…)
//	./bin/brain/ann.go --json -n 10 "query"      # CLI search, ANN forced on (bench --candidate <bin>)
//
// Index lives at <root>/var/state/vector.ann (+ .wal), params from
// config vector.ann (see etc/brain/config.yml). Missing index → search falls
// back to the linear scan (queryVector), never fails.
//
// Shebang routes through bin/cgo/zig (same as bin/brain/search.go).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(brain.MainANN(os.Args[1:]))
}
