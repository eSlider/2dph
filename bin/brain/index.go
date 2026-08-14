//usr/bin/env go run -tags=brain_index "$0" "$@"; exit
//go:build brain_index
//
// bin/brain/index.go - rebuild the Ladybug graph (Python write path).
//
//	./bin/brain/index.go --rebuild
//	./bin/brain/index.go --rebuild --with-mail
//	./bin/brain/index.go --dry-run --with-mail
//
// v1 write: bin/brain/add.go for one/few leafs (indexes may already exist).
// Bulk mail/corpus still --rebuild (fresh file, indexes last).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	args := append([]string{"--with-mail"}, os.Args[1:]...)
	os.Exit(cmdbin.ExecFile("bin/kb/index", args))
}
