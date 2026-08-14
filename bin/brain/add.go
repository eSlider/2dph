//usr/bin/env go run -tags=brain_add "$0" "$@"; exit
//go:build brain_add
//
// bin/brain/add.go - incremental leaf write (Python kblib, no rebuild).
//
//	./bin/brain/add.go --text T --root facts --source "a.md x b.md"
//	./bin/brain/add.go --json
//
// D6: write stays Python. Does not delete var/kb.lbug.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/kb/add", os.Args[1:]))
}
