//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_eval "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_eval
//
// bin/brain/eval.go - recall@5 gate.
//
//	./bin/brain/eval.go
//	./bin/brain/eval.go --json
//
// Shebang routes through bin/cgo/zig. Python bin/kb/eval is the CI fallback (no cgo).
// Control questions live in internal/brain/rank (cgo-free).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(brain.MainEval(os.Args[1:]))
}
