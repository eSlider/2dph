//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_stats "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_stats
//
// bin/brain/stats.go - index health.
//
//	./bin/brain/stats.go
//	./bin/brain/stats.go --json
//
// Shebang routes through bin/cgo/zig. Python bin/kb/stats is the CI fallback (no cgo).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(brain.MainStats(os.Args[1:]))
}
