//usr/bin/env go run -tags=system_ladybug "$0" "$@"; exit
//go:build cgo && system_ladybug
//
// bin/brain/search.go - deduction search over the 2dph brain.
//
//	./bin/brain/search.go "query" [--root facts|info] [--repo P] [-n N] [--json]
//	./bin/brain/search.go serve [port]
//	./bin/brain/search.go --list-model
//
// Needs CGO + libladybug (CGO_CFLAGS/CGO_LDFLAGS). Prefer the wrapper
// bin/kb/search which sets those and builds a binary for the embed daemon.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(brain.Main(os.Args[1:]))
}
