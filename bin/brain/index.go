//usr/bin/env go run -tags=brain_index "$0" "$@"; exit
//go:build brain_index
//
// bin/brain/index.go - rebuild the Ladybug graph (Python write path).
//
//	./bin/brain/index.go --rebuild
//	./bin/brain/index.go --rebuild --with-mail
//	./bin/brain/index.go --dry-run --with-mail
//
// v1 write is always a rebuild when mail is included (live FTS/HNSW + bulk
// insert corrupts Ladybug 0.19 WAL). `add` is v2.
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
