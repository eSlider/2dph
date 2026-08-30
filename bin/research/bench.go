//usr/bin/env go run -tags=research_bench "$0" "$@"; exit
//go:build research_bench
//
// bin/research/bench.go — warm latency benchmark for the liteparse service
// (epic #219): measures p50/p95 of per-document parses through the warm
// compose service (`docker compose exec`), after a warmup pass. Documents the
// gain of the start-as-service pattern over per-doc `docker run`.
//
//	./bin/research/bench.go <file> [N]        # default N=10 iterations
//	./bin/research/bench.go --ocr <scan.pdf>  # benchmark the OCR path
//	./bin/research/bench.go --json <file>     # time --format json, not md
//
// Prints per-iteration ms + p50/p95 + min/max. Requires:
//   docker compose up -d liteparse
// NOTE: never run gofmt -w on this file — it breaks the shebang.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/eSlider/2dph/internal/research"
)

func main() {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	ocr := fs.Bool("ocr", false, "benchmark OCR path")
	jsonOut := fs.Bool("json", false, "parse --format json (default markdown)")
	service := fs.String("service", "liteparse", "compose service name")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: bench.go [flags] <file> [N]\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])

	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	file := args[0]
	n := 10
	if len(args) > 1 {
		if _, err := fmt.Sscanf(args[1], "%d", &n); err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "bench: bad N", args[1])
			os.Exit(2)
		}
	}
	if !research.DockerOK() {
		fmt.Fprintln(os.Stderr, "bench: docker not on PATH (start `docker compose up -d liteparse`)")
		os.Exit(1)
	}

	r := research.NewRunner(*service, 3*time.Minute)
	format := "markdown"
	if *jsonOut {
		format = "json"
	}
	rel, _ := filepath.Rel(filepath.Dir(research.StructDir), file)
	in := "/v/" + filepath.ToSlash(rel)

	// Warmup: first parse loads pdfium + tess into page cache; discard timing.
	_ = warm(ctx(r), r, in, format, *ocr)

	// Timed iterations.
	var ms []float64
	for i := 0; i < n; i++ {
		start := time.Now()
		_, err := r.Parse(context.Background(), in, "", research.ParseOpts{Format: format, OCR: *ocr})
		if err != nil {
			fmt.Fprintf(os.Stderr, "bench iter %d: %v\n", i+1, err)
			continue
		}
		d := time.Since(start).Seconds() * 1000
		ms = append(ms, d)
	}
	if len(ms) == 0 {
		os.Exit(1)
	}
	sort.Float64s(ms)
	fmt.Printf("file: %s  format=%s  ocr=%v  n=%d\n", file, format, *ocr, len(ms))
	fmt.Printf("  min   %7.2f ms\n", ms[0])
	fmt.Printf("  p50   %7.2f ms\n", pct(ms, 0.50))
	fmt.Printf("  p95   %7.2f ms\n", pct(ms, 0.95))
	fmt.Printf("  max   %7.2f ms\n", ms[len(ms)-1])
	fmt.Printf("  mean  %7.2f ms\n", mean(ms))
}

func ctx(r *research.Runner) context.Context { return context.Background() }

func warm(ctx context.Context, r *research.Runner, in, format string, ocr bool) int64 {
	start := time.Now()
	_, _ = r.Parse(ctx, in, "", research.ParseOpts{Format: format, OCR: ocr})
	return time.Since(start).Milliseconds()
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func mean(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}