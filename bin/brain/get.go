//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_get "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_get
//
// bin/brain/get.go - read one leaf by id.
//
//	./bin/brain/get.go <id>
//	./bin/brain/get.go <id> --body
//	./bin/brain/get.go <id> --json
//
// Shebang routes through bin/cgo/zig. Python bin/kb/get is the CI fallback (no cgo).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(brain.MainGet(os.Args[1:]))
}
