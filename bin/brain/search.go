//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug
//
// bin/brain/search.go - deduction search over the 2dph brain.
//
//	./bin/brain/search.go "query" [--root facts|info] [--repo P] [-n N] [--hop N] [--json] [--no-web]
//	./bin/brain/search.go serve [port]
//	./bin/brain/search.go --list-model
//
// Shebang routes through bin/cgo/zig (Zig cc + liblbug), not gcc.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(brain.Main(os.Args[1:]))
}
