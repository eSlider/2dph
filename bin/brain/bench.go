//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_bench "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_bench
//
// bin/brain/bench.go - A/B benchmark of brain search (issue #202).
//
//	./bin/brain/bench.go                                 # baseline vs live brain :8630
//	./bin/brain/bench.go --json                          # machine report
//	./bin/brain/bench.go --candidate /path/to/bin        # baseline + candidate, A/B gates
//	./bin/brain/bench.go --candidate http://127.0.0.1:8631
//	./bin/brain/bench.go --inproc                        # local DB (quiesce serve first)
//
// Runs the golden-set (internal/brain/testdata/golden-set.json) and prints
// latency p50/p95/mean, recall@5/@10, CPU/RSS. Exit codes: 0 pass, 1 runtime
// error, 2 gate failure (recall@5 < 0.95, or candidate p50 > 1.5x baseline).
//
// Shebang routes through bin/cgo/zig (same as bin/brain/search.go).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(brain.MainBench(os.Args[1:]))
}
