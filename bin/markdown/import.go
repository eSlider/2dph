//usr/bin/env go run -tags=markdown_import "$0" "$@"; exit
//go:build markdown_import
//
// bin/markdown/import.go - split markdown into leafs (mistune).
//
//	./bin/markdown/import.go [dir]
//	./bin/markdown/import.go --files a.md,b.md --json
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/md/import", os.Args[1:]))
}
