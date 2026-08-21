//usr/bin/env go run -tags=qa_stats "$0" "$@"; exit
//go:build qa_stats
//
// bin/qa/stats.go - DuckDB quantiles over a JSON number array or JSONL count.
//
//	./bin/qa/stats.go <<< '[1,2,3,4,5]'
//	./bin/qa/stats.go --jsonl rows.jsonl
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
// DuckDB CGO needs gcc/g++ (not Zig). After eval "$(bin/cgo/zig env)":
//   CC=gcc CXX=g++ CGO_CFLAGS= CGO_LDFLAGS= ./bin/qa/stats.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	cliparse "github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/duckstats"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	c, err := cliparse.ParseQAStats(args)
	if err != nil {
		return cliparse.Fail(err)
	}
	jsonl := c.JSONL
	if jsonl != "" {
		n, err := duckstats.CountJSONL(jsonl)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("n: %d\n", n)
		return 0
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var samples []float64
	if err := json.Unmarshal(raw, &samples); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	s, err := duckstats.Quantiles(samples)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("n: %d\nmin: %g\np50: %g\np95: %g\nmax: %g\navg: %g\n",
		s.N, s.Min, s.P50, s.P95, s.Max, s.Avg)
	return 0
}
