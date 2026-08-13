//usr/bin/env go run -tags=facts_extract "$0" "$@"; exit
//go:build facts_extract
//
// bin/facts/extract.go - acquire confirmed facts (2-source each).
//
//	./bin/facts/extract.go [--json] [--dry-run]
//
// Python bin/facts/extract is the implementation. Graph write stays Python.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/facts/extract", os.Args[1:]))
}
