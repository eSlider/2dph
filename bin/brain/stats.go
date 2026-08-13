//usr/bin/env go run -tags=brain_stats "$0" "$@"; exit
//go:build brain_stats
//
// bin/brain/stats.go - index health.
//
//	./bin/brain/stats.go
//	./bin/brain/stats.go --json
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/kb/stats", os.Args[1:]))
}
